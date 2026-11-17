// Package hub coordinates independent RTC probes. Mock evidence never establishes RTC health.
package hub

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type ProbeConfig struct {
	Name        string `yaml:"name"`
	ID          string `yaml:"probe_id"`
	Secret      string `yaml:"secret"`
	Location    string `yaml:"location"`
	HostAddress string `yaml:"host_address"`
}
type Config struct {
	Probes            []ProbeConfig     `yaml:"probes"`
	Targets           []protocol.Target `yaml:"targets"`
	Tasks             []protocol.Task   `yaml:"tasks"`
	MaxConcurrentRuns int               `yaml:"max_concurrent_runs"`
}

// Documents is also implemented by the existing center PostgreSQL backend.
type Documents interface {
	LoadDocument(string) ([]byte, bool, error)
	SaveDocument(string, []byte) error
}
type Endpoint struct {
	Job    protocol.Job     `json:"job"`
	Ready  bool             `json:"ready"`
	Events []protocol.Event `json:"events"`
	Result *protocol.Result `json:"result,omitempty"`
}
type Run struct {
	Kind         string                         `json:"kind,omitempty"`
	ParentRunID  string                         `json:"parent_run_id,omitempty"`
	EdgeEndpoint string                         `json:"edge_endpoint,omitempty"`
	Edges        []EdgeResult                   `json:"edges,omitempty"`
	ID           string                         `json:"run_id"`
	TaskID       string                         `json:"task_id"`
	TargetID     string                         `json:"target_id"`
	Revision     string                         `json:"config_revision"`
	Driver       string                         `json:"driver"`
	Status       string                         `json:"status"`
	Reason       string                         `json:"reason"`
	CreatedAt    time.Time                      `json:"created_at"`
	Deadline     time.Time                      `json:"deadline"`
	FinishedAt   *time.Time                     `json:"finished_at,omitempty"`
	Endpoints    map[string]*Endpoint           `json:"endpoints"`
	Logs         map[string]protocol.LogRequest `json:"logs"`
}
type state struct {
	TargetCredentials map[string]protocol.Credentials `json:"target_credentials,omitempty"`
	PendingAudits     []Audit                         `json:"pending_audits,omitempty"`
	Registered        map[string]ProbeConfig          `json:"registered,omitempty"`
	DisabledProbes    map[string]bool                 `json:"disabled_probes,omitempty"`
	Settings          Settings                        `json:"settings"`
	Health            map[string]TaskHealth           `json:"health,omitempty"`
	Credentials       map[string]protocol.Credentials `json:"credentials,omitempty"`
	Plan              *Plan                           `json:"plan,omitempty"`
	Runs              map[string]*Run                 `json:"runs"`
	Enabled           map[string]bool                 `json:"enabled"`
}
type session struct {
	StorageBytes                       int64
	ResourceError                      string
	Capabilities                       []string
	RetentionSent                      string
	ID                                 string
	Seen                               time.Time
	Capacity                           int
	Driver                             string
	ProbeVersion, SDKVersion, Platform string
	Out                                chan protocol.Message
	Stop                               chan struct{}
}
type Hub struct {
	lastStorageCheck   time.Time
	uploadMu           sync.Mutex
	archive            Archive
	lastMaintenance    time.Time
	storageInterrupted bool
	scheduleCursor     int
	tokens             map[string]*protocol.Credentials
	mu                 sync.Mutex
	config             Config
	state              state
	sessions           map[string]*session
	next               map[string]time.Time
	dir                string
	documents          Documents
	storageOK          bool
}

var scenarios = map[string]bool{"success": true, "join_failure": true, "publish_failure": true, "no_media": true, "sdk_error": true, "cleanup_error": true}

