package probe

import (
	"context"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func retentionFixture(t *testing.T) *Client {
	t.Helper()
	return &Client{config: Config{DataDir: t.TempDir()}, records: map[string]*record{}, workers: map[string]*process{}, collectActive: map[string]string{}, retention: protocol.DefaultRetention()}
}
func addRetained(t *testing.T, c *Client, id, outcome string, acked bool, finished time.Time, size int64) {
	t.Helper()
	dir := c.runDir(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "sdk.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, role := range []string{"sender", "receiver"} {
		c.records[recordKey(id, role)] = &record{Job: protocol.Job{RunID: id, Role: role, Deadline: finished}, Acked: acked, Events: []protocol.Event{}, Result: &protocol.Result{Outcome: outcome, Cleanup: "SUCCESS", FinishedAt: finished}}
	}
}
func TestRetentionUsesOutcomeAndProtectsUnacknowledgedAndActive(t *testing.T) {
	c := retentionFixture(t)
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	addRetained(t, c, "success", "SUCCESS", true, old, 1024)
	addRetained(t, c, "failure", "FAILED", true, old, 1024)
	addRetained(t, c, "unacked", "SUCCESS", false, old, 1024)
	addRetained(t, c, "active", "SUCCESS", true, old, 1024)
	c.workers["active"] = &process{}
	addRetained(t, c, "collecting", "SUCCESS", true, old, 1024)
	c.collectActive["request"] = "collecting"
	if err := c.pruneLocked(now, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.runDir("success")); !os.IsNotExist(err) {
		t.Fatal("expired success retained")
	}
	for _, id := range []string{"failure", "unacked", "active", "collecting"} {
		if _, err := os.Stat(c.runDir(id)); err != nil {
			t.Fatal("protected run deleted", id)
		}
	}
	if c.records[recordKey("success", "sender")] != nil || c.records[recordKey("success", "receiver")] != nil {
		t.Fatal("only one role removed")
	}
	delete(c.workers, "active")
	delete(c.collectActive, "request")
	if err := c.pruneLocked(now.Add(25*time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.runDir("failure")); !os.IsNotExist(err) {
		t.Fatal("expired failure retained")
	}
	if c.records[recordKey("unacked", "sender")] == nil {
		t.Fatal("unacknowledged evidence lost")
	}
}
func TestBudgetEvictsConfirmedSuccessBeforeFailureAndRejectsWhenProtected(t *testing.T) {
	c := retentionFixture(t)
	c.retention.DiskMB = 16
	now := time.Now().UTC()
	addRetained(t, c, "failure", "FAILED", true, now, 8<<20)
	addRetained(t, c, "success", "SUCCESS", true, now, 8<<20)
	if err := c.pruneLocked(now, 4<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.runDir("success")); !os.IsNotExist(err) {
		t.Fatal("success not reclaimed first")
	}
	if c.records[recordKey("failure", "sender")] == nil {
		t.Fatal("failure reclaimed before success")
	}
	addRetained(t, c, "unacked", "FAILED", false, now, 20<<20)
	if err := c.pruneLocked(now, 0); err == nil {
		t.Fatal("protected evidence exceeds budget without rejecting")
	}
	if c.records[recordKey("unacked", "receiver")] == nil {
		t.Fatal("unacknowledged result deleted")
	}
}

func TestBudgetRejectsAssignmentWithoutStartingSDKAndExpiryIsReported(t *testing.T) {
	c := retentionFixture(t)
	c.config.ID = "probe"
	c.config.Driver = "agora"
	c.config.Capacity = 1
	c.retention.DiskMB = 16
	c.config.WorkerCommand = func(context.Context) *exec.Cmd { t.Fatal("SDK started despite exhausted reserve"); return nil }
	job := protocol.Job{RunID: protocol.ID(), TaskID: "task", ProbeID: "probe", Role: "pair", Revision: "v1", Deadline: time.Now().Add(time.Minute), Target: protocol.Target{ID: "target", Driver: "agora"}}
	if err := c.prepare(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"sender", "receiver"} {
		r := c.records[recordKey(job.RunID, role)]
		if r == nil || r.Result == nil || r.Result.Outcome != "ERROR" || r.Result.Reason != "LOCAL_RESOURCE_EXHAUSTED" {
			t.Fatal("resource rejection missing", role)
		}
	}
	req := protocol.LogRequest{ID: protocol.ID(), RunID: protocol.ID(), ProbeID: "probe", Role: "receiver", ExpiresAt: time.Now().Add(time.Minute)}
	c.collect(context.Background(), req)
	report, ok := c.logReports[req.ID]
	if !ok || report.Status != "EXPIRED" {
		t.Fatal("expired local history leaves center waiting")
	}
	ack := protocol.Msg("log_status_ack")
	ack.ID = req.ID
	if err := c.handle(context.Background(), ack); err != nil {
		t.Fatal(err)
	}
	if len(c.logReports) != 0 {
		t.Fatal("log status acknowledgement ignored")
	}
}
