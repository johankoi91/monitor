package center

import (
	"context"
	"github.com/johankoi91/monitor/runtime/internal/history"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"testing"
	"time"
)

func TestResourceHistoryPreservesMissingZeroAndLogicalIdentity(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	if _, err := s.Save(selection("", "rtc-a"), "fixture"); err != nil {
		t.Fatal(err)
	}
	zero := float64(0)
	memory := uint64(0)
	at := time.Now().UTC()
	r := report("ap")
	r.Sequence = 2
	r.Containers[0].StartedAt = "first-start"
	r.Containers[0].Container.Resources = &model.ResourceMetrics{Source: "cadvisor", Status: "AVAILABLE", ReasonCode: "OK", SampledAt: &at, FetchedAt: at, CPUUsageCores: &zero, MemoryWorkingSetBytes: &memory, NetworkScope: "HOST_SHARED", DiskIOStatus: "NO_VALID_SAMPLES", DiskReadBPS: &zero}
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureResourceHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err := s.ResourceHistory(context.Background(), "rtc-a/ap", "1h")
	if err != nil {
		t.Fatal(err)
	}
	points := view.(map[string]any)["samples"].([]history.Sample)
	if len(points) != 1 || points[0].CPU == nil || *points[0].CPU != 0 || points[0].Memory == nil || *points[0].Memory != 0 {
		t.Fatal("measured zero was lost")
	}
	if points[0].RX != nil || points[0].DiskRead != nil {
		t.Fatal("unattributable or missing counters recorded as zero")
	}
	r.Sequence = 3
	r.Containers[0].Container.ContainerID = "recreated"
	r.Containers[0].StartedAt = "new-start"
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureResourceHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err = s.ResourceHistory(context.Background(), "rtc-a/ap", "1h")
	if err != nil {
		t.Fatal(err)
	}
	points = view.(map[string]any)["samples"].([]history.Sample)
	if len(points) != 2 {
		t.Fatal("container incarnation history was overwritten")
	}
	s.Disconnect("rtc-a", session)
	if err := s.CaptureResourceHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, _ = s.ResourceHistory(context.Background(), "rtc-a/ap", "1h")
	points = view.(map[string]any)["samples"].([]history.Sample)
	last := points[len(points)-1]
	if last.CPU != nil || last.Memory != nil {
		t.Fatal("offline snapshot fabricated metrics")
	}
	if _, err = s.ResourceHistory(context.Background(), "rtc-a/unmanaged", "1h"); err == nil {
		t.Fatal("unmanaged history exposed")
	}
	if _, err = s.ResourceHistory(context.Background(), "rtc-a/ap", "365d"); err == nil {
		t.Fatal("unbounded range accepted")
	}
}
