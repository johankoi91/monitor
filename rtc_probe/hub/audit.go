package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type auditedDocuments interface{ RTCCommitState([]byte, []Audit) error }

func healthAudit(before, after *TaskHealth) Audit {
	data, _ := json.Marshal([]*TaskHealth{before, after})
	hash := sha256.Sum256(data)
	a := Audit{ID: hex.EncodeToString(hash[:16]), At: time.Now().UTC(), Actor: "rtc-health", Action: "RTC_HEALTH_CHANGED", Target: after.TargetID, TaskID: after.TaskID, RunID: after.LastRunID, Revision: after.Revision, Outcome: after.State, Before: before, After: after}
	return a
}
func (h *Hub) healthAudits(next state) []Audit {
	var out []Audit
	for id, after := range next.Health {
		before, ok := h.state.Health[id]
		if ok && before.State == after.State && before.Enabled == after.Enabled && before.Revision == after.Revision && (after.State != "UNKNOWN" || before.Reason == after.Reason) {
			continue
		}
		a, b := before, after
		var previous *TaskHealth
		if ok {
			previous = &a
		}
		out = append(out, healthAudit(previous, &b))
	}
	for id, before := range h.state.Health {
		if _, ok := next.Health[id]; ok {
			continue
		}
		a, b := before, before
		b.State = "UNKNOWN"
		b.Enabled = false
		b.Reason = "CONFIG_CHANGED_OR_REMOVED"
		out = append(out, healthAudit(&a, &b))
	}
	return out
}
func (h *Hub) commitAuditedState(s *state, pending []byte) error {
	audits := s.PendingAudits
	if db, ok := h.documents.(auditedDocuments); ok {
		s.PendingAudits = nil
		clean, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return db.RTCCommitState(clean, audits)
	}
	save := func(b []byte) error {
		if h.documents != nil {
			return h.documents.SaveDocument("rtc-probe/state-v1", b)
		}
		return durable.Bytes(filepath.Join(h.dir, "state.json"), b)
	}
	if err := save(pending); err != nil {
		return err
	}
	if len(audits) == 0 {
		return nil
	}
	for _, a := range audits {
		if err := h.archive.RTCPutAudit(a, int64(s.Settings.HistoryMB)<<20); err != nil {
			return err
		}
	}
	s.PendingAudits = nil
	clean, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return save(clean)
}
func (h *Hub) Audits(task string, limit, offset int) (AuditPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return AuditPage{}, errors.New("审计分页参数无效")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.archive.RTCListAudits(task, limit, offset)
}
func (f *fileArchive) RTCListAudits(task string, limit, offset int) (AuditPage, error) {
	page := AuditPage{Audits: []Audit{}, Limit: limit, Offset: offset}
	files, err := os.ReadDir(filepath.Join(f.dir, "audit"))
	if err != nil {
		return page, err
	}
	var rows []Audit
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		var a Audit
		if err := durable.Read(filepath.Join(f.dir, "audit", file.Name()), 1<<20, &a); err != nil {
			return page, err
		}
		if task == "" || a.TaskID == task {
			rows = append(rows, a)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })
	page.Total = len(rows)
	if offset < len(rows) {
		end := offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		page.Audits = rows[offset:end]
	}
	return page, nil
}

// Called with the hub lock held. A log request is already scoped to one probe.
func (h *Hub) receiveLogStatus(probe string, m protocol.Message) (protocol.Message, error) {
	if m.Logs == nil || (m.Logs.Status != "EXPIRED" && m.Logs.Status != "FAILED") {
		return protocol.Message{}, errors.New("invalid log status")
	}
	v, ok := h.logOwner(m.Logs.ID, probe)
	if !ok || v.RunID != m.Logs.RunID || v.Role != m.Logs.Role || m.Logs.ProbeID != probe {
		return protocol.Message{}, errors.New("log status owner mismatch")
	}
	if v.Status == "REQUESTED" {
		if err := h.change(func(s *state) error {
			if s.Runs[v.RunID] == nil {
				r, err := h.archive.RTCGetRun(v.RunID)
				if err != nil {
					return err
				}
				s.Runs[v.RunID] = &r
			}
			v.Status = m.Logs.Status
			if v.Status == "EXPIRED" {
				v.Error = "LOCAL_RETENTION_EXPIRED"
			} else {
				v.Error = "PROBE_LOG_UNAVAILABLE"
				if m.Logs.Error == "SDK_NOT_STARTED" || m.Logs.Error == "LOG_BUNDLE_LIMIT_EXCEEDED" {
					v.Error = m.Logs.Error
				}
			}
			s.Runs[v.RunID].Logs[v.ID] = v
			return nil
		}); err != nil {
			return protocol.Message{}, err
		}
	}
	ack := protocol.Msg("log_status_ack")
	ack.ID = v.ID
	return ack, nil
}
