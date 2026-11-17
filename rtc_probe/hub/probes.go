package hub

import (
	"errors"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"sort"
	"time"
)

func (h *Hub) probesLocked() []ProbeConfig {
	values := map[string]ProbeConfig{}
	for _, p := range h.config.Probes {
		values[p.ID] = p
	}
	for id, p := range h.state.Registered {
		values[id] = p
	}
	out := []ProbeConfig{}
	for _, p := range values {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (h *Hub) RegisterProbe(p ProbeConfig) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !protocol.ValidID(p.ID) || p.Location == "" || len(p.Location) > 200 || len(p.Name) > 128 {
		return "", errors.New("探针标识、名称或位置无效")
	}
	for _, old := range h.probesLocked() {
		if old.ID == p.ID {
			return "", errors.New("探针标识已存在")
		}
	}
	p.Secret = protocol.ID() + protocol.ID()
	if err := h.change(func(s *state) error { s.Registered[p.ID] = p; return nil }); err != nil {
		return "", err
	}
	return p.Secret, nil
}
func (h *Hub) EnableProbe(id string, enabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	found := false
	for _, p := range h.probesLocked() {
		if p.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("探针不存在")
	}
	now := time.Now().UTC()
	if err := h.change(func(s *state) error {
		s.DisabledProbes[id] = !enabled
		if !enabled {
			for _, r := range s.Runs {
				if terminal(r.Status) {
					continue
				}
				if r.Kind == nativeCoverageKind {
					h.stopNative(s, r, now, "PROBE_DISABLED", "CANCELLED")
					continue
				}
				if r.Kind == edgeSweepKind {
					if task, ok := h.task(r.TaskID); ok && task.ProbeID == id {
						h.stopSweep(s, r, now, "PROBE_DISABLED", "CANCELLED")
					}
					continue
				}
				if r.Endpoints["sender"].Job.ProbeID == id || r.Endpoints["receiver"].Job.ProbeID == id {
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
