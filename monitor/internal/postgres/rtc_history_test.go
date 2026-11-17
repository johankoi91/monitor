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

func TestRTCHistoryQuotaAndRestrictedRuntime(t *testing.T) {
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
	if _, err = pool.Exec("CREATE SCHEMA avops; CREATE TABLE avops.rtc_probe_audit(audit_id text PRIMARY KEY,at timestamptz,payload jsonb); INSERT INTO avops.rtc_probe_audit VALUES('legacy',now(),'{}')"); err != nil {
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
	if err = db.SaveDocument("rtc-probe/state-v1", state); err != nil {
		t.Fatal(err)
	}
	saved, ok, err := db.LoadDocument("rtc-probe/state-v1")
	if err != nil || !ok || !bytes.Equal(saved, state) {
		t.Fatal("control state did not persist", err)
	}
	var retired bool
	if err = pool.QueryRow("SELECT to_regclass('avops.rtc_probe_audit') IS NULL").Scan(&retired); err != nil || !retired {
		t.Fatal("retired table still exists", err)
	}
	if _, err = pool.Exec("INSERT INTO avops.system_audit(actor,source_ip,action,target,outcome) VALUES('fixture','127.0.0.1','ACCESS_DENIED','/api/v1/rtc/tasks','DENIED'),('fixture','127.0.0.1','OTHER','baseline','SUCCESS')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = postgres.Open(dsn, key, true)
	if err != nil {
		t.Fatal(err)
	}
	var rtcRows, otherRows int
	if err = pool.QueryRow("SELECT count(*) FILTER(WHERE target LIKE '/api/v1/rtc/%'),count(*) FILTER(WHERE target='baseline') FROM avops.system_audit").Scan(&rtcRows, &otherRows); err != nil || rtcRows != 0 || otherRows != 1 {
		t.Fatal("retired operation cleanup scope incorrect", rtcRows, otherRows, err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	r := hub.Run{ID: protocol.ID(), TaskID: "task", TargetID: "rtc", Driver: "agora", Status: "SUCCESS", CreatedAt: old, FinishedAt: &old, Logs: map[string]protocol.LogRequest{}}
	if err = db.RTCPutRun(r, 1); err == nil {
		t.Fatal("history quota not enforced")
	}
	if err = db.RTCPutRun(r, 16<<20); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RTCGetRun(r.ID); err == nil {
		t.Fatal("expired execution not cleaned")
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
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RTCGetRun(child.ID); err != nil {
		t.Fatal("port details expired before sweep summary", err)
	}
	parent.FinishedAt = &old
	if err = db.RTCPutRun(parent, 16<<20); err != nil {
		t.Fatal(err)
	}
	if err = db.RTCCleanup(time.Now().Add(-7*24*time.Hour), 100); err != nil {
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
	if err = runtime.SaveDocument("rtc-probe/state-v1", data); err != nil {
		t.Fatal("restricted runtime cannot save control state", err)
	}
}
