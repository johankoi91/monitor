package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"strconv"
	"time"
)

type TaskHealth struct {
	InfrastructureReason string     `json:"infrastructure_reason,omitempty"`
	TaskID               string     `json:"task_id"`
	TargetID             string     `json:"target_id"`
	Revision             string     `json:"config_revision"`
	State                string     `json:"state"`
	Reason               string     `json:"reason"`
	Enabled              bool       `json:"enabled"`
	FailureCount         int        `json:"failure_count"`
	SuccessCount         int        `json:"success_count"`
	FailureThreshold     int        `json:"failure_threshold"`
	RecoveryThreshold    int        `json:"recovery_threshold"`
	LastRunID            string     `json:"last_run_id,omitempty"`
	LastResultAt         *time.Time `json:"last_result_at,omitempty"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
	UnknownSince         *time.Time `json:"unknown_since,omitempty"`
}
type BusinessFact struct {
	TaskID   string
	TargetID string
	Cluster  string
	Revision string
	RunID    string
	Path     string
	State    string
	Reason   string
	Notify   bool
	At       *time.Time
}

func thresholds(t protocol.Task) (int, int) {
	f, r := t.FailureThreshold, t.RecoveryThreshold
	if f == 0 {
		f = 3
	}
	if r == 0 {
		r = 2
	}
	return f, r
}
func (h *Hub) newHealth(t protocol.Task, now time.Time, reason string) TaskHealth {
	f, r := thresholds(t)
	return TaskHealth{TaskID: t.ID, TargetID: t.TargetID, Revision: revision(t, h.target(t.TargetID)), State: "UNKNOWN", Reason: reason, Enabled: h.enabled(t), FailureThreshold: f, RecoveryThreshold: r, UnknownSince: &now}
}
func (h *Hub) applyHealth(s *state, run *Run, now time.Time) {
	if run.Kind == nativeCoverageKind {
		h.applyNativePhaseResult(s, run, now)
		return
	}
	if run.ParentRunID != "" {
		if h.recordNativeChild(s, run, now) {
			return
		}
		h.recordEdgeResult(s, run, now)
		return
	}
	if !protocol.RealDriver(run.Driver) {
		return
	}
	task, ok := h.task(run.TaskID)
	if !ok || !h.enabled(task) || run.Revision != revision(task, h.target(task.TargetID)) {
		return
	}
	v, ok := s.Health[task.ID]
	if !ok || v.Revision != run.Revision {
		v = h.newHealth(task, now, "WAITING_FIRST_RESULT")
	}
	if v.LastRunID == run.ID {
		return
	}
	s.Health[task.ID] = advanceHealth(v, task, run, now)
}
func advanceHealth(v TaskHealth, task protocol.Task, run *Run, now time.Time) TaskHealth {
	v.Revision = run.Revision
	v.TaskID = task.ID
	v.TargetID = task.TargetID
	v.LastRunID = run.ID
	v.LastResultAt = &now
	v.Enabled = true
	v.FailureThreshold, v.RecoveryThreshold = thresholds(task)
	switch run.Status {
	case "SUCCESS":
		v.FailureCount = 0
		v.LastSuccessAt = &now
		if v.State == "UNHEALTHY" {
			v.SuccessCount++
			if v.SuccessCount >= v.RecoveryThreshold {
				v.State = "HEALTHY"
				v.Reason = "MEDIA_VERIFIED"
				v.SuccessCount = 0
			} else {
				v.Reason = "RECOVERY_CONFIRMING"
			}
		} else {
			v.State = "HEALTHY"
			v.Reason = "MEDIA_VERIFIED"
			v.SuccessCount = 0
		}
		v.UnknownSince = nil
	case "FAILED":
		v.SuccessCount = 0
		v.FailureCount++
		if v.FailureCount >= v.FailureThreshold {
			v.State = "UNHEALTHY"
			v.Reason = "FAILURE_CONFIRMED"
			v.UnknownSince = nil
		} else {
			v.Reason = "FAILURE_CONFIRMING"
		}
	case "ERROR", "CANCELLED":
		v.State = "UNKNOWN"
		v.Reason = run.Reason
		v.FailureCount = 0
		v.SuccessCount = 0
		if v.UnknownSince == nil {
			v.UnknownSince = &now
		}
	}
	return v
}
func (h *Hub) effectiveHealth(t protocol.Task, now time.Time) TaskHealth {
	v, ok := h.state.Health[t.ID]
	if !ok || v.Revision != revision(t, h.target(t.TargetID)) {
		v = h.newHealth(t, now, "WAITING_FIRST_RESULT")
	}
	if coverageEnabled(t) {
		v = h.nativeHealth(t, now)
	}
	v.Enabled = h.enabled(t)
	v.FailureThreshold, v.RecoveryThreshold = thresholds(t)
	if !v.Enabled {
		v.State = "UNKNOWN"
		v.Reason = "TASK_DISABLED"
		v.FailureCount = 0
		v.SuccessCount = 0
		return v
	}
	reason := ""
	driver := protocol.TaskDriver(t, h.target(t.TargetID))
	session := h.sessions[h.runtimeForTask(t)]
	if !h.storageOK {
		reason = "STORAGE_UNAVAILABLE"
	} else if h.runtimeDisabled(h.runtimeForTask(t)) {
		reason = "PROBE_DISABLED"
	} else if session != nil && protocol.RealDriver(session.Driver) && (!supportsTaskMedia(session, driver) || t.RTC != nil && t.RTC.MediaEdges != nil && !supportsEdgeRouting(session)) {
		reason = "PROBE_CAPABILITY_MISMATCH"
	} else if session != nil && session.ResourceError != "" {
		reason = "PROBE_STORAGE_EXHAUSTED"
	} else if session == nil || session.Driver != driver || now.Sub(session.Seen) > 60*time.Second {
		reason = "PROBE_OFFLINE"
		if h.runtimeDisabled(h.runtimeForTask(t)) {
			reason = "PROBE_DISABLED"
		}
	} else if !coverageEnabled(t) && v.LastResultAt != nil && now.Sub(*v.LastResultAt) > taskFreshness(t) {
		reason = "RESULT_EXPIRED"
	}
	if reason == "" && protocol.IsInteropClient(t.ClientType) {
		for _, client := range []string{"native", "web"} {
			copy := t
			copy.ClientType = client
			id := h.runtimeForTask(copy)
			peer := h.sessions[id]
			if h.runtimeDisabled(id) {
				reason = "PROBE_DISABLED"
				break
			}
			if peer == nil || now.Sub(peer.Seen) > 60*time.Second {
				reason = "PROBE_OFFLINE"
				break
			}
			if peer.ResourceError != "" {
				reason = "PROBE_STORAGE_EXHAUSTED"
				break
			}
			for _, required := range []string{"native_web_interop_v1", "native_aut_web_interop_v1", "native_web_duplex_v1"} {
				capable := false
				for _, cap := range peer.Capabilities {
					if cap == required {
						capable = true
					}
				}
				if !capable {
					reason = "PROBE_CAPABILITY_MISMATCH"
					break
				}
			}
			if reason != "" {
				break
			}
		}
	}
	if reason != "" {
		if coverageEnabled(t) && v.State == "UNHEALTHY" {
			v.InfrastructureReason = reason
			return v
		}
		v.State = "UNKNOWN"
		v.Reason = reason
		v.FailureCount = 0
		v.SuccessCount = 0
		if old, ok := h.state.Health[t.ID]; ok && old.Revision == v.Revision && old.State == "UNKNOWN" && old.UnknownSince != nil {
			v.UnknownSince = old.UnknownSince
		} else if v.UnknownSince == nil {
			v.UnknownSince = &now
		}
	}
	return v
}
func (h *Hub) refreshHealth(now time.Time) {
	changes := map[string]TaskHealth{}
	for _, t := range h.config.Tasks {
		if h.target(t.TargetID).Driver != "agora" {
			continue
		}
		v := h.effectiveHealth(t, now)
		old, ok := h.state.Health[t.ID]
		if !ok || old.State != v.State || old.Reason != v.Reason || old.Enabled != v.Enabled {
			changes[t.ID] = v
		}
	}
	if len(changes) > 0 {
		_ = h.change(func(s *state) error {
			for k, v := range changes {
				s.Health[k] = v
			}
			return nil
		})
	}
}
func (h *Hub) BusinessFacts() []BusinessFact {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	result := []BusinessFact{}
	for _, t := range h.config.Tasks {
		if !h.enabled(t) || h.target(t.TargetID).Driver != "agora" {
			continue
		}
		v := h.effectiveHealth(t, now)
		delay := t.UnknownNotifySeconds
		if delay == 0 {
			delay = 120
		}
		eligible := v.State != "UNKNOWN" || (v.UnknownSince != nil && now.Sub(*v.UnknownSince) >= time.Duration(delay)*time.Second && v.Reason != "FAILURE_CONFIRMING")
		result = append(result, BusinessFact{TaskID: t.ID, TargetID: t.TargetID, Cluster: h.target(t.TargetID).ClusterCode, Revision: v.Revision, RunID: v.LastRunID, Path: taskPath(t), State: v.State, Reason: v.Reason, Notify: eligible, At: v.LastResultAt})
	}
	return result
}

func taskPath(t protocol.Task) string {
	path := t.HostID + " / " + t.ServiceType + " / " + t.ClientType + " → " + t.TargetID
	if t.RTC != nil && t.RTC.MediaEdges != nil {
		path += " [" + t.RTC.MediaEdges.Host + ":" + strconv.Itoa(t.RTC.MediaEdges.StartPort) + "-" + strconv.Itoa(t.RTC.MediaEdges.EndPort) + "]"
	}
	return path
}
