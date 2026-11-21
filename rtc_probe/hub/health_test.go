package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"testing"
	"time"
)

func healthFixture(t *testing.T) (*Hub, protocol.Task) {
	t.Helper()
	c := testConfig()
	c.Targets[0].Driver = "agora"
	c.Tasks[0].TimeoutSeconds = 30
	c.Tasks[0].FailureThreshold = 3
	c.Tasks[0].RecoveryThreshold = 2
	c.Targets[0].RTC = &protocol.RTCParameters{AppID: "88dc432b1fe248039879fb84844c497b", AuthMode: "none"}
	if err := c.Targets[0].RTC.Normalize(); err != nil {
		t.Fatal(err)
	}
	h, err := newTestHub(t.TempDir(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := h.Session("probe-a")
	m := protocol.Msg("hello")
	m.Driver = "agora"
	m.Capacity = 1
	if _, err = receiveTest(h, "probe-a", s, m); err != nil {
		t.Fatal(err)
	}
	task, _ := h.task(c.Tasks[0].ID)
	return h, task
}
func TestHealthFailureRecoveryAndInterruption(t *testing.T) {
	h, task := healthFixture(t)
	now := time.Now().UTC()
	seq := 0
	apply := func(status string) {
		seq++
		r := &Run{ID: protocol.ID(), TaskID: task.ID, TargetID: task.TargetID, Driver: "agora", Revision: revision(task, h.target(task.TargetID)), Status: status, Reason: "TEST", CreatedAt: now, FinishedAt: &now}
		h.applyHealth(&h.state, r, now)
	}
	apply("FAILED")
	if h.state.Health[task.ID].State != "UNKNOWN" {
		t.Fatal("first failure established abnormal baseline")
	}
	apply("SUCCESS")
	if h.state.Health[task.ID].State != "HEALTHY" {
		t.Fatal("first success did not establish baseline")
	}
	apply("FAILED")
	apply("FAILED")
	v := h.state.Health[task.ID]
	if v.State != "HEALTHY" || v.FailureCount != 2 || v.Reason != "FAILURE_CONFIRMING" {
		t.Fatal("failure debounce missing")
	}
	apply("FAILED")
	if h.state.Health[task.ID].State != "UNHEALTHY" {
		t.Fatal("failure threshold not applied")
	}
	apply("SUCCESS")
	if h.state.Health[task.ID].State != "UNHEALTHY" || h.state.Health[task.ID].SuccessCount != 1 {
		t.Fatal("recovery debounce missing")
	}
	apply("SUCCESS")
	if h.state.Health[task.ID].State != "HEALTHY" {
		t.Fatal("recovery threshold not applied")
	}
	apply("FAILED")
	apply("ERROR")
	v = h.state.Health[task.ID]
	if v.State != "UNKNOWN" || v.FailureCount != 0 || v.SuccessCount != 0 {
		t.Fatal("monitor-side error counted as business failure")
	}
	apply("SUCCESS")
	if err := h.EnableProbe("probe-a", false); err != nil {
		t.Fatal(err)
	}
	h.refreshHealth(now)
	v = h.state.Health[task.ID]
	if v.State != "UNKNOWN" || v.Reason != "PROBE_DISABLED" {
		t.Fatal("disabled probe remained healthy")
	}
}
func TestHealthExpiryRequiresNewEvidence(t *testing.T) {
	h, task := healthFixture(t)
	now := time.Now().UTC()
	old := now.Add(-time.Duration(task.IntervalSeconds*2+task.TimeoutSeconds+1) * time.Second)
	v := h.newHealth(task, old, "MEDIA_VERIFIED")
	v.State = "HEALTHY"
	v.LastResultAt = &old
	v.UnknownSince = nil
	h.state.Health[task.ID] = v
	h.refreshHealth(now)
	if h.state.Health[task.ID].State != "UNKNOWN" || h.state.Health[task.ID].Reason != "RESULT_EXPIRED" {
		t.Fatal("expired evidence kept service normal")
	}
	s := h.sessions["probe-a"]
	s.Seen = now
	if h.effectiveHealth(task, now).State != "UNKNOWN" {
		t.Fatal("heartbeat restored business health")
	}
}
func TestHistoryRetentionIsScoped(t *testing.T) {
	h, err := newTestHub(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	for i := 0; i < 120; i++ {
		id := protocol.ID()
		r := Run{ID: id, TaskID: "audio", TargetID: "service", Driver: "agora", Status: "SUCCESS", CreatedAt: old, FinishedAt: &old}
		if err := h.archive.RTCPutRun(r, 512<<20); err != nil {
			t.Fatal(err)
		}
	}
	page, err := h.History("audio", 10, 100)
	if err != nil || page.Total != 120 || len(page.Runs) != 10 {
		t.Fatal("history tied to 100-run cache", err)
	}
	if err = h.archive.RTCCleanup(now.Add(-7*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	usage, err := h.archive.RTCUsage()
	if err != nil || usage.Runs != 20 {
		t.Fatal("retention was not bounded to a batch", err)
	}
}

func TestControlCacheKeepsHistoryAndPendingDiagnostics(t *testing.T) {
	h, err := newTestHub(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	oldest := ""
	if err = h.change(func(s *state) error {
		for i := 0; i < 12; i++ {
			id := protocol.ID()
			at := now.Add(-time.Duration(i+1) * time.Minute)
			s.Runs[id] = &Run{ID: id, TaskID: "audio", TargetID: "service", Driver: "agora", Status: "SUCCESS", CreatedAt: at, Deadline: at, FinishedAt: &at, Logs: map[string]protocol.LogRequest{}, Endpoints: map[string]*Endpoint{"sender": {Job: protocol.Job{RuntimeID: "probe-a"}}, "receiver": {Job: protocol.Job{RuntimeID: "probe-b"}}}}
			if i == 11 {
				oldest = id
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(h.state.Runs) != 4 {
		t.Fatal("completed history bloats control state", len(h.state.Runs))
	}
	page, err := h.History("audio", 20, 0)
	if err != nil || page.Total != 12 {
		t.Fatal("control compaction discarded history", page.Total, err)
	}
	if _, err = h.Detail(oldest); err != nil {
		t.Fatal("archived detail missing", err)
	}
	if err = h.Collect(oldest); err != nil {
		t.Fatal("archived recollect failed", err)
	}
	if h.state.Runs[oldest] == nil {
		t.Fatal("pending collection evicted")
	}
}
