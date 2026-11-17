package main

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"testing"
	"time"
)

func TestStartupReportingKeepsHealthFrequentAndParametersPeriodic(t *testing.T) {
	r := startupReporter{}
	now := time.Now().UTC()
	snapshot := func(id, config string) model.Report {
		return model.Report{Complete: true, Containers: []model.Observation{{Container: model.Container{ContainerID: id, SnapshotID: config, RuntimeStatus: "running"}, Startup: &model.Startup{SnapshotID: config}}}}
	}
	first := snapshot("original", "config1")
	r.filter(&first, now)
	if first.Containers[0].Startup == nil {
		t.Fatal("first report omitted")
	}
	// Failed/oversized messages do not postpone the next successful report.
	retry := snapshot("original", "config1")
	r.filter(&retry, now.Add(15*time.Second))
	if retry.Containers[0].Startup == nil {
		t.Fatal("unsent payload marked sent")
	}
	r.sent(first, now)
	for _, elapsed := range []time.Duration{15 * time.Second, 45 * time.Second, 9*time.Minute + 59*time.Second} {
		next := snapshot("original", "config1")
		r.filter(&next, now.Add(elapsed))
		if next.Containers[0].Startup != nil || !next.Containers[0].StartupValid || next.Containers[0].Container.RuntimeStatus != "running" {
			t.Fatal("intermediate health snapshot lost validity or repeated parameters")
		}
	}
	due := snapshot("original", "config1")
	r.filter(&due, now.Add(10*time.Minute))
	if due.Containers[0].Startup == nil {
		t.Fatal("10 minute report missing")
	}
	for _, s := range []model.Report{snapshot("recreated", "config1"), snapshot("original", "config2")} {
		r.filter(&s, now.Add(time.Minute))
		if s.Containers[0].Startup == nil {
			t.Fatal("new identity/config not reported immediately")
		}
	}
	incomplete := snapshot("original", "config1")
	incomplete.Complete = false
	r.filter(&incomplete, now.Add(10*time.Minute))
	r.sent(incomplete, now.Add(10*time.Minute))
	if incomplete.Containers[0].Startup != nil {
		t.Fatal("partial report retained payload")
	}
	retry = snapshot("original", "config1")
	r.filter(&retry, now.Add(10*time.Minute+15*time.Second))
	if retry.Containers[0].Startup == nil {
		t.Fatal("partial report advanced timer")
	}
	reconnect := startupReporter{}
	retry = snapshot("original", "config1")
	reconnect.filter(&retry, now.Add(time.Minute))
	if retry.Containers[0].Startup == nil {
		t.Fatal("reconnect omitted initial parameters")
	}
}
