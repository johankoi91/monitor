package hub

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

const nativeCoverageKind = "NATIVE_COVERAGE"

var nativePhases = []string{"AP", "DIRECT", "CROSS"}

type NativeCaseResult struct {
	Case             protocol.NativeCase     `json:"case"`
	RunID            string                  `json:"run_id,omitempty"`
	Status           string                  `json:"status"`
	Reason           string                  `json:"reason,omitempty"`
	SenderEvidence   *protocol.Evidence      `json:"sender_evidence,omitempty"`
	ReceiverEvidence *protocol.Evidence      `json:"receiver_evidence,omitempty"`
	SenderRoute      *protocol.RouteEvidence `json:"sender_route,omitempty"`
	ReceiverRoute    *protocol.RouteEvidence `json:"receiver_route,omitempty"`
}
type NativePhaseState struct {
	ActiveRunID string     `json:"active_run_id,omitempty"`
	CaseCount   int        `json:"case_count"`
	Health      TaskHealth `json:"health"`
	NextAt      time.Time  `json:"next_at"`
	Round       uint64     `json:"round"`
}
type NativeState struct {
	SchedulingError string                       `json:"scheduling_error,omitempty"`
	WorkerLimit     int                          `json:"worker_limit"`
	CrossTopology   string                       `json:"cross_topology"`
	Version         string                       `json:"coverage_version"`
	Expected        []protocol.NativeMember      `json:"expected"`
	Phases          map[string]*NativePhaseState `json:"phases"`
	Issues          []string                     `json:"issues"`
}

