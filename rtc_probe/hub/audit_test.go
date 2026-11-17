package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"testing"
	"time"
)

func TestHealthAuditSurvivesRunRetentionAndRecordsOnlyTransitions(t *testing.T) {
	h, task := healthFixture(t)
	now := time.Now().UTC()
	apply := func(status string) {
		t.Helper()
		now = now.Add(time.Second)
		r := &Run{ID: protocol.ID(), TaskID: task.ID, TargetID: task.TargetID, Driver: "agora", Revision: revision(task, h.target(task.TargetID)), Status: status, Reason: "TEST", CreatedAt: now, FinishedAt: &now}
		if err := h.change(func(s *state) error { h.applyHealth(s, r, now); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	apply("SUCCESS")
	apply("SUCCESS")
	apply("FAILED")
	apply("FAILED")
	apply("FAILED")
	apply("SUCCESS")
	apply("SUCCESS")
	page, err := h.Audits(task.ID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Fatal("repeated state creates audit or transitions missing", page.Total)
	}
	if page.Audits[0].Before.State != "UNHEALTHY" || page.Audits[0].After.State != "HEALTHY" {
		t.Fatal("audit lacks transition identity")
	}
	if err = h.archive.RTCCleanup(now.Add(8*24*time.Hour).Add(-7*24*time.Hour), now.Add(8*24*time.Hour).Add(-30*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	page, err = h.Audits(task.ID, 100, 0)
	if err != nil || page.Total != 3 {
		t.Fatal("7-day history cleanup deleted 30-day health audit", err)
	}
	h2, err := New(h.dir, h.config, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err = h2.Audits(task.ID, 100, 0)
	if err != nil || page.Total != 4 {
		t.Fatal("restart transition not durable", page.Total, err)
	}
}
