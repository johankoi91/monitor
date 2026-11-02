package center

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

// One last valid report per Agent/container name. Historical identity is kept
// separately from current observations and is never used to approve a restart.
type StartupRecord struct {
	ContainerID string         `json:"container_id"`
	Startup     *model.Startup `json:"startup"`
	ReportedAt  time.Time      `json:"reported_at"`
}

// Caller holds s.mu. Returned values are copies, including startup parameters.
func (s *Store) startupViewLocked(agent, name string, observed *model.Observation) map[string]any {
	r, ok := s.lastStartups[agent][name]
	stale := true
	if ok && observed != nil {
		stale = !fresh(s.agents[agent], time.Now().UTC()) || observed.Container.ContainerID != r.ContainerID || observed.Container.RuntimeStatus != "running" || time.Since(r.ReportedAt) > 45*time.Second
	}
	if !ok {
		return map[string]any{"startup": nil, "stale": true}
	}
	return map[string]any{"startup": clone(r.Startup), "container_id": r.ContainerID, "reported_at": r.ReportedAt, "stale": stale, "source": "agent_report"}
}