func isSummary(r *Run) bool              { return r.Kind == edgeSweepKind || r.Kind == nativeCoverageKind }
func nativeEnabled(t protocol.Task) bool { return t.RTC != nil && t.RTC.NativeCoverage != nil }
func (h *Hub) SetNativeInventoryProvider(provider func() (protocol.NativeInventory, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nativeInventory = provider
	h.nativeInventoryAt = time.Time{}
}
func (h *Hub) readNativeInventory(now time.Time) {
	if now.Sub(h.nativeInventoryAt) < 5*time.Second {
		return
	}
	h.nativeInventoryAt = now
	if h.nativeInventory == nil {
		h.nativeInventoryErr = errors.New("Native inventory provider unavailable")
		return
	}
	h.nativeSnapshot, h.nativeInventoryErr = h.nativeInventory()
}
func phaseInterval(t protocol.Task, phase string) time.Duration {
	n := t.IntervalSeconds
	if phase == "DIRECT" {
		n = t.SweepIntervalSeconds
	}
	if phase == "CROSS" {
		n = t.CrossIntervalSeconds
	}
	if n <= 0 {
		n = 900
	}
	return time.Duration(n) * time.Second
}
func nativeBudget(t protocol.Task, count int) time.Duration {
	return time.Duration(count) * (time.Duration(t.TimeoutSeconds)*time.Second + protocol.CleanupGrace + protocol.ResultReportGrace + 5*time.Second)
}
func (h *Hub) nativeTaskPlan(t protocol.Task, phase string, now time.Time) protocol.NativePlan {
	h.readNativeInventory(now)
	var prior []protocol.NativeMember
	var round uint64
	if state := h.state.Native[t.ID]; state != nil {
		prior = state.Expected
		if p := state.Phases[phase]; p != nil {
			round = p.Round
		}
	}
	plan := BuildNativePlan(*t.RTC.NativeCoverage, h.nativeSnapshot, prior, phase, round)
	if h.nativeInventoryErr != nil {
		plan.Issues = append(plan.Issues, "NATIVE_INVENTORY_UNAVAILABLE")
	}
	return plan
}
func (h *Hub) activeNativePhase(task, phase string) bool {
	for _, run := range h.state.Runs {
		if run.TaskID == task && run.Kind == nativeCoverageKind && run.NativePlan != nil && run.NativePlan.Phase == phase && !terminal(run.Status) {
			return true
		}
	}
	return false
}
func (h *Hub) startNativePhase(t protocol.Task, phase string, now time.Time) (Run, error) {
	if h.activeNativePhase(t.ID, phase) {
		return Run{}, errors.New("native phase already running")
	}
	plan := h.nativeTaskPlan(t, phase, now)
	if len(plan.Cases) == 0 {
		return Run{}, errors.New("native plan has no cases")
	}
	r := Run{ID: protocol.ID(), TaskID: t.ID, TargetID: t.TargetID, Revision: revision(t, h.target(t.TargetID)), Driver: "agora", Kind: nativeCoverageKind, NativePlan: &plan, Status: "RUNNING", Reason: "NATIVE_COVERAGE_RUNNING", CreatedAt: now, Deadline: now.Add(nativeBudget(t, len(plan.Cases)) + phaseInterval(t, phase)), Endpoints: map[string]*Endpoint{}, Logs: map[string]protocol.LogRequest{}}
	for _, c := range plan.Cases {
		r.Cases = append(r.Cases, NativeCaseResult{Case: c, Status: "PENDING"})
	}
	plan.Cases = nil
	err := h.change(func(s *state) error {
		if s.Native == nil {
			s.Native = map[string]*NativeState{}
		}
		state := s.Native[t.ID]
		if state == nil {
			state = &NativeState{Phases: map[string]*NativePhaseState{}}
			s.Native[t.ID] = state
		}
		if state.Phases == nil {
			state.Phases = map[string]*NativePhaseState{}
		}
		if state.Version != plan.Version {
			for _, p := range state.Phases {
				p.Health = h.newHealth(t, now, "COVERAGE_CHANGED")
				p.NextAt = now
			}
		}
		state.Version = plan.Version
		state.WorkerLimit = h.nativeWorkerLimit(t, len(plan.Members))
		state.CrossTopology = "ring"
		state.Expected = plan.Members
		state.Issues = plan.Issues
		p := state.Phases[phase]
		if p == nil {
			p = &NativePhaseState{Health: h.newHealth(t, now, "WAITING_FIRST_RESULT")}
			state.Phases[phase] = p
		}
		p.NextAt = now.Add(phaseInterval(t, phase))
		p.CaseCount = len(r.Cases)
		p.ActiveRunID = r.ID
		s.Runs[r.ID] = &r
		return nil
	})
	return r, err
}
func (h *Hub) startNativeCoverage(t protocol.Task, now time.Time) (Run, error) {
	if !h.enabled(t) {
		return Run{}, errors.New("task disabled")
	}
	var first Run
	for _, phase := range nativePhases {
		if phase == "CROSS" && !t.RTC.NativeCoverage.CrossEdge {
			continue
		}
		if h.activeNativePhase(t.ID, phase) {
			continue
		}
		r, err := h.startNativePhase(t, phase, now)
		if err != nil {
			return Run{}, err
		}
		if first.ID == "" {
			first = r
		}
	}
	if first.ID == "" {
		return Run{}, errors.New("task already running")
	}
	h.advanceNative(now)
	return first, nil
}
func (h *Hub) tickNative(now time.Time) {
	for _, t := range h.config.Tasks {
		if !nativeEnabled(t) || !h.enabled(t) {
			continue
		}
		plan := h.nativeTaskPlan(t, "AP", now)
		current := h.state.Native[t.ID]
		changed := current == nil || current.Version != plan.Version
		workerLimit := h.nativeWorkerLimit(t, len(plan.Members))
		issuesChanged := current != nil && (fingerprint(current.Issues) != fingerprint(plan.Issues) || current.WorkerLimit != workerLimit)
		if changed || issuesChanged {
			_ = h.change(func(s *state) error {
				if s.Native == nil {
					s.Native = map[string]*NativeState{}
				}
				value := s.Native[t.ID]
				if value == nil {
					value = &NativeState{Phases: map[string]*NativePhaseState{}}
					s.Native[t.ID] = value
				}
				if changed || len(plan.Issues) > 0 && len(value.Issues) == 0 {
					for _, p := range value.Phases {
						if changed || p.Health.State != "UNHEALTHY" {
							p.Health = h.newHealth(t, now, "COVERAGE_CHANGED")
						}
						p.NextAt = now
					}
				}
				value.Version = plan.Version
				value.WorkerLimit = workerLimit
				value.CrossTopology = "ring"
				value.Expected = plan.Members
				value.Issues = plan.Issues
				return nil
			})
		}
		effective := append([]protocol.Task(nil), h.config.Tasks...)
		for i := range effective {
			effective[i].Enabled = h.enabled(effective[i])
		}
		budgetErr := ""
		if err := h.validateNativeBudget(effective, h.config.Targets); err != nil {
			budgetErr = err.Error()
		}
		if value := h.state.Native[t.ID]; value != nil && value.SchedulingError != budgetErr {
			_ = h.change(func(s *state) error {
				value := s.Native[t.ID]
				value.SchedulingError = budgetErr
				if budgetErr != "" {
					for phase, p := range value.Phases {
						if phase != "AP" && p.Health.State != "UNHEALTHY" {
							p.Health.State = "UNKNOWN"
							p.Health.Reason = "NATIVE_SCHEDULE_BUDGET_EXCEEDED"
						}
					}
				}
				return nil
			})
		}
		for _, phase := range nativePhases {
			if phase != "AP" && h.state.Native[t.ID].SchedulingError != "" {
				continue
			}
			if phase == "CROSS" && !t.RTC.NativeCoverage.CrossEdge {
				continue
			}
			if h.activeNativePhase(t.ID, phase) {
				continue
			}
			state := h.state.Native[t.ID]
			if state != nil && state.Phases[phase] != nil && now.Before(state.Phases[phase].NextAt) {
				continue
			}
			_, _ = h.startNativePhase(t, phase, now)
		}
	}
	h.advanceNative(now)
}
func (h *Hub) advanceNative(now time.Time) {
	var parents []*Run
	for _, r := range h.state.Runs {
		if r.Kind == nativeCoverageKind && !terminal(r.Status) {
			parents = append(parents, r)
		}
	}
	priority := map[string]int{"AP": 0, "DIRECT": 1, "CROSS": 2}
	sort.Slice(parents, func(i, j int) bool {
		a, b := parents[i], parents[j]
		if priority[a.NativePlan.Phase] != priority[b.NativePlan.Phase] {
			return priority[a.NativePlan.Phase] < priority[b.NativePlan.Phase]
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	for _, parent := range parents {
		for {
			parent = h.state.Runs[parent.ID]
			task, ok := h.task(parent.TaskID)
			if !ok || !h.enabled(task) || !nativeEnabled(task) {
				break
			}
			if current := h.state.Native[task.ID]; current == nil || current.Version != parent.NativePlan.Version || parent.Revision != revision(task, h.target(task.TargetID)) {
				_ = h.change(func(s *state) error {
					h.stopNative(s, s.Runs[parent.ID], now, "COVERAGE_CHANGED", "CANCELLED")
					return nil
				})
				break
			}
			if now.After(parent.Deadline) {
				_ = h.change(func(s *state) error {
					h.stopNative(s, s.Runs[parent.ID], now, "NATIVE_PLAN_TIMEOUT", "ERROR")
					return nil
				})
				break
			}
			active, pending := false, -1
			for i, c := range parent.Cases {
				if c.Status == "PENDING" && pending < 0 {
					pending = i
				}
				if c.Status == "PREPARING" || c.Status == "RUNNING" {
					active = true
				}
			}
			if pending < 0 {
				if !active {
					h.finishNative(parent.ID, now)
				}
				break
			}
			c := parent.Cases[pending].Case
			if c.Unavailable != "" {
				_ = h.change(func(s *state) error {
					r := s.Runs[parent.ID]
					r.Cases[pending].Status = "ERROR"
					r.Cases[pending].Reason = c.Unavailable
					return nil
				})
				continue
			}
			session := h.sessions[task.ProbeID]
			capable := false
			if session != nil {
				for _, cap := range session.Capabilities {
					if cap == "native_routes_v1" {
						capable = true
					}
				}
			}
			if !capable && session != nil && session.Driver == "agora" {
				_ = h.change(func(s *state) error {
					r := s.Runs[parent.ID]
					r.Cases[pending].Status = "ERROR"
					r.Cases[pending].Reason = "PROBE_NATIVE_CAPABILITY_UNAVAILABLE"
					return nil
				})
				continue
			}
			if err := h.admitProbe(task, now, parent.ID); err != nil {
				break
			}
			c.Candidates = nil
			for _, member := range parent.NativePlan.Members {
				if member.Group == c.APGroup {
					c.Candidates = append(c.Candidates, member.Edge)
				}
			}
			run, err := h.startProbeSpec(task.ID, now, "", parent.ID, &c)
			if err != nil {
				break
			}
			_ = h.change(func(s *state) error {
				r := s.Runs[parent.ID]
				r.Cases[pending].RunID = run.ID
				r.Cases[pending].Status = "PREPARING"
				return nil
			})
		}
	}
}
func (h *Hub) recordNativeChild(s *state, child *Run, now time.Time) bool {
	parent := s.Runs[child.ParentRunID]
	if parent == nil || parent.Kind != nativeCoverageKind {
		return false
	}
	if terminal(parent.Status) {
		return true
	}
	for i := range parent.Cases {
		row := &parent.Cases[i]
		if row.RunID != child.ID {
			continue
		}
		row.Status = child.Status
		row.Reason = child.Reason
		if e := child.Endpoints["sender"]; e != nil && e.Result != nil {
			v := e.Result.Evidence
			row.SenderEvidence = &v
			row.SenderRoute = e.Result.Route
		}
		if e := child.Endpoints["receiver"]; e != nil && e.Result != nil {
			v := e.Result.Evidence
			row.ReceiverEvidence = &v
			row.ReceiverRoute = e.Result.Route
		}
	}
	return true
}
func (h *Hub) finishNative(id string, now time.Time) {
	_ = h.change(func(s *state) error {
		r := s.Runs[id]
		if r == nil || terminal(r.Status) {
			return nil
		}
		r.Status = "SUCCESS"
		r.Reason = "NATIVE_PHASE_VERIFIED"
		r.FinishedAt = &now
		for _, c := range r.Cases {
			if c.Status == "FAILED" {
				r.Status = "FAILED"
				r.Reason = c.Reason
				break
			}
			if c.Status != "SUCCESS" {
				r.Status = "ERROR"
				r.Reason = c.Reason
			}
		}
		if r.Status == "SUCCESS" && len(r.NativePlan.Issues) > 0 {
			r.Status = "ERROR"
			r.Reason = r.NativePlan.Issues[0]
		}
		h.applyNativePhaseResult(s, r, now)
		return nil
	})
}
func (h *Hub) applyNativePhaseResult(s *state, r *Run, now time.Time) {
	task, ok := h.task(r.TaskID)
	current := s.Native[r.TaskID]
	enabled := ok && h.enabled(task)
	if value, exists := s.Enabled[r.TaskID]; exists {
		enabled = value
	}
	if !ok || !enabled || !nativeEnabled(task) || current == nil || r.NativePlan == nil || current.Version != r.NativePlan.Version || r.Revision != revision(task, h.target(task.TargetID)) {
		return
	}
	phase := current.Phases[r.NativePlan.Phase]
	if phase == nil || phase.Health.LastRunID == r.ID {
		return
	}
	phase.Health = advanceHealth(phase.Health, task, r, now)
	if phase.ActiveRunID == r.ID {
		phase.ActiveRunID = ""
	}
	phase.NextAt = now.Add(phaseInterval(task, r.NativePlan.Phase))
	if r.Status != "CANCELLED" {
		phase.Round++
	}
}
func (h *Hub) stopNative(s *state, parent *Run, now time.Time, reason, status string) {
	if parent == nil {
		return
	}
	parent.Status = status
	parent.Reason = reason
	parent.FinishedAt = &now
	for i := range parent.Cases {
		row := &parent.Cases[i]
		if !terminal(row.Status) {
			row.Status = status
			row.Reason = reason
		}
		if child := s.Runs[row.RunID]; child != nil && !terminal(child.Status) {
			child.Status = status
			child.Reason = reason
			child.FinishedAt = &now
		}
	}
	h.applyNativePhaseResult(s, parent, now)
}
func (h *Hub) nativeHealth(t protocol.Task, now time.Time) TaskHealth {
	out := h.newHealth(t, now, "NATIVE_COVERAGE_PENDING")
	if old, ok := h.state.Health[t.ID]; ok && old.Revision == out.Revision {
		out.LastSuccessAt = old.LastSuccessAt
		if old.State == "UNKNOWN" && old.UnknownSince != nil {
			out.UnknownSince = old.UnknownSince
		}
	}
	state := h.state.Native[t.ID]
	if state == nil {
		return out
	}
	healthy, unknown := true, false
	if state.SchedulingError != "" {
		healthy = false
		unknown = true
		out.Reason = "NATIVE_SCHEDULE_BUDGET_EXCEEDED"
	}
	if len(state.Issues) > 0 {
		healthy = false
		unknown = true
		out.Reason = state.Issues[0]
	}
	for _, phase := range nativePhases {
		if phase == "CROSS" && !t.RTC.NativeCoverage.CrossEdge {
			continue
		}
		p := state.Phases[phase]
		if p == nil {
			unknown = true
			healthy = false
			continue
		}
		value := p.Health
		if value.Revision != revision(t, h.target(t.TargetID)) {
			unknown = true
			healthy = false
			continue
		}
		if out.State != "UNHEALTHY" && value.LastResultAt != nil && (out.LastResultAt == nil || value.LastResultAt.After(*out.LastResultAt)) {
			out.LastRunID = value.LastRunID
			out.LastResultAt = value.LastResultAt
		}
		budget := phaseInterval(t, phase)*2 + nativeBudget(t, p.CaseCount)
		if value.LastResultAt == nil || now.Sub(*value.LastResultAt) > budget {
			unknown = true
			healthy = false
			continue
		}
		if value.State == "UNHEALTHY" {
			if out.State != "UNHEALTHY" || out.LastResultAt == nil || value.LastResultAt.After(*out.LastResultAt) {
				out.LastRunID = value.LastRunID
				out.LastResultAt = value.LastResultAt
				out.Reason = "NATIVE_" + phase + "_FAILED"
				out.FailureCount = value.FailureCount
			}
			out.State = "UNHEALTHY"
			healthy = false
			continue
		}
		if value.State != "HEALTHY" || value.LastResultAt == nil || now.Sub(*value.LastResultAt) > budget {
			unknown = true
			healthy = false
		}
	}
	if out.State == "UNHEALTHY" {
		out.UnknownSince = nil
		return out
	}
	if healthy {
		out.State = "HEALTHY"
		out.Reason = "NATIVE_COVERAGE_VERIFIED"
		out.LastSuccessAt = out.LastResultAt
		out.UnknownSince = nil
		return out
	}
	if unknown {
		out.State = "UNKNOWN"
	}
	return out
}
func (h *Hub) nativeSnapshotJSON() json.RawMessage {
	data, _ := json.Marshal(h.state.Native)
	return data
}
