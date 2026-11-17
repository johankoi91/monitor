package main

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

type startupStamp struct {
	snapshot string
	at       time.Time
}
type startupReporter map[string]startupStamp

// Health and restart eligibility still use each fresh Docker inspection. Only
// the large startup payload is omitted between periodic or changed reports.
func (r startupReporter) filter(report *model.Report, now time.Time) {
	for i := range report.Containers {
		o := &report.Containers[i]
		o.StartupValid = o.Startup != nil && !o.Startup.Incomplete && !o.Startup.Truncated
		last, ok := r[o.Container.ContainerID]
		if !report.Complete || !o.StartupValid || ok && last.snapshot == o.Container.SnapshotID && now.Sub(last.at) < model.StartupReportInterval {
			o.Startup = nil
		}
	}
}

// Advance only after a complete, size-checked snapshot enters the WSS queue.
// A reconnect creates a new reporter, sending the initial parameters again.
func (r startupReporter) sent(report model.Report, now time.Time) {
	if !report.Complete {
		return
	}
	seen := map[string]bool{}
	for _, o := range report.Containers {
		seen[o.Container.ContainerID] = true
		if o.Startup != nil && o.StartupValid {
			r[o.Container.ContainerID] = startupStamp{o.Container.SnapshotID, now}
		}
	}
	for id := range r {
		if !seen[id] {
			delete(r, id)
		}
	}
}
