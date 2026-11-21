package hub

import (
	"errors"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"sort"
	"time"
)

func (h *Hub) runtimesLocked() []RuntimeRegistration {
	out := make([]RuntimeRegistration, 0, len(h.state.Runtimes))
	for _, runtime := range h.state.Runtimes {
		out = append(out, runtime)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func clientKey(host, service, client string) string { return host + "/" + service + "/" + client }
func (h *Hub) runtimeForTask(task protocol.Task) string {
	if protocol.IsInteropClient(task.ClientType) {
		task.ClientType = "native"
	}
	for _, r := range h.state.Runtimes {
		if r.HostID == task.HostID && r.ServiceType == task.ServiceType && r.ClientType == task.ClientType {
			return r.ID
		}
	}
	return ""
}
func (h *Hub) runtimeDisabled(id string) bool {
	r, ok := h.state.Runtimes[id]
	return !ok || h.state.DisabledClients[clientKey(r.HostID, r.ServiceType, r.ClientType)]
}
func (h *Hub) EnableProbe(id string, enabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	found := false
	for _, p := range h.runtimesLocked() {
		if p.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("探针不存在")
	}
	now := time.Now().UTC()
	if err := h.change(func(s *state) error {
		runtime := s.Runtimes[id]
		s.DisabledClients[clientKey(runtime.HostID, runtime.ServiceType, runtime.ClientType)] = !enabled
		if !enabled {
			for _, r := range s.Runs {
				if terminal(r.Status) {
					continue
				}
				if r.Kind == nativeCoverageKind {
					task, ok := h.task(r.TaskID)
					if !ok || !h.taskUsesRuntime(task, id) {
						continue
					}
					h.stopNative(s, r, now, "PROBE_DISABLED", "CANCELLED")
					continue
				}
				if r.Kind == edgeSweepKind {
					if task, ok := h.task(r.TaskID); ok && h.runtimeForTask(task) == id {
						h.stopSweep(s, r, now, "PROBE_DISABLED", "CANCELLED")
					}
					continue
				}
				if r.Endpoints["sender"].Job.RuntimeID == id || r.Endpoints["receiver"].Job.RuntimeID == id {
					r.Status = "CANCELLED"
					r.Reason = "PROBE_DISABLED"
					r.FinishedAt = &now

				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
