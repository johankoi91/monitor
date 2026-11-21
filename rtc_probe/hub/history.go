package hub

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type Settings struct {
	Version             string `json:"version"`
	HistoryDays         int    `json:"history_days"`
	HistoryMB           int    `json:"history_mb"`
	LogsMB              int    `json:"logs_mb"`
	ProbeSuccessMinutes int    `json:"probe_success_minutes"`
	ProbeFailureMinutes int    `json:"probe_failure_minutes"`
	ProbeDiskMB         int    `json:"probe_disk_mb"`
}
type HistoryPage struct {
	Runs   []Run `json:"runs"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}
type HistoryUsage struct {
	Runs  int64 `json:"runs"`
	Bytes int64 `json:"bytes"`
}
type Archive interface {
	RTCInit() error
	RTCPutRun(Run, int64) error
	RTCGetRun(string) (Run, error)
	RTCGetLog(string) (protocol.LogRequest, error)
	RTCListRuns(string, int, int) (HistoryPage, error)
	RTCListFilteredRuns(HistoryFilter, int, int) (HistoryPage, error)
	RTCUsage() (HistoryUsage, error)
	RTCCleanup(time.Time, int) error
}

func defaultSettings() Settings {
	return Settings{Version: protocol.ID(), HistoryDays: 7, HistoryMB: 2048, LogsMB: 2048, ProbeSuccessMinutes: 60, ProbeFailureMinutes: 1440, ProbeDiskMB: 1024}
}
func (h *Hub) UpdateSettings(expected string, v Settings) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if expected != h.state.Settings.Version {
		return errors.New("保留设置已变化，请刷新")
	}
	if v.ProbeDiskMB == 0 {
		v.ProbeSuccessMinutes = 60
		v.ProbeFailureMinutes = 1440
		v.ProbeDiskMB = 1024
	}
	if err := (protocol.RetentionPolicy{Version: "validate", SuccessMinutes: v.ProbeSuccessMinutes, FailureMinutes: v.ProbeFailureMinutes, DiskMB: v.ProbeDiskMB}).Validate(); err != nil {
		return err
	}
	if v.HistoryDays < 1 || v.HistoryDays > 365 || v.HistoryMB < 16 || v.HistoryMB > 8192 || v.LogsMB < 16 || v.LogsMB > 8192 {
		return errors.New("保留天数或容量无效")
	}
	v.Version = protocol.ID()
	return h.change(func(s *state) error { s.Settings = v; return nil })
}

type HistoryFilter struct {
	TaskID     string
	ClientType string
	Status     string
}

func (h *Hub) History(task string, limit, offset int) (HistoryPage, error) {
	return h.FilteredHistory(HistoryFilter{TaskID: task}, limit, offset)
}
func (h *Hub) FilteredHistory(filter HistoryFilter, limit, offset int) (HistoryPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return HistoryPage{}, errors.New("历史分页参数无效")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	page, err := h.archive.RTCListFilteredRuns(filter, limit, offset)
	for i := range page.Runs {
		h.decorateRun(&page.Runs[i])
	}
	return page, err
}
func (h *Hub) Usage() (HistoryUsage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.archive.RTCUsage()
}
func (h *Hub) maintenance(now time.Time) {
	if now.Sub(h.lastMaintenance) < time.Minute {
		return
	}
	h.lastMaintenance = now
	h.cleanStagedUploads(now)
	if err := h.archive.RTCCleanup(now.Add(-time.Duration(h.state.Settings.HistoryDays)*24*time.Hour), 100); err != nil {
		h.storageOK = false
		h.storageInterrupted = true
	}
	entries, _ := os.ReadDir(filepath.Join(h.dir, "logs"))
	type candidate struct {
		path  string
		at    time.Time
		bytes int64
	}
	var files []candidate
	var total int64
	cutoff := now.Add(-time.Duration(h.state.Settings.HistoryDays) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, candidate{filepath.Join(h.dir, "logs", e.Name()), info.ModTime(), info.Size()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	removed := 0
	for _, f := range files {
		if removed >= 100 {
			break
		}
		if f.at.Before(cutoff) || total > int64(h.state.Settings.LogsMB)<<20 {
			if os.Remove(f.path) == nil {
				total -= f.bytes
				removed++
			}
		}
	}
}

type fileArchive struct{ dir string }

func (f *fileArchive) RTCInit() error {
	if err := os.MkdirAll(filepath.Join(f.dir, "runs"), 0700); err != nil {
		return err
	}
	return nil
}
func (f *fileArchive) RTCPutRun(v Run, max int64) error {
	b, _ := json.Marshal(v)
	old, _ := os.Stat(filepath.Join(f.dir, "runs", v.ID+".json"))
	usage, err := f.RTCUsage()
	if err != nil {
		return err
	}
	size := int64(0)
	if old != nil {
		size = old.Size()
	}
	if usage.Bytes-size+int64(len(b)) > max {
		return errors.New("RTC 历史容量已达到上限")
	}
	return durable.Bytes(filepath.Join(f.dir, "runs", v.ID+".json"), b)
}
func (f *fileArchive) RTCGetRun(id string) (Run, error) {
	var r Run
	if !protocol.ValidID(id) {
		return r, errors.New("invalid run")
	}
	err := durable.Read(filepath.Join(f.dir, "runs", id+".json"), 4<<20, &r)
	return r, err
}
func (f *fileArchive) RTCGetLog(id string) (protocol.LogRequest, error) {
	entries, err := os.ReadDir(filepath.Join(f.dir, "runs"))
	if err != nil {
		return protocol.LogRequest{}, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, err := f.RTCGetRun(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return protocol.LogRequest{}, err
		}
		if v, ok := r.Logs[id]; ok {
			return v, nil
		}
	}
	return protocol.LogRequest{}, os.ErrNotExist
}
func (h *Hub) decorateRun(r *Run) {
	logs := map[string]protocol.LogRequest{}
	for id, v := range r.Logs {
		if v.Status == "AVAILABLE" {
			if _, err := os.Stat(filepath.Join(h.dir, "logs", id+".zip")); err != nil {
				v.Status = "EXPIRED"
			}
		}
		logs[id] = v
	}
	r.Logs = logs
}
func (f *fileArchive) RTCListRuns(task string, limit, offset int) (HistoryPage, error) {
	return f.RTCListFilteredRuns(HistoryFilter{TaskID: task}, limit, offset)
}
func (f *fileArchive) RTCListFilteredRuns(filter HistoryFilter, limit, offset int) (HistoryPage, error) {
	out := HistoryPage{Runs: []Run{}, Limit: limit, Offset: offset}
	entries, err := os.ReadDir(filepath.Join(f.dir, "runs"))
	if err != nil {
		return out, err
	}
	var rows []Run
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, err := f.RTCGetRun(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return out, err
		}
		if r.ParentRunID == "" && (filter.TaskID == "" || r.TaskID == filter.TaskID) && (filter.Status == "" || r.Status == filter.Status) && (filter.ClientType == "" || filter.ClientType == "web" && r.Driver == protocol.WebDriver || filter.ClientType == "native" && r.Driver != protocol.WebDriver && !protocol.IsInteropClient(r.ClientType) || (filter.ClientType == protocol.InteropClient && protocol.IsInteropClient(r.ClientType) || filter.ClientType == protocol.InteropAUTClient && r.ClientType == protocol.InteropAUTClient)) {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	out.Total = len(rows)
	if offset < len(rows) {
		end := offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		out.Runs = rows[offset:end]
	}
	return out, nil
}
func (f *fileArchive) RTCUsage() (HistoryUsage, error) {
	var out HistoryUsage
	entries, err := os.ReadDir(filepath.Join(f.dir, "runs"))
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return out, err
		}
		out.Bytes += info.Size()
		var metadata struct {
			ParentRunID string `json:"parent_run_id"`
		}
		if err := durable.Read(filepath.Join(f.dir, "runs", entry.Name()), 4<<20, &metadata); err != nil {
			return out, err
		}
		if metadata.ParentRunID == "" {
			out.Runs++
		}
	}
	return out, nil
}
func (f *fileArchive) RTCCleanup(cutoff time.Time, batch int) error {
	entries, err := os.ReadDir(filepath.Join(f.dir, "runs"))
	if err != nil {
		return err
	}
	removed := 0
	for _, entry := range entries {
		if removed >= batch {
			break
		}
		path := filepath.Join(f.dir, "runs", entry.Name())
		var run Run
		if err := durable.Read(path, 4<<20, &run); err != nil {
			return err
		}
		if run.ParentRunID != "" {
			if parent, err := f.RTCGetRun(run.ParentRunID); err == nil && (parent.FinishedAt == nil || !parent.FinishedAt.Before(cutoff)) {
				continue
			}
		}
		if run.FinishedAt != nil && run.FinishedAt.Before(cutoff) {
			if err := os.Remove(path); err != nil {
				return err
			}
			removed++
		}
	}
	return nil
}
