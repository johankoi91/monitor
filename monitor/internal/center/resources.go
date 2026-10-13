package center

import (
	"context"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

// Node startup is limited to saved baseline members. It does not expose the
// broader discovery inventory to customer operators.
func (s *Store) NodeStartup(ctx context.Context, id string) (any, error) {
	s.mu.RLock()
	n, _, observed := s.findNodeLocked(id)
	if n.ID == "" {
		s.mu.RUnlock()
		return nil, problem(404, "NODE_NOT_FOUND", "节点不在当前基准中")
	}
	if observed == nil {
		s.mu.RUnlock()
		return nil, problem(404, "STARTUP_UNAVAILABLE", "容器缺失或尚未收到采集")
	}
	cid := observed.Container.ContainerID
	s.mu.RUnlock()
	return s.RequestStartup(ctx, n.AgentID, cid)
}

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
