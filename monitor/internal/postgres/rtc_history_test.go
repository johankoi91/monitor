package postgres_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/runtime/internal/postgres"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestRTCAtomicHealthAuditQuotaAndRestrictedRuntime(t *testing.T) {
	dsn := os.Getenv("AVOPS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/avops_test" {
		t.Fatal("refusing any database except avops_test")
	}
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec("DROP SCHEMA IF EXISTS avops CASCADE"); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{42}, 32)
	db, err := postgres.Open(dsn, key, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	state := []byte(`{"revision":"v1","settings":{"history_mb":16}}`)
	audit := hub.Audit{ID: protocol.ID(), At: time.Now().UTC(), Actor: "fixture", Action: "RTC_HEALTH_CHANGED", Target: "rtc", TaskID: "task", Outcome: "HEALTHY", After: &hub.TaskHealth{State: "HEALTHY"}}
	if err = db.RTCCommitState(state, []hub.Audit{audit}); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCCommitState(state, []hub.Audit{audit}); err != nil {
		t.Fatal("idempotent audit failed", err)
	}
	page, err := db.RTCListAudits("task", 20, 0)
	if err != nil || page.Total != 1 {
		t.Fatal("audit duplicated", page.Total, err)
	}
	if _, err = pool.Exec("ALTER TABLE avops.rtc_probe_audit ADD CONSTRAINT reject_fixture CHECK(payload->>'actor'<>'reject')"); err != nil {
		t.Fatal(err)
	}
	bad := audit
	bad.ID = protocol.ID()
	bad.Actor = "reject"
	if err = db.RTCCommitState([]byte(`{"revision":"v2","settings":{"history_mb":16}}`), []hub.Audit{bad}); err == nil {
		t.Fatal("invalid audit committed")
	}
	saved, ok, err := db.LoadDocument("rtc-probe/state-v1")
	if err != nil || !ok || !bytes.Equal(saved, state) {
		t.Fatal("state committed without its health audit", err)
	}
	if _, err = pool.Exec("ALTER TABLE avops.rtc_probe_audit DROP CONSTRAINT reject_fixture"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	r := hub.Run{ID: protocol.ID(), TaskID: "task", TargetID: "rtc", Driver: "agora", Status: "SUCCESS", CreatedAt: old, FinishedAt: &old, Logs: map[string]protocol.LogRequest{}}
	if err = db.RTCPutRun(r, 1); err == nil {
		t.Fatal("history quota not enforced")
	}
	if err = db.RTCPutRun(r, 16<<20); err != nil {
		t.Fatal(err)
	}
	critical := audit
	critical.ID = protocol.ID()
	critical.At = old
	if err = db.RTCPutAudit(critical, 16<<20); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), time.Now().Add(-30*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RTCGetRun(r.ID); err == nil {
		t.Fatal("expired execution not cleaned")
	}
	page, err = db.RTCListAudits("task", 20, 0)
	if err != nil || page.Total != 2 {
		t.Fatal("critical 30-day audit deleted with 7-day execution", err)
	}
	// A sweep is public history; its older child remains readable for the parent window.
	parentAt := time.Now().UTC().Add(-6 * 24 * time.Hour)
	parent := hub.Run{ID: protocol.ID(), TaskID: "sweep", TargetID: "rtc", Driver: "agora", Kind: "EDGE_SWEEP", Status: "SUCCESS", CreatedAt: old, FinishedAt: &parentAt, Logs: map[string]protocol.LogRequest{}}
	child := r
	child.ID = protocol.ID()
	child.TaskID = "sweep"
	child.ParentRunID = parent.ID
	if err = db.RTCPutRun(parent, 16<<20); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCPutRun(child, 16<<20); err != nil {
		t.Fatal(err)
	}
	public, err := db.RTCListRuns("sweep", 20, 0)
	if err != nil || public.Total != 1 {
		t.Fatal("child leaked into public sweep history", public.Total, err)
	}
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), time.Now().Add(-30*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RTCGetRun(child.ID); err != nil {
		t.Fatal("port details expired before sweep summary", err)
	}
	parent.FinishedAt = &old
	if err = db.RTCPutRun(parent, 16<<20); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), time.Now().Add(-30*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RTCGetRun(child.ID); err == nil {
		t.Fatal("expired sweep child retained indefinitely")
	}
	role := "rtc_runtime_" + protocol.ID()[:8]
	if _, err = pool.Exec("CREATE ROLE " + role + " LOGIN; GRANT USAGE ON SCHEMA avops TO " + role + "; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA avops TO " + role); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = nil
	u.User = url.User(role)
	runtime, err := postgres.Open(u.String(), key, false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err = runtime.RTCInit(); err != nil {
		t.Fatal("runtime requires DDL", err)
	}
	if _, err = runtime.Pool.Exec("CREATE TABLE avops.forbidden_runtime_ddl(id text)"); err == nil {
		t.Fatal("runtime unexpectedly owns schema DDL")
	}
	data, _ := json.Marshal(map[string]any{"settings": map[string]int{"history_mb": 16}, "revision": "runtime"})
	audit.ID = protocol.ID()
	if err = runtime.RTCCommitState(data, []hub.Audit{audit}); err != nil {
		t.Fatal("restricted runtime cannot save audited health", err)
	}
}
