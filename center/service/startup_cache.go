package center

import (
	"github.com/johankoi91/monitor/runtime/shared/model"
	"time"
)

// One last valid report per Agent/container name. Historical identity is kept
// separately from current observations and is never used to approve a restart.
type StartupRecord struct {
	ContainerID string         `json:"container_id"`
	Startup     *model.Startup `json:"startup"`
	ReportedAt  time.Time      `json:"reported_at"`
}

// Frequently polled status carries identity/freshness, not raw parameters.
type StartupReference struct {
	ContainerID string     `json:"container_id,omitempty"`
	SnapshotID  string     `json:"snapshot_id,omitempty"`
	ReportedAt  *time.Time `json:"reported_at,omitempty"`
	Stale       bool       `json:"stale"`
}

func (s *Store) startupReferenceLocked(agent, name string, observed *model.Observation) StartupReference {
	r, ok := s.lastStartups[agent][name]
	ref := StartupReference{Stale: true}
	if !ok || r.Startup == nil {
		return ref
	}
	ref.ContainerID, ref.SnapshotID, ref.ReportedAt = r.ContainerID, r.Startup.SnapshotID, &r.ReportedAt
	if observed != nil {
		ref.Stale = !fresh(s.agents[agent], time.Now().UTC()) || observed.Container.ContainerID != r.ContainerID || observed.Container.SnapshotID != r.Startup.SnapshotID || observed.Container.RuntimeStatus != "running" || time.Since(r.ReportedAt) > model.StartupReportInterval+time.Minute
	}
	return ref
}

func (s *Store) NodeStartup(id, containerID, snapshotID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node, _, observed := s.findNodeLocked(id)
	if node.ID == "" {
		return nil, problem(404, "NODE_NOT_FOUND", "节点不在当前基准中")
	}
	ref := s.startupReferenceLocked(node.AgentID, node.ContainerName, observed)
	if containerID != "" && ref.ContainerID != containerID || snapshotID != "" && ref.SnapshotID != snapshotID {
		return nil, problem(409, "STARTUP_CHANGED", "启动参数快照已更新，请等待列表更新后重试")
	}
	return s.startupViewLocked(node.AgentID, node.ContainerName, observed), nil
}

// Caller holds s.mu. Returned values are copies, including startup parameters.
func (s *Store) startupViewLocked(agent, name string, observed *model.Observation) map[string]any {
	r, ok := s.lastStartups[agent][name]
	if !ok {
		return map[string]any{"startup": nil, "stale": true}
	}
	return map[string]any{"startup": clone(r.Startup), "container_id": r.ContainerID, "reported_at": r.ReportedAt, "stale": s.startupReferenceLocked(agent, name, observed).Stale, "source": "agent_report"}
}