func New(dir string, c Config, docs Documents) (*Hub, error) {
	if c.MaxConcurrentRuns == 0 {
		c.MaxConcurrentRuns = 4
	}
	if c.MaxConcurrentRuns < 1 || c.MaxConcurrentRuns > 32 {
		return nil, errors.New("invalid RTC concurrency")
	}
	probes := map[string]bool{}
	secrets := map[string]bool{}
	for _, p := range c.Probes {
		if !protocol.ValidID(p.ID) || len(p.Secret) < 24 || p.Location == "" || probes[p.ID] || secrets[p.Secret] {
			return nil, errors.New("invalid or duplicated RTC probe identity")
		}
		probes[p.ID] = true
		secrets[p.Secret] = true
	}
	targets := map[string]bool{}
	for _, t := range c.Targets {
		if !protocol.ValidID(t.ID) || t.Name == "" || (t.Driver != "mock" && t.Driver != "agora") || targets[t.ID] {
			return nil, errors.New("RTC scaffold requires distinct mock targets")
		}
		targets[t.ID] = true
	}
	tasks := map[string]bool{}
	for i := range c.Tasks {
		t := &c.Tasks[i]
		if t.Scenario == "" {
			t.Scenario = "success"
		}
		if t.IntervalSeconds == 0 {
			t.IntervalSeconds = 60
		}
		if t.TimeoutSeconds == 0 {
			t.TimeoutSeconds = 30
		}
		if !protocol.ValidID(t.ID) || tasks[t.ID] || !targets[t.TargetID] || !probes[t.Sender] || !probes[t.Receiver] || t.IntervalSeconds < 5 || t.IntervalSeconds > 3600 || t.TimeoutSeconds < 3 || t.TimeoutSeconds >= t.IntervalSeconds {
			return nil, errors.New("invalid RTC task")
		}
		tasks[t.ID] = true
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0700); err != nil {
		return nil, err
	}
	validated, validationErr := normalizePlan(c.Probes, Plan{Targets: c.Targets, Tasks: c.Tasks})
	if validationErr != nil {
		return nil, validationErr
	}
	c.Targets, c.Tasks = validated.Targets, validated.Tasks
	h := &Hub{config: c, state: state{Runs: map[string]*Run{}, Enabled: map[string]bool{}}, sessions: map[string]*session{}, next: map[string]time.Time{}, dir: dir, documents: docs, storageOK: true}
	if archive, ok := docs.(Archive); ok {
		h.archive = archive
	} else {
		h.archive = &fileArchive{dir: filepath.Join(dir, "history")}
	}
	if err := h.archive.RTCInit(); err != nil {
		return nil, err
	}
	var b []byte
	var err error
	if docs != nil {
		var found bool
		b, found, err = docs.LoadDocument("rtc-probe/state-v1")
		if !found && err == nil {
			b = nil
		}
	} else {
		b, err = os.ReadFile(filepath.Join(dir, "state.json"))
		if os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if len(b) > 32<<20 || json.Unmarshal(b, &h.state) != nil || h.state.Runs == nil || h.state.Enabled == nil {
			return nil, errors.New("corrupt RTC state; preserve for repair")
		}
	}
	for id, run := range h.state.Runs {
		if run == nil || !protocol.ValidID(id) || run.ID != id || (run.Kind != edgeSweepKind && (run.Endpoints["sender"] == nil || run.Endpoints["receiver"] == nil)) || (run.Kind == edgeSweepKind && (len(run.Edges) == 0 || len(run.Edges) > protocol.MaxMediaEdges)) || run.Logs == nil {
			return nil, errors.New("corrupt RTC execution; preserve for repair")
		}
	}
	migrated, migrationErr := h.loadTargetConfiguration()
	if migrationErr != nil {
		return nil, migrationErr
	}

	if h.state.Health == nil {
		h.state.Health = map[string]TaskHealth{}
	} else {
		now := time.Now().UTC()
		for id, v := range h.state.Health {
			before := v
			v.State = "UNKNOWN"
			v.Reason = "CENTER_RESTARTED"
			v.FailureCount = 0
			v.SuccessCount = 0
			v.UnknownSince = &now
			h.state.Health[id] = v
			h.state.PendingAudits = append(h.state.PendingAudits, healthAudit(&before, &v))
		}
	}
	h.tokens = map[string]*protocol.Credentials{}
	if h.state.Registered == nil {
		h.state.Registered = map[string]ProbeConfig{}
	}
	if h.state.DisabledProbes == nil {
		h.state.DisabledProbes = map[string]bool{}
	}
	if h.state.Settings.HistoryDays == 0 {
		h.state.Settings = defaultSettings()
	}
	if h.state.Settings.ProbeDiskMB == 0 {
		h.state.Settings.ProbeSuccessMinutes = 60
		h.state.Settings.ProbeFailureMinutes = 1440
		h.state.Settings.ProbeDiskMB = 1024
	}
	for _, r := range h.state.Runs {
		if terminal(r.Status) {
			if err := h.archive.RTCPutRun(*r, int64(h.state.Settings.HistoryMB)<<20); err != nil {
				return nil, err
			}
		}
	}
	now := time.Now().UTC()
	dirty := migrated || len(h.state.PendingAudits) > 0
	for _, r := range h.state.Runs {
		if !terminal(r.Status) {
			r.Status = "ERROR"
			r.Reason = "CENTER_RESTARTED"
			r.FinishedAt = &now
			if r.Kind == edgeSweepKind {
				h.stopSweep(&h.state, r, now, "CENTER_RESTARTED", "ERROR")
			}
			dirty = true
		}
	}
	for _, t := range h.config.Tasks {
		h.next[t.ID] = now.Add(time.Duration(t.IntervalSeconds) * time.Second)
	}
	if dirty {
		if err = h.persist(&h.state); err != nil {
			return nil, err
		}
	}
	return h, nil
}
func terminal(s string) bool {
	return s == "SUCCESS" || s == "FAILED" || s == "ERROR" || s == "CANCELLED"
}
func (h *Hub) persist(s *state) error {
	for id, r := range s.Runs {
		if !terminal(r.Status) {
			continue
		}
		before, _ := json.Marshal(h.state.Runs[id])
		after, _ := json.Marshal(r)
		if string(before) != string(after) {
			if err := h.archive.RTCPutRun(*r, int64(s.Settings.HistoryMB)<<20); err != nil {
				h.storageOK = false
				h.storageInterrupted = true
				return err
			}
		}
	}
	// Full execution history lives in Archive. Keep a small control document,
	// plus runs still finishing or awaiting an automatic diagnostic upload.
	var completed []*Run
	for _, run := range s.Runs {
		if !terminal(run.Status) {
			continue
		}
		waiting := false
		for _, request := range run.Logs {
			if request.Status == "REQUESTED" && time.Now().Before(request.ExpiresAt) {
				waiting = true
			}
		}
		if !waiting && time.Now().After(run.Deadline.Add(10*time.Second)) {
			completed = append(completed, run)
		}
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].CreatedAt.After(completed[j].CreatedAt) })
	roots, children := 0, 0
	for _, run := range completed {
		if run.ParentRunID == "" {
			roots++
			if roots > 4 {
				delete(s.Runs, run.ID)
			}
		} else {
			children++
			if children > 2 {
				delete(s.Runs, run.ID)
			}
		}
	}
	s.PendingAudits = append(s.PendingAudits, h.healthAudits(*s)...)
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if len(b) > 32<<20 {
		return errors.New("RTC state capacity reached")
	}
	e = h.commitAuditedState(s, b)
	h.storageOK = e == nil
	if e != nil {
		h.storageInterrupted = true
	}
	return e
}

