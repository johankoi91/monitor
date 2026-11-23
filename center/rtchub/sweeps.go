package hub

import (
	"errors"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"time"
)

const edgeSweepKind = "EDGE_SWEEP"

type EdgeResult struct {
	Endpoint         string             `json:"endpoint"`
	RunID            string             `json:"run_id,omitempty"`
	Status           string             `json:"status"`
	Reason           string             `json:"reason,omitempty"`
	StartedAt        *time.Time         `json:"started_at,omitempty"`
	FinishedAt       *time.Time         `json:"finished_at,omitempty"`
	SenderEvidence   *protocol.Evidence `json:"sender_evidence,omitempty"`
	ReceiverEvidence *protocol.Evidence `json:"receiver_evidence,omitempty"`
	Cleanup          string             `json:"cleanup,omitempty"`
}

func supportsEdgeRouting(s *session) bool {
	if s == nil {
		return false
	}
	for _, c := range s.Capabilities {
		if c == "rtc_vos_list_v1" {
			return true
		}
	}
	return false
}
func sweepBudget(t protocol.Task) time.Duration {
	n := 1
	if t.RTC != nil && t.RTC.MediaEdges != nil {
		n = len(t.RTC.MediaEdges.Endpoints())
	}
	return time.Duration(n) * (time.Duration(t.TimeoutSeconds)*time.Second + 2*(protocol.CleanupGrace+protocol.ResultReportGrace) + 5*time.Second)
}
func taskFreshness(t protocol.Task) time.Duration {
	interval := time.Duration(t.IntervalSeconds) * time.Second
	if t.RTC != nil && t.RTC.MediaEdges != nil {
		if budget := sweepBudget(t); budget > interval {
			interval = budget
		}
	}
	return interval*2 + time.Duration(t.TimeoutSeconds)*time.Second
}
func (h *Hub) admitProbe(t protocol.Task, now time.Time, parentID string) error {
	if !h.enabled(t) {
		return errors.New("task disabled")
	}
	nativeParent := h.state.Runs[parentID]
	parallelNative := coverageEnabled(t) && nativeParent != nil && nativeParent.Kind == nativeCoverageKind && nativeParent.TaskID == t.ID && !terminal(nativeParent.Status)
	taskUsed := 0
	active := 0
	used := map[string]int{}
	for _, r := range h.state.Runs {
		if r.Kind == edgeSweepKind && !terminal(r.Status) && r.TaskID == t.ID && r.ID != parentID {
			return errors.New("task already running")
		}
		if runOccupiesSlot(r, now) {
			active++
			if r.TaskID == t.ID {
				if !parallelNative || r.NativeCase == nil {
					return errors.New("task already running")
				}
				taskUsed++
			}
			seen := map[string]bool{}
			for _, e := range r.Endpoints {
				if !seen[e.Job.RuntimeID] {
					used[e.Job.RuntimeID]++
					seen[e.Job.RuntimeID] = true
				}
			}
		}
	}
	members := 0
	if state := h.state.Native[t.ID]; state != nil {
		members = len(state.Expected)
	}
	if parallelNative && taskUsed >= h.nativeWorkerLimit(t, members) {
		return errors.New("native task worker limit reached")
	}
	if active >= h.config.MaxConcurrentRuns {
		return errors.New("RTC capacity exhausted")
	}
	target := h.target(t.TargetID)
	target.Driver = protocol.TaskDriver(t, target)
	for _, id := range []string{h.runtimeForTask(t)} {
		s := h.sessions[id]
		if protocol.RealDriver(target.Driver) && s != nil && !supportsTaskMedia(s, target.Driver) {
			return errors.New("probe audio/video capability unavailable")
		}
		if t.RTC != nil && t.RTC.MediaEdges != nil && !supportsEdgeRouting(s) {
			return errors.New("探针版本不支持指定 UDP edge，请升级探针")
		}
		if h.runtimeDisabled(id) || s == nil || s.ResourceError != "" || time.Since(s.Seen) > 60*time.Second || s.Driver != target.Driver || s.Capacity <= used[id] {
			return errors.New("probe offline or capacity unavailable")
		}
	}
	return nil
}
func (h *Hub) startSweepLocked(taskID string, now time.Time) (Run, error) {
	if !h.storageOK {
		if err := h.change(func(*state) error { return nil }); err != nil {
			return Run{}, err
		}
	}
	t, ok := h.task(taskID)
	if !ok || t.RTC == nil || t.RTC.MediaEdges == nil {
		return Run{}, errors.New("edge sweep not configured")
	}
	if err := h.admitProbe(t, now, ""); err != nil {
		return Run{}, err
	}
	r := Run{Kind: edgeSweepKind, ID: protocol.ID(), TaskID: t.ID, TargetID: t.TargetID, Revision: revision(t, h.target(t.TargetID)), Driver: "agora", Status: "RUNNING", Reason: "MEDIA_EDGE_SWEEP_RUNNING", CreatedAt: now, Deadline: now.Add(sweepBudget(t)), Endpoints: map[string]*Endpoint{}, Logs: map[string]protocol.LogRequest{}}
	for _, endpoint := range t.RTC.MediaEdges.Endpoints() {
		r.Edges = append(r.Edges, EdgeResult{Endpoint: endpoint, Status: "PENDING"})
	}
	if err := h.change(func(s *state) error { s.Runs[r.ID] = &r; return nil }); err != nil {
		return Run{}, err
	}
	if _, err := h.startProbeLocked(taskID, now, r.Edges[0].Endpoint, r.ID); err != nil {
		_ = h.change(func(s *state) error {
			parent := s.Runs[r.ID]
			h.stopSweep(s, parent, now, "SWEEP_DISPATCH_FAILED", "ERROR")
			h.applyHealth(s, parent, now)
			return nil
		})
		return Run{}, err
	}
	return *h.state.Runs[r.ID], nil
}
func (h *Hub) recordEdgeResult(s *state, child *Run, now time.Time) {
	parent := s.Runs[child.ParentRunID]
	if parent == nil || terminal(parent.Status) {
		return
	}
	for i := range parent.Edges {
		edge := &parent.Edges[i]
		if edge.RunID != child.ID || edge.Endpoint != child.EdgeEndpoint {
			continue
		}
		edge.Status = child.Status
		edge.Reason = child.Reason
		edge.FinishedAt = child.FinishedAt
		cleanup := "SUCCESS"
		for role, endpoint := range child.Endpoints {
			if endpoint.Result == nil {
				cleanup = "UNKNOWN"
				continue
			}
			e := endpoint.Result.Evidence
			if role == "sender" {
				edge.SenderEvidence = &e
			} else {
				edge.ReceiverEvidence = &e
			}
			if endpoint.Result.Cleanup != "SUCCESS" {
				cleanup = endpoint.Result.Cleanup
			}
		}
		edge.Cleanup = cleanup
	}
	done := true
	failed, unknown := false, false
	for _, edge := range parent.Edges {
		if !terminal(edge.Status) {
			done = false
		}
		failed = failed || edge.Status == "FAILED"
		unknown = unknown || edge.Status == "ERROR" || edge.Status == "CANCELLED"
	}
	if !done {
		return
	}
	parent.Status = "SUCCESS"
	parent.Reason = "ALL_MEDIA_EDGES_VERIFIED"
	if failed {
		parent.Status = "FAILED"
		parent.Reason = "MEDIA_EDGE_FAILED"
	} else if unknown {
		parent.Status = "ERROR"
		parent.Reason = "MEDIA_EDGE_INCOMPLETE"
	}
	parent.FinishedAt = &now
	h.applyHealth(s, parent, now)
}
func (h *Hub) stopSweep(s *state, parent *Run, now time.Time, reason, status string) {
	parent.Status = status
	parent.Reason = reason
	parent.FinishedAt = &now
	for i := range parent.Edges {
		edge := &parent.Edges[i]
		if !terminal(edge.Status) {
			edge.Status = status
			edge.Reason = reason
			edge.FinishedAt = &now
		}
	}
	for _, child := range s.Runs {
		if child.ParentRunID == parent.ID && !terminal(child.Status) {
			child.Status = status
			child.Reason = reason
			child.FinishedAt = &now
		}
	}
}
func (h *Hub) advanceSweeps(now time.Time) {
	var parents []string
	for id, r := range h.state.Runs {
		if r.Kind == edgeSweepKind && !terminal(r.Status) {
			parents = append(parents, id)
		}
	}
	for _, id := range parents {
		parent := h.state.Runs[id]
		if parent == nil || terminal(parent.Status) {
			continue
		}
		task, ok := h.task(parent.TaskID)
		if !ok || !h.enabled(task) || parent.Revision != revision(task, h.target(parent.TargetID)) {
			_ = h.change(func(s *state) error { h.stopSweep(s, s.Runs[id], now, "CONFIG_CHANGED", "CANCELLED"); return nil })
			continue
		}
		if now.After(parent.Deadline) {
			_ = h.change(func(s *state) error {
				p := s.Runs[id]
				h.stopSweep(s, p, now, "SWEEP_DEADLINE_EXCEEDED", "ERROR")
				h.applyHealth(s, p, now)
				return nil
			})
			continue
		}
		busy := false
		for _, child := range h.state.Runs {
			if child.ParentRunID == id && runOccupiesSlot(child, now) {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		endpoint := ""
		for _, edge := range parent.Edges {
			if edge.Status == "PENDING" {
				endpoint = edge.Endpoint
				break
			}
		}
		if endpoint == "" {
			continue
		}
		_, _ = h.startProbeLocked(parent.TaskID, now, endpoint, id)
	}
}
