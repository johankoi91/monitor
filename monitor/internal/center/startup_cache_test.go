package center

import (
	"context"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"testing"
	"time"
)

func TestReportedStartupSurvivesOfflineMissingAndRecreation(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	if _, err := s.Save(selection("", "rtc-a"), "test"); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		v, err := s.NodeStartup(context.Background(), "rtc-a/ap")
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)
	}
	if read()["startup"] == nil || read()["stale"] != false {
		t.Fatal("valid pushed record missing")
	}
	s.Disconnect("rtc-a", session)
	if read()["startup"] == nil || read()["stale"] != true {
		t.Fatal("offline record lost")
	}
	session = s.Connect("rtc-a")
	now := time.Now().UTC()
	if err := s.Receive("rtc-a", session, model.Report{Type: "snapshot", Complete: true, Sequence: 1, CollectedAt: now}); err != nil {
		t.Fatal(err)
	}
	if read()["startup"] == nil || read()["stale"] != true {
		t.Fatal("deleted container record lost")
	}
	s.mu.RLock()
	previous := s.lastStartups["rtc-a"]["ap"]
	s.mu.RUnlock()
	observation := model.Observation{Container: model.Container{ContainerID: "new-id", ContainerName: "ap", SnapshotID: "new-snapshot", CollectedAt: now, RuntimeStatus: "running"}, StartupValid: true}
	report := model.Report{Type: "snapshot", Complete: true, Sequence: 2, CollectedAt: now, Containers: []model.Observation{observation}}
	if err := s.Receive("rtc-a", session, report); err != nil {
		t.Fatal(err)
	}
	if read()["container_id"] != previous.ContainerID || read()["stale"] != true {
		t.Fatal("old record attributed to recreated container")
	}
	report.Sequence = 3
	report.Containers[0].Startup = &model.Startup{SnapshotID: "new-snapshot", CollectedAt: now, Config: model.StartupConfig{Cmd: []string{"actual-raw-parameter"}}}
	if err := s.Receive("rtc-a", session, report); err != nil {
		t.Fatal(err)
	}
	if read()["container_id"] != "new-id" || read()["stale"] != false {
		t.Fatal("new report did not replace historical record")
	}
	report.Sequence = 4
	report.Containers[0].Startup.Incomplete = true
	if err := s.Receive("rtc-a", session, report); err != nil {
		t.Fatal(err)
	}
	if read()["startup"].(*model.Startup).Incomplete {
		t.Fatal("incomplete inspect replaced valid record")
	}
	if _, err := s.NodeStartup(context.Background(), "rtc-a/not-managed"); err == nil {
		t.Fatal("unmanaged history exposed")
	}

}
