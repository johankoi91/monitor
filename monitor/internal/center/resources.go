package center

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

// Freshness belongs to each cAdvisor sample, not just to the Agent report.
func resourceView(m *model.ResourceMetrics, a *agentState, now time.Time) *model.ResourceMetrics {
	if m == nil {
		return nil
	}
	copy := *m
	if copy.HostFilesystem != nil && (!fresh(a, now) || now.Sub(copy.HostFilesystem.CollectedAt) > 45*time.Second || copy.HostFilesystem.CollectedAt.After(now.Add(5*time.Second))) {
		copy.HostFilesystem = nil
	}
	if copy.OOMObservedAt != nil && (!fresh(a, now) || now.Sub(*copy.OOMObservedAt) > 45*time.Second || copy.OOMObservedAt.After(now.Add(5*time.Second))) {
		copy.OOMEventsTotal = nil
		copy.OOMObservedAt = nil
	}
	if !fresh(a, now) || m.SampledAt == nil || m.SampledAt.IsZero() || now.Sub(*m.SampledAt) > 45*time.Second || m.SampledAt.After(now.Add(5*time.Second)) || m.FetchedAt.After(now.Add(5*time.Second)) {
		copy.Stale = true
		if copy.Status != "UNAVAILABLE" {
			copy.ReasonCode = "RESOURCE_STALE"
		}
	}
	return &copy
}

func lifecycleView(value *model.ContainerLifecycle, a *agentState, now time.Time) *model.ContainerLifecycle {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Stale = !fresh(a, now) || copy.LastObservedAt.IsZero() || now.Sub(copy.LastObservedAt) > 45*time.Second || copy.LastObservedAt.After(now.Add(5*time.Second))
	return &copy
}