// change commits before publishing state or acknowledging evidence.
func (h *Hub) change(fn func(*state) error) error {
	b, e := json.Marshal(h.state)
	if e != nil {
		return e
	}
	var next state
	if e = json.Unmarshal(b, &next); e != nil {
		return e
	}
	if next.Health == nil {
		next.Health = map[string]TaskHealth{}
	}
	if next.Registered == nil {
		next.Registered = map[string]ProbeConfig{}
	}
	if next.DisabledProbes == nil {
		next.DisabledProbes = map[string]bool{}
	}
	if h.storageInterrupted {
		now := time.Now().UTC()
		for id, v := range next.Health {
			v.State = "UNKNOWN"
			v.Reason = "STORAGE_UNAVAILABLE"
			v.FailureCount = 0
			v.SuccessCount = 0
			v.UnknownSince = &now
			next.Health[id] = v
		}
	}
	if next.Credentials == nil {
		next.Credentials = map[string]protocol.Credentials{}
	}
	if e = fn(&next); e != nil {
		return e
	}
	if e = h.persist(&next); e != nil {
		return e
	}
	h.state = next
	h.storageInterrupted = false
	return nil
}
func (h *Hub) task(id string) (protocol.Task, bool) {
	for _, t := range h.config.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return protocol.Task{}, false
}
func (h *Hub) target(id string) protocol.Target {
	for _, t := range h.config.Targets {
		if t.ID == id {
			return t
		}
	}
	return protocol.Target{}
}
func revision(t protocol.Task, target protocol.Target) string {
	b, _ := json.Marshal([]any{t, target})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:24]
}
func (h *Hub) enabled(t protocol.Task) bool {
	if v, ok := h.state.Enabled[t.ID]; ok {
		return v
	}
	return t.Enabled
}
func (h *Hub) Authenticate(id, secret string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	actual := sha256.Sum256([]byte(secret))
	valid := false
	for _, p := range h.probesLocked() {
		expected := sha256.Sum256([]byte(p.Secret))
		if p.ID == id && subtle.ConstantTimeCompare(actual[:], expected[:]) == 1 {
			valid = true
		}
	}
	return valid
}
func (h *Hub) Session(id string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.sessions[id]; old != nil {
		close(old.Stop)
	}
	s := &session{ID: protocol.ID(), Seen: time.Now(), Out: make(chan protocol.Message, 64), Stop: make(chan struct{})}
	h.sessions[id] = s
	return s
}
func (h *Hub) Disconnect(id string, s *session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[id] == s {
		delete(h.sessions, id)
	}
}
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	defer func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		for id, s := range h.sessions {
			close(s.Stop)
			delete(h.sessions, id)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.tick(now.UTC())
		}
	}
}
func (h *Hub) tick(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Sub(h.lastStorageCheck) >= time.Second {
		h.lastStorageCheck = now
		if db, ok := h.documents.(interface{ Healthy() bool }); ok && !db.Healthy() {
			h.storageOK = false
			h.storageInterrupted = true
		}
	}
	h.refreshHealth(now)
	h.maintenance(now)
	h.advanceSweeps(now)
	for _, r := range h.state.Runs {
		for id, request := range r.Logs {
			if request.Status == "REQUESTED" && now.After(request.ExpiresAt) {
				runID, logID := r.ID, id
				_ = h.change(func(s *state) error {
					v := s.Runs[runID].Logs[logID]
					v.Status = "EXPIRED"
					s.Runs[runID].Logs[logID] = v
					return nil
				})
			}
		}
		if r.Kind != edgeSweepKind && !terminal(r.Status) && now.After(r.Deadline.Add(protocol.CleanupGrace+protocol.ResultReportGrace)) {
			id := r.ID
			_ = h.change(func(s *state) error {
				v := s.Runs[id]
				v.Status = "ERROR"
				v.Reason = "EXECUTION_DEADLINE_EXCEEDED"
				v.FinishedAt = &now
				h.applyHealth(s, v, now)
				h.addLogs(v, now)
				return nil
			})
		}
	}
	scheduled := false
	for i := 0; i < len(h.config.Tasks); i++ {
		t := h.config.Tasks[(i+h.scheduleCursor)%len(h.config.Tasks)]
		if h.enabled(t) && !now.Before(h.next[t.ID]) {
			scheduled = true
			h.next[t.ID] = now.Add(time.Duration(t.IntervalSeconds) * time.Second)
			if _, err := h.startLocked(t.ID, now); err != nil && err.Error() != "task already running" && h.target(t.TargetID).Driver == "agora" {
				_ = h.change(func(s *state) error {
					v := h.newHealth(t, now, "NOT_SCHEDULED")
					if old, ok := s.Health[t.ID]; ok {
						v.LastSuccessAt = old.LastSuccessAt
						v.LastRunID = old.LastRunID
						v.LastResultAt = old.LastResultAt
					}
					s.Health[t.ID] = v
					return nil
				})
			}
		}
	}
	if scheduled && len(h.config.Tasks) > 0 {
		h.scheduleCursor = (h.scheduleCursor + 1) % len(h.config.Tasks)
	}
}
func (h *Hub) Start(taskID string) (Run, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.startLocked(taskID, time.Now().UTC())
}
func (h *Hub) startLocked(taskID string, now time.Time) (Run, error) {
	if task, ok := h.task(taskID); ok && task.RTC != nil && task.RTC.MediaEdges != nil {
		return h.startSweepLocked(taskID, now)
	}
	return h.startProbeLocked(taskID, now, "", "")
}
func (h *Hub) startProbeLocked(taskID string, now time.Time, edgeEndpoint, parentID string) (Run, error) {
	if !h.storageOK {
		if err := h.change(func(*state) error { return nil }); err != nil {
			return Run{}, errors.New("RTC 存储不可用")
		}
	}
	t, ok := h.task(taskID)
	if !ok {
		return Run{}, errors.New("task not found")
	}
	if !h.enabled(t) {
		return Run{}, errors.New("task disabled")
	}
	if err := h.admitProbe(t, now, parentID); err != nil {
		return Run{}, err
	}
	target := h.target(t.TargetID)
	rev := revision(t, target)
	id := protocol.ID()
	r := Run{ParentRunID: parentID, EdgeEndpoint: edgeEndpoint, ID: id, TaskID: t.ID, TargetID: t.TargetID, Revision: rev, Driver: target.Driver, Status: "PREPARING", Reason: "MOCK_EXECUTION", CreatedAt: now, Deadline: now.Add(time.Duration(t.TimeoutSeconds) * time.Second), Endpoints: map[string]*Endpoint{}, Logs: map[string]protocol.LogRequest{}}
	if target.Driver == "agora" {
		r.Reason = "SDK_EXECUTION"
	}
	for role, probe := range map[string]string{"sender": t.Sender, "receiver": t.Receiver} {
		uid := "10001"
		if role == "receiver" {
			uid = "10002"
		}
		job := protocol.Job{ParentRunID: parentID, EdgeEndpoint: edgeEndpoint, RunID: id, TaskID: t.ID, Target: target, Revision: rev, ProbeID: probe, Role: role, Channel: "probe_" + id, UID: uid, PublisherUID: "10001", Scenario: t.Scenario, Deadline: r.Deadline, MediaDurationMS: 1200}
		if sess := h.sessions[probe]; sess != nil {
			job.ProbeVersion = sess.ProbeVersion
			job.SDKVersion = sess.SDKVersion
			job.Platform = sess.Platform
		}
		for _, config := range h.probesLocked() {
			if config.ID == probe {
				job.ProbeLocation = config.Location
				break
			}
		}
		if t.RTC != nil {
			job.RTC = t.RTC
			job.MediaDurationMS = t.RTC.MediaDurationMS
			job.Channel = t.RTC.ChannelPrefix + "_" + id
			if t.RTC.ChannelName != "" {
				job.Channel = t.RTC.ChannelName
			}
			job.PublisherUID = strconv.FormatUint(uint64(t.RTC.SenderUID), 10)
			job.UID = job.PublisherUID
			if role == "receiver" {
				job.UID = strconv.FormatUint(uint64(t.RTC.ReceiverUID), 10)
			}
		}
		r.Endpoints[role] = &Endpoint{Job: job, Events: []protocol.Event{}}
	}
	var evicted []string
	err := h.change(func(s *state) error {
		if len(s.Runs) >= 100 {
			var oldest *Run
			for _, v := range s.Runs {
				if terminal(v.Status) && (oldest == nil || v.CreatedAt.Before(oldest.CreatedAt)) {
					oldest = v
				}
			}
			if oldest == nil {
				return errors.New("history capacity reached")
			}
			delete(h.tokens, oldest.ID)
			delete(s.Runs, oldest.ID)
		}
		s.Runs[id] = &r
		if parentID != "" {
			parent := s.Runs[parentID]
			if parent == nil || terminal(parent.Status) {
				return errors.New("sweep no longer active")
			}
			for i := range parent.Edges {
				if parent.Edges[i].Endpoint == edgeEndpoint && parent.Edges[i].Status == "PENDING" {
					parent.Edges[i].RunID = id
					parent.Edges[i].Status = "PREPARING"
					parent.Edges[i].StartedAt = &now
					break
				}
			}
			parent.Status = "RUNNING"
			parent.Reason = "MEDIA_EDGE_SWEEP_RUNNING"
		}
		return nil
	})
	if err == nil {
		for _, id := range evicted {
			_ = os.Remove(filepath.Join(h.dir, "logs", id+".zip"))
		}
	}
	return r, err
}
func (h *Hub) Enable(id string, enabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.task(id)
	if !ok {
		return errors.New("task not found")
	}
	if h.enabled(t) == enabled {
		return nil
	}
	now := time.Now().UTC()
	return h.change(func(s *state) error {
		s.Enabled[id] = enabled
		if !enabled {
			v := s.Health[id]
			if v.TaskID == "" {
				v = h.newHealth(t, now, "TASK_DISABLED")
			}
			v.State = "UNKNOWN"
			v.Reason = "TASK_DISABLED"
			v.Enabled = false
			v.FailureCount = 0
			v.SuccessCount = 0
			s.Health[id] = v
		} else {
			v := h.newHealth(t, now, "WAITING_FIRST_RESULT")
			v.Enabled = true
			old := s.Health[id]
			v.LastSuccessAt = old.LastSuccessAt
			v.LastRunID = old.LastRunID
			v.LatestResult = old.LatestResult
			v.LastResultAt = old.LastResultAt
			s.Health[id] = v
		}
		h.next[id] = now.Add(time.Duration(t.IntervalSeconds) * time.Second)
		if !enabled {
			for _, r := range s.Runs {
				if r.TaskID == id && !terminal(r.Status) {
					r.Status = "CANCELLED"
					r.Reason = "TASK_DISABLED"
					r.FinishedAt = &now
					if r.Kind == edgeSweepKind {
						h.stopSweep(s, r, now, "TASK_DISABLED", "CANCELLED")
					}
				}
			}
		}
		return nil
	})
}
func (h *Hub) Cancel(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Runs[id] == nil {
		return errors.New("run not found")
	}
	now := time.Now().UTC()
	return h.change(func(s *state) error {
		r := s.Runs[id]
		if !terminal(r.Status) {
			r.Status = "CANCELLED"
			r.Reason = "MANUAL_CANCEL"
			r.FinishedAt = &now
			if r.Kind == edgeSweepKind {
				h.stopSweep(s, r, now, "MANUAL_CANCEL", "CANCELLED")
			}
			h.applyHealth(s, r, now)
		}
		return nil
	})
}
func (h *Hub) addLogs(r *Run, now time.Time) {
	for role, e := range r.Endpoints {
		exists := false
		for _, v := range r.Logs {
			if v.Role == role {
				if v.Partial && e.Result != nil {
					continue
				}
				if v.Status == "REQUESTED" && now.Before(v.ExpiresAt) {
					exists = true
				}
				if v.Status == "AVAILABLE" {
					if _, err := os.Stat(filepath.Join(h.dir, "logs", v.ID+".zip")); err == nil {
						exists = true
					}
				}
			}
		}
		if !exists {
			id := protocol.ID()
			r.Logs[id] = protocol.LogRequest{ID: id, RunID: r.ID, ProbeID: e.Job.ProbeID, Role: role, Status: "REQUESTED", ExpiresAt: now.Add(time.Hour)}
		}
	}
}
func (h *Hub) Collect(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var archived Run
	if h.state.Runs[id] == nil {
		var err error
		archived, err = h.archive.RTCGetRun(id)
		if err != nil {
			return errors.New("run not found")
		}
	}
	return h.change(func(s *state) error {
		if s.Runs[id] == nil {
			s.Runs[id] = &archived
		}
		if s.Runs[id].Kind == edgeSweepKind {
			for _, edge := range s.Runs[id].Edges {
				if edge.RunID == "" {
					continue
				}
				child := s.Runs[edge.RunID]
				if child == nil {
					v, err := h.archive.RTCGetRun(edge.RunID)
					if err != nil {
						continue
					}
					child = &v
					s.Runs[edge.RunID] = child
				}
				h.addLogs(child, time.Now().UTC())
			}
			return nil
		}
		h.addLogs(s.Runs[id], time.Now().UTC())
		return nil
	})
}

func (h *Hub) Commands(id string, s *session) []protocol.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[id] != s {
		return nil
	}
	out := []protocol.Message{}
	for _, capability := range s.Capabilities {
		if capability == "retention_v1" && s.RetentionSent != h.state.Settings.Version {
			m := protocol.Msg("configure_probe")
			m.Retention = &protocol.RetentionPolicy{Version: h.state.Settings.Version, SuccessMinutes: h.state.Settings.ProbeSuccessMinutes, FailureMinutes: h.state.Settings.ProbeFailureMinutes, DiskMB: h.state.Settings.ProbeDiskMB}
			out = append(out, m)
			s.RetentionSent = h.state.Settings.Version
			break
		}
	}
	for _, r := range h.state.Runs {
		if r.Kind == edgeSweepKind {
			continue
		}
		if r.Driver == "agora" && r.Endpoints["sender"].Job.ProbeID == id && r.Endpoints["receiver"].Job.ProbeID == id {
			if m, ok := h.pairCommand(r); ok {
				out = append(out, m)
			}
			for _, request := range r.Logs {
				if request.ProbeID == id && request.Status == "REQUESTED" && time.Now().Before(request.ExpiresAt) {
					m := protocol.Msg("collect_logs")
					copy := request
					m.Logs = &copy
					out = append(out, m)
				}
			}
			continue
		}
		for role, e := range r.Endpoints {
			if e.Job.ProbeID != id {
				continue
			}
			var kind string
			if !terminal(r.Status) {
				if !e.Ready && e.Result == nil {
					kind = "prepare_run"
				} else if r.Status == "RUNNING" && e.Result == nil {
					kind = "start_media"
				}
			} else if e.Result == nil {
				kind = "cancel_run"
			}
			if kind != "" {
				m := protocol.Msg(kind)
				m.RunID = r.ID
				m.Revision = r.Revision
				m.Role = role
				if kind == "prepare_run" {
					job := e.Job
					m.Job = &job
				}
				out = append(out, m)
			}
		}
		for _, request := range r.Logs {
			if request.ProbeID == id && request.Status == "REQUESTED" && time.Now().Before(request.ExpiresAt) {
				m := protocol.Msg("collect_logs")
				copy := request
				m.Logs = &copy
				out = append(out, m)
			}
		}
	}
	return out
}
func (h *Hub) Receive(id string, sess *session, m protocol.Message) (protocol.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[id] != sess || m.Version != protocol.Version {
		return protocol.Message{}, errors.New("obsolete or invalid probe session")
	}
	sess.Seen = time.Now()
	if m.Type == "log_status" {
		return h.receiveLogStatus(id, m)
	}
	if m.Type == "hello" || m.Type == "heartbeat" {
		if (m.Driver != "mock" && m.Driver != "agora") || m.Capacity < 1 || m.Capacity > 8 {
			return protocol.Message{}, errors.New("unsupported capability")
		}
		sess.Driver = m.Driver
		sess.ProbeVersion, sess.SDKVersion, sess.Platform = m.ProbeVersion, m.SDKVersion, m.Platform
		sess.Capacity = m.Capacity
		sess.ResourceError = m.ResourceError
		sess.StorageBytes = m.StorageBytes
		sess.Capabilities = append([]string(nil), m.Capabilities...)
		return protocol.Message{}, nil
	}
	r := h.state.Runs[m.RunID]
	if r == nil {
		if saved, err := h.archive.RTCGetRun(m.RunID); err == nil {
			r = &saved
		}
	}
	if r == nil || m.Revision != r.Revision {
		return protocol.Message{}, errors.New("unknown execution or config revision")
	}
	e := r.Endpoints[m.Role]
	if e == nil || e.Job.ProbeID != id {
		return protocol.Message{}, errors.New("execution owner mismatch")
	}
	ack := protocol.Msg("")
	ack.RunID = r.ID
	ack.Role = m.Role
	ack.Revision = r.Revision
	if m.Type == "run_ready" && e.Ready {
		return protocol.Message{}, nil
	}
	err := h.change(func(s *state) error {
		if s.Runs[m.RunID] == nil {
			s.Runs[m.RunID] = r
		}
		run := s.Runs[m.RunID]
		ep := run.Endpoints[m.Role]
		switch m.Type {
		case "run_ready":
			ep.Ready = true
			if !terminal(run.Status) && run.Endpoints["sender"].Ready && run.Endpoints["receiver"].Ready {
				run.Status = "RUNNING"
				if p := s.Runs[run.ParentRunID]; p != nil && !terminal(p.Status) {
					for i := range p.Edges {
						if p.Edges[i].RunID == run.ID {
							p.Edges[i].Status = "RUNNING"
						}
					}
				}
			}
		case "telemetry_batch":
			if len(m.Events) > 128 {
				return errors.New("event batch too large")
			}
			seen := map[string]protocol.Event{}
			for _, ev := range ep.Events {
				seen[ev.ID] = ev
			}
			for _, ev := range m.Events {
				if ev.RunID != "" && (ev.RunID != m.RunID || ev.Role != m.Role || ev.ProbeID != id || ev.Revision != m.Revision || ev.ParentRunID != run.ParentRunID || ev.EdgeEndpoint != run.EdgeEndpoint) {
					return errors.New("SDK event context mismatch")
				}
				if !protocol.ValidID(ev.ID) || !protocol.ValidID(ev.WorkerID) || ev.Seq == 0 || ev.At.IsZero() {
					return errors.New("invalid event")
				}
				if previous, exists := seen[ev.ID]; exists {
					a, _ := json.Marshal(previous)
					b, _ := json.Marshal(ev)
					if string(a) != string(b) {
						return errors.New("conflicting event replay")
					}
				} else {
					if len(ep.Events) >= 512 {
						return errors.New("execution event capacity reached")
					}
					ep.Events = append(ep.Events, ev)
					seen[ev.ID] = ev
				}
				ack.EventIDs = append(ack.EventIDs, ev.ID)
			}
			ack.Type = "telemetry_ack"
		case "run_result":
			v := m.Result
			if v == nil || !protocol.ValidID(v.ID) || v.Driver != run.Driver || !protocol.ValidID(v.WorkerID) || v.FinishedAt.IsZero() || !(v.Outcome == "SUCCESS" || v.Outcome == "FAILED" || v.Outcome == "ERROR" || v.Outcome == "CANCELLED") {
				return errors.New("invalid endpoint result")
			}
			if ep.Result != nil {
				old, _ := json.Marshal(ep.Result)
				fresh, _ := json.Marshal(v)
				if string(old) != string(fresh) {
					return errors.New("conflicting endpoint result")
				}
			} else {
				ep.Result = v
			}
			ack.Type = "result_ack"
			ack.ID = v.ID
			if !terminal(run.Status) {
				a, b := run.Endpoints["sender"].Result, run.Endpoints["receiver"].Result
				if a != nil && b != nil {
					now := time.Now().UTC()
					run.FinishedAt = &now
					run.Status = "SUCCESS"
					run.Reason = "SIMULATION_ONLY"
					if run.Driver == "agora" {
						run.Reason = "MEDIA_VERIFIED"
					}
					for _, x := range []*protocol.Result{a, b} {
						if x.Outcome == "ERROR" {
							run.Status = "ERROR"
							run.Reason = x.Reason
							break
						}
						if x.Outcome != "SUCCESS" {
							run.Status = x.Outcome
							run.Reason = x.Reason
						}
					}
					if run.Status == "SUCCESS" && !verifiedEvidence(run, a, b) {
						run.Status = "FAILED"
						run.Reason = "MEDIA_EVIDENCE_MISSING"
					}
					h.applyHealth(s, run, now)
					if run.Driver == "agora" || run.Status != "SUCCESS" || a.Cleanup != "SUCCESS" || b.Cleanup != "SUCCESS" {
						h.addLogs(run, now)
					}
				}
			}
			if terminal(run.Status) {
				h.addLogs(run, time.Now().UTC())
			}
		default:
			return errors.New("unknown probe message")
		}
		return nil
	})
	return ack, err
}
func (h *Hub) Snapshot() any {
	h.mu.Lock()
	defer h.mu.Unlock()
	probes := []any{}
	now := time.Now()
	for _, p := range h.probesLocked() {
		s := h.sessions[p.ID]
		online := s != nil && s.Driver != "" && now.Sub(s.Seen) < 60*time.Second
		view := map[string]any{"probe_id": p.ID, "name": p.Name, "enabled": !h.state.DisabledProbes[p.ID], "location": p.Location, "host_address": p.HostAddress, "online": online, "driver": "mock"}
		if s != nil {
			view["driver"] = s.Driver
			view["last_heartbeat"] = s.Seen
			view["probe_version"] = s.ProbeVersion
			view["sdk_version"] = s.SDKVersion
			view["platform"] = s.Platform
			view["capacity"] = s.Capacity
			view["capabilities"] = s.Capabilities
			view["resource_error"] = s.ResourceError
			view["storage_bytes"] = s.StorageBytes
		}
		probes = append(probes, view)
	}
	tasks := []any{}
	for _, t := range h.config.Tasks {
		t.Enabled = h.enabled(t)
		t = h.publicTask(t)
		tasks = append(tasks, t)
	}
	runs := []*Run{}
	for _, r := range h.state.Runs {
		copy := *r
		h.decorateRun(&copy)
		runs = append(runs, &copy)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	// Marshal under lock to return an immutable snapshot to HTTP callers.
	mode, health, reason := h.healthLocked()
	healths := map[string]TaskHealth{}
	for _, t := range h.config.Tasks {
		if h.target(t.TargetID).Driver == "agora" {
			healths[t.ID] = h.effectiveHealth(t, now)
		}
	}
	v := map[string]any{"mode": mode, "config_revision": planRevision(h.planLocked()), "rtc_service_status": health, "reason": reason, "storage_available": h.storageOK, "probes": probes, "targets": h.planLocked().Targets, "tasks": tasks, "runs": runs, "task_health": healths, "settings": h.state.Settings}
	b, _ := json.Marshal(v)
	return json.RawMessage(b)
}
func (h *Hub) Detail(id string) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.state.Runs[id]
	if r == nil {
		archived, err := h.archive.RTCGetRun(id)
		if err != nil {
			return nil, fmt.Errorf("run not found")
		}
		h.decorateRun(&archived)
		return json.Marshal(archived)
	}
	copy := *r
	h.decorateRun(&copy)
	b, e := json.Marshal(copy)
	return b, e
}
