// Package hub coordinates independent RTC probes. Mock evidence never establishes RTC health.
package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/durable"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

type RuntimeRegistration struct {
	ID          string `json:"runtime_id"`
	Name        string `json:"name"`
	SecretHash  string `json:"secret_hash"`
	HostID      string `json:"host_id"`
	HostName    string `json:"host_name"`
	HostAddress string `json:"host_address"`
	Location    string `json:"location"`
	ServiceType string `json:"service_type"`
	ClientType  string `json:"client_type"`
	Driver      string `json:"driver"`
}
type Config struct {
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
	ClientType   string                         `json:"client_type,omitempty"`
	NativePlan   *protocol.NativePlan           `json:"native_plan,omitempty"`
	NativeCase   *protocol.NativeCase           `json:"native_case,omitempty"`
	Cases        []NativeCaseResult             `json:"cases,omitempty"`
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
	SchemaVersion     int                                `json:"schema_version"`
	ProbeHosts        map[string]ProbeHostRegistration   `json:"probe_hosts"`
	Native            map[string]*NativeState            `json:"native,omitempty"`
	TargetCredentials map[string]protocol.AppCredentials `json:"target_credentials,omitempty"`
	Runtimes          map[string]RuntimeRegistration     `json:"runtimes"`
	DisabledClients   map[string]bool                    `json:"disabled_clients,omitempty"`
	Settings          Settings                           `json:"settings"`
	Health            map[string]TaskHealth              `json:"health,omitempty"`
	Plan              *Plan                              `json:"plan,omitempty"`
	Runs              map[string]*Run                    `json:"runs"`
	Enabled           map[string]bool                    `json:"enabled"`
}
type session struct {
	StorageBytes                         int64
	ResourceError                        string
	Capabilities                         []string
	RetentionSent                        string
	ID                                   string
	Seen                                 time.Time
	Capacity                             int
	Driver                               string
	RuntimeVersion, SDKVersion, Platform string
	Out                                  chan protocol.Message
	Stop                                 chan struct{}
}
type Hub struct {
	nativeInventory    func() (protocol.NativeInventory, error)
	nativeSnapshot     protocol.NativeInventory
	nativeInventoryAt  time.Time
	nativeInventoryErr error
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
		if !protocol.ValidID(t.ID) || tasks[t.ID] || !targets[t.TargetID] || t.IntervalSeconds < 5 || t.IntervalSeconds > 3600 || t.TimeoutSeconds < 3 || t.TimeoutSeconds >= t.IntervalSeconds {
			return nil, errors.New("invalid RTC task")
		}
		tasks[t.ID] = true
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0700); err != nil {
		return nil, err
	}
	h := &Hub{config: c, state: state{SchemaVersion: 2, Runs: map[string]*Run{}, Enabled: map[string]bool{}, ProbeHosts: map[string]ProbeHostRegistration{}, Runtimes: map[string]RuntimeRegistration{}}, sessions: map[string]*session{}, next: map[string]time.Time{}, dir: dir, documents: docs, storageOK: true}
	if archive, ok := docs.(Archive); ok {
		h.archive = archive
	} else {
		h.archive = &fileArchive{dir: filepath.Join(dir, "history-v2")}
	}
	if err := h.archive.RTCInit(); err != nil {
		return nil, err
	}
	var b []byte
	var err error
	if docs != nil {
		var found bool
		b, found, err = docs.LoadDocument("probe-center/state-v2")
		if !found && err == nil {
			b = nil
		}
	} else {
		b, err = os.ReadFile(filepath.Join(dir, "state-v2.json"))
		if os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if len(b) > 32<<20 || decodeStateV2(b, &h.state) != nil || h.state.SchemaVersion != 2 || h.state.Runs == nil || h.state.Enabled == nil || h.state.ProbeHosts == nil || h.state.Runtimes == nil {
			return nil, errors.New("invalid v2 probe state; old identities and bindings are unsupported")
		}
	}
	if err := validateRegistry(h.state); err != nil {
		return nil, err
	}
	for id, run := range h.state.Runs {
		if run == nil || !protocol.ValidID(id) || run.ID != id || (!isSummary(run) && (run.Endpoints["sender"] == nil || run.Endpoints["receiver"] == nil)) || (run.Kind == edgeSweepKind && (len(run.Edges) == 0 || len(run.Edges) > protocol.MaxMediaEdges)) || (run.Kind == nativeCoverageKind && (run.NativePlan == nil || len(run.Cases) == 0)) || run.Logs == nil {
			return nil, errors.New("corrupt RTC execution; preserve for repair")
		}
	}
	configurationDirty, configurationErr := h.loadTargetConfiguration()
	if configurationErr != nil {
		return nil, configurationErr
	}

	if h.state.Health == nil {
		h.state.Health = map[string]TaskHealth{}
	} else {
		now := time.Now().UTC()
		for id, v := range h.state.Health {
			v.State = "UNKNOWN"
			v.Reason = "CENTER_RESTARTED"
			v.FailureCount = 0
			v.SuccessCount = 0
			v.UnknownSince = &now
			h.state.Health[id] = v
		}
	}
	if h.state.Native == nil {
		h.state.Native = map[string]*NativeState{}
	}
	for _, state := range h.state.Native {
		for _, phase := range state.Phases {
			phase.Health.State = "UNKNOWN"
			phase.Health.Reason = "CENTER_RESTARTED"
			phase.Health.FailureCount = 0
			phase.Health.SuccessCount = 0
			phase.NextAt = time.Now().UTC()
		}
	}
	h.tokens = map[string]*protocol.Credentials{}
	if h.state.Runtimes == nil {
		h.state.Runtimes = map[string]RuntimeRegistration{}
	}
	if h.state.DisabledClients == nil {
		h.state.DisabledClients = map[string]bool{}
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
	dirty := configurationDirty || len(h.state.Health) > 0
	for _, r := range h.state.Runs {
		if !terminal(r.Status) {
			r.Status = "ERROR"
			r.Reason = "CENTER_RESTARTED"
			r.FinishedAt = &now
			if r.Kind == nativeCoverageKind {
				h.stopNative(&h.state, r, now, "CENTER_RESTARTED", "ERROR")
			}
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
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if len(b) > 32<<20 {
		return errors.New("RTC state capacity reached")
	}
	if h.documents != nil {
		e = h.documents.SaveDocument("probe-center/state-v2", b)
	} else {
		e = durable.Bytes(filepath.Join(h.dir, "state-v2.json"), b)
	}
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
	if next.ProbeHosts == nil {
		next.ProbeHosts = map[string]ProbeHostRegistration{}
	}
	if next.Runtimes == nil {
		next.Runtimes = map[string]RuntimeRegistration{}
	}
	if next.DisabledClients == nil {
		next.DisabledClients = map[string]bool{}
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
	valid := false
	for _, p := range h.runtimesLocked() {
		if p.ID == id && probeSecretMatches(p, secret) {
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
	h.tickNative(now)
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
		if !isSummary(r) && !terminal(r.Status) && now.After(r.Deadline.Add(protocol.CleanupGrace+protocol.ResultReportGrace)) {
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
		if coverageEnabled(t) {
			continue
		}
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
	if task, ok := h.task(taskID); ok && coverageEnabled(task) {
		return h.startNativeCoverage(task, now)
	}
	if task, ok := h.task(taskID); ok && task.RTC != nil && task.RTC.MediaEdges != nil {
		return h.startSweepLocked(taskID, now)
	}
	return h.startProbeLocked(taskID, now, "", "")
}
func (h *Hub) startProbeLocked(taskID string, now time.Time, edgeEndpoint, parentID string) (Run, error) {
	return h.startProbeSpec(taskID, now, edgeEndpoint, parentID, nil)
}
func (h *Hub) startProbeSpec(taskID string, now time.Time, edgeEndpoint, parentID string, nativeCase *protocol.NativeCase) (Run, error) {
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
	if protocol.IsInteropClient(t.ClientType) {
		if err := h.admitInterop(t, now, parentID); err != nil {
			return Run{}, err
		}
	}
	if err := h.admitProbe(t, now, parentID); err != nil {
		return Run{}, err
	}
	if nativeCase != nil {
		supported := false
		if session := h.sessions[h.runtimeForTask(t)]; session != nil {
			for _, cap := range session.Capabilities {
				if cap == protocol.ProbeEdgeAddressCapability {
					supported = true
				}
			}
		}
		if !supported {
			return Run{}, errors.New("PROBE_EDGE_FORMAT_UNAVAILABLE")
		}
	}
	target := h.target(t.TargetID)
	rev := revision(t, target)
	target.Driver = protocol.TaskDriver(t, target)
	id := protocol.ID()
	r := Run{ClientType: t.ClientType, NativeCase: nativeCase, ParentRunID: parentID, EdgeEndpoint: edgeEndpoint, ID: id, TaskID: t.ID, TargetID: t.TargetID, Revision: rev, Driver: target.Driver, Status: "PREPARING", Reason: "MOCK_EXECUTION", CreatedAt: now, Deadline: now.Add(time.Duration(t.TimeoutSeconds) * time.Second), Endpoints: map[string]*Endpoint{}, Logs: map[string]protocol.LogRequest{}}
	if protocol.RealDriver(target.Driver) {
		r.Reason = "SDK_EXECUTION"
	}
	for _, role := range []string{"sender", "receiver"} {
		endpointTask := t
		endpointTask.ClientType = protocol.EndpointClient(t, nativeCase, role)
		probe := h.runtimeForTask(endpointTask)
		endpointTarget := target
		endpointTarget.Driver = protocol.TaskDriver(endpointTask, h.target(t.TargetID))

		uid := "10001"
		if role == "receiver" {
			uid = "10002"
		}
		job := protocol.Job{HostID: t.HostID, ServiceType: t.ServiceType, ClientType: endpointTask.ClientType, NativeCase: nativeCase, ParentRunID: parentID, EdgeEndpoint: edgeEndpoint, RunID: id, TaskID: t.ID, Target: endpointTarget, Revision: rev, RuntimeID: probe, Role: role, Channel: "probe_" + id, UID: uid, PublisherUID: "10001", Scenario: t.Scenario, Deadline: r.Deadline, MediaDurationMS: 1200}
		if nativeCase != nil && nativeCase.Duplex {
			job.PublisherUID = "10001"
			if role == "sender" {
				job.PublisherUID = "10002"
			}
		}
		if sess := h.sessions[probe]; sess != nil {
			job.RuntimeVersion = sess.RuntimeVersion
			job.SDKVersion = sess.SDKVersion
			job.Platform = sess.Platform
			for _, capability := range sess.Capabilities {
				if capability == protocol.SharedServiceLogsCapability {
					job.LogScope = protocol.ServiceLogScope
				}
			}
		}
		for _, config := range h.runtimesLocked() {
			if config.ID == probe {
				job.RuntimeLocation = config.Location
				break
			}
		}
		if t.RTC != nil {
			job.RTC = taskRTC(endpointTask, h.target(t.TargetID).RTC)
			if protocol.IsInteropClient(t.ClientType) {
				job.RTC.Codec = 1
			}
			job.MediaDurationMS = job.RTC.MediaDurationMS
		}
		if protocol.IsInteropClient(t.ClientType) {
			job.LogScope = ""
		}

		if nativeCase != nil {
			job.NativeCase = nativeCase.ForProbe()
			job.EdgeFormat = protocol.ProbeEdgeAddressFormat
			job.RTC = cloneRTC(job.RTC)
			if job.RTC != nil {
				job.RTC.NativeCoverage = nil
				if job.RTC.WebAccess != nil {
					job.RTC.WebAccess.Coverage = nil
				}
			}
			job.Target.RTC = cloneRTC(job.RTC)
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
			if parent.Kind == edgeSweepKind {
				parent.Reason = "MEDIA_EDGE_SWEEP_RUNNING"
			}
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
	if enabled && coverageEnabled(t) {
		tasks := append([]protocol.Task(nil), h.config.Tasks...)
		for i := range tasks {
			tasks[i].Enabled = h.enabled(tasks[i])
			if tasks[i].ID == id {
				tasks[i].Enabled = true
			}
		}
		if err := h.validateNativeBudget(tasks, h.config.Targets); err != nil {
			return err
		}
	}
	if h.enabled(t) == enabled {
		return nil
	}
	now := time.Now().UTC()
	return h.change(func(s *state) error {
		s.Enabled[id] = enabled
		if state := s.Native[id]; state != nil {
			for _, phase := range state.Phases {
				phase.Health.State = "UNKNOWN"
				phase.Health.Reason = "TASK_DISABLED"
				phase.Health.FailureCount = 0
				phase.Health.SuccessCount = 0
				phase.NextAt = now
			}
		}
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
					if r.Kind == nativeCoverageKind {
						h.stopNative(s, r, now, "TASK_DISABLED", "CANCELLED")
					}
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
			if r.Kind == nativeCoverageKind {
				h.stopNative(s, r, now, "MANUAL_CANCEL", "CANCELLED")
			}
			if r.Kind == edgeSweepKind {
				h.stopSweep(s, r, now, "MANUAL_CANCEL", "CANCELLED")
			}
			h.applyHealth(s, r, now)
		}
		return nil
	})
}
func logJob(r *Run, role string) (protocol.Job, bool) {
	if r == nil {
		return protocol.Job{}, false
	}
	if role == "pair" {
		a, b := r.Endpoints["sender"], r.Endpoints["receiver"]
		if a == nil || b == nil || a.Job.RuntimeID != b.Job.RuntimeID || a.Job.LogScope != protocol.ServiceLogScope || b.Job.LogScope != protocol.ServiceLogScope {
			return protocol.Job{}, false
		}
		job := a.Job
		job.Role = "pair"
		return job, true
	}
	if endpoint := r.Endpoints[role]; endpoint != nil {
		return endpoint.Job, true
	}
	return protocol.Job{}, false
}
func (h *Hub) addLogs(r *Run, now time.Time) {
	if r.Logs == nil {
		r.Logs = map[string]protocol.LogRequest{}
	}
	roles := []string{"sender", "receiver"}
	if _, ok := logJob(r, "pair"); ok {
		roles = []string{"pair"}
	}
	for _, role := range roles {
		job, ok := logJob(r, role)
		if !ok {
			continue
		}
		complete := true
		if role == "pair" {
			complete = r.Endpoints["sender"].Result != nil && r.Endpoints["receiver"].Result != nil
		} else {
			complete = r.Endpoints[role].Result != nil
		}
		exists := false
		for _, v := range r.Logs {
			if v.Role != role {
				continue
			}
			if v.Partial && complete {
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
		if !exists {
			id := protocol.ID()
			scope := ""
			if role == "pair" {
				scope = protocol.ServiceLogScope
			}
			r.Logs[id] = protocol.LogRequest{Scope: scope, ID: id, RunID: r.ID, RuntimeID: job.RuntimeID, Role: role, Status: "REQUESTED", ExpiresAt: now.Add(time.Hour)}
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
		if s.Runs[id].Kind == nativeCoverageKind {
			for _, row := range s.Runs[id].Cases {
				if row.RunID == "" {
					continue
				}
				child := s.Runs[row.RunID]
				if child == nil {
					saved, err := h.archive.RTCGetRun(row.RunID)
					if err != nil {
						continue
					}
					child = &saved
					s.Runs[row.RunID] = child
				}
				h.addLogs(child, time.Now().UTC())
			}
			return nil
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
		if isSummary(r) {
			continue
		}
		if r.Endpoints["sender"].Job.RuntimeID == id && r.Endpoints["receiver"].Job.RuntimeID == id {
			if m, ok := h.pairCommand(r); ok {
				out = append(out, m)
			}
			for _, request := range r.Logs {
				if request.RuntimeID == id && request.Status == "REQUESTED" && time.Now().Before(request.ExpiresAt) {
					m := protocol.Msg("collect_logs")
					copy := request
					m.Logs = &copy
					out = append(out, m)
				}
			}
			continue
		}
		for role, e := range r.Endpoints {
			if e.Job.RuntimeID != id {
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
					credentials, err := h.runCredentials(r)
					if err != nil {
						continue
					}
					job.Credentials = &protocol.Credentials{}
					if role == "sender" {
						job.Credentials.SenderToken = credentials.SenderToken
					} else {
						job.Credentials.ReceiverToken = credentials.ReceiverToken
					}
					m.Job = &job
				}
				out = append(out, m)
			}
		}
		for _, request := range r.Logs {
			if request.RuntimeID == id && request.Status == "REQUESTED" && time.Now().Before(request.ExpiresAt) {
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
	if sess == nil || h.sessions[id] != sess || m.Version != protocol.Version {
		return protocol.Message{}, errors.New("obsolete or invalid probe session")
	}
	sess.Seen = time.Now()
	if m.Type == "log_status" {
		return h.receiveLogStatus(id, m)
	}
	if m.Type == "hello" || m.Type == "heartbeat" {
		if (m.Driver != "mock" && !protocol.RealDriver(m.Driver)) || m.Capacity < 1 || m.Capacity > protocol.MaxWorkerCapacity {
			return protocol.Message{}, errors.New("unsupported capability")
		}
		registration, ok := h.state.Runtimes[id]
		if !ok || m.HostID != registration.HostID || m.ServiceType != registration.ServiceType || m.ClientType != registration.ClientType || (registration.Driver != "" && m.Driver != registration.Driver) {
			return protocol.Message{}, errors.New("runtime advertisement does not match its server registration")
		}
		sess.Driver = m.Driver
		sess.RuntimeVersion, sess.SDKVersion, sess.Platform = m.RuntimeVersion, m.SDKVersion, m.Platform
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
	if e == nil || e.Job.RuntimeID != id {
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
					for i := range p.Cases {
						if p.Cases[i].RunID == run.ID {
							p.Cases[i].Status = "RUNNING"
						}
					}
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
				if ev.RunID != "" && (ev.RunID != m.RunID || ev.Role != m.Role || ev.RuntimeID != id || ev.Revision != m.Revision || ev.ParentRunID != run.ParentRunID || ev.EdgeEndpoint != run.EdgeEndpoint) {
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
			if v == nil || !protocol.ValidID(v.ID) || v.Driver != ep.Job.Target.Driver || !protocol.ValidID(v.WorkerID) || v.FinishedAt.IsZero() || !(v.Outcome == "SUCCESS" || v.Outcome == "FAILED" || v.Outcome == "ERROR" || v.Outcome == "CANCELLED") {
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
					if protocol.RealDriver(run.Driver) {
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
					if run.NativeCase != nil {
						for _, result := range []*protocol.Result{a, b} {
							if result.Outcome == "FAILED" {
								run.Status = "FAILED"
								run.Reason = result.Reason
								break
							}
						}
					}
					if run.Status == "SUCCESS" && !verifiedEvidence(run, a, b) {
						run.Status = "FAILED"
						run.Reason = "MEDIA_EVIDENCE_MISSING"
					}
					h.applyHealth(s, run, now)
					if protocol.RealDriver(run.Driver) || run.Status != "SUCCESS" || a.Cleanup != "SUCCESS" || b.Cleanup != "SUCCESS" {
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
	return h.snapshot("full")
}

// History needs task/target names, not all cached executions and coverage
// evidence. Detail remains available from the individual run endpoint.
func (h *Hub) historySnapshot() any {
	return h.snapshot("history")
}

func (h *Hub) taskSnapshot() any {
	return h.snapshot("tasks")
}

type taskActivity struct {
	Running     bool   `json:"running"`
	ActiveRunID string `json:"active_run_id,omitempty"`
	LatestRunID string `json:"latest_run_id,omitempty"`
}

func (h *Hub) snapshot(view string) any {
	h.mu.Lock()
	defer h.mu.Unlock()
	probes := []any{}
	now := time.Now()
	for _, p := range h.runtimesLocked() {
		s := h.sessions[p.ID]
		online := s != nil && s.Driver != "" && now.Sub(s.Seen) < 60*time.Second
		view := map[string]any{"runtime_id": p.ID, "host_id": p.HostID, "host_name": p.HostName, "service_type": p.ServiceType, "client_type": p.ClientType, "name": p.Name, "enabled": !h.runtimeDisabled(p.ID), "location": p.Location, "host_address": p.HostAddress, "online": online, "driver": p.Driver}
		if s != nil {
			view["driver"] = s.Driver
			view["last_heartbeat"] = s.Seen
			view["runtime_version"] = s.RuntimeVersion
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
	if view == "full" {
		for _, r := range h.state.Runs {
			copy := *r
			h.decorateRun(&copy)
			runs = append(runs, &copy)
		}
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
	v := map[string]any{"mode": mode, "config_revision": planRevision(h.planLocked()), "rtc_service_status": health, "reason": reason, "storage_available": h.storageOK, "runtimes": probes, "hosts": h.hostViewsLocked(probes), "targets": h.planLocked().Targets, "tasks": tasks, "runs": runs, "task_health": healths, "settings": h.state.Settings}
	if view != "history" {
		v["native_coverage"] = h.state.Native
	}
	if view == "tasks" {
		activity := map[string]taskActivity{}
		configured := map[string]bool{}
		for _, task := range h.config.Tasks {
			configured[task.ID] = true
		}
		latest := map[string]*Run{}
		active := map[string]*Run{}
		for _, run := range h.state.Runs {
			if !configured[run.TaskID] {
				continue
			}
			value := activity[run.TaskID]
			if !terminal(run.Status) {
				value.Running = true
			}
			if run.ParentRunID == "" {
				previous := latest[run.TaskID]
				if previous == nil || run.CreatedAt.After(previous.CreatedAt) {
					latest[run.TaskID] = run
					value.LatestRunID = run.ID
				}
				if !terminal(run.Status) {
					previous = active[run.TaskID]
					if previous == nil || run.CreatedAt.After(previous.CreatedAt) {
						active[run.TaskID] = run
						value.ActiveRunID = run.ID
					}
				}
			}
			activity[run.TaskID] = value
		}
		for id, health := range healths {
			value := activity[id]
			if value.LatestRunID == "" {
				value.LatestRunID = health.LastRunID
			}
			activity[id] = value
		}
		v["task_activity"] = activity
	}
	b, _ := json.Marshal(v)
	return json.RawMessage(b)
}
func (h *Hub) Detail(id string) (json.RawMessage, error) {
	return h.detailResponse(id, false)
}

func (h *Hub) detailView(id string) (json.RawMessage, error) {
	return h.detailResponse(id, true)
}

func (h *Hub) detailResponse(id string, compact bool) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.state.Runs[id]
	if r == nil {
		archived, err := h.archive.RTCGetRun(id)
		if err != nil {
			return nil, fmt.Errorf("run not found")
		}
		r = &archived
	}
	copy := *r
	if compact {
		copy.Endpoints = make(map[string]*Endpoint, len(r.Endpoints))
		for role, endpoint := range r.Endpoints {
			if endpoint != nil {
				view := *endpoint
				view.Events = nil
				copy.Endpoints[role] = &view
			}
		}
	}
	h.decorateRun(&copy)
	b, e := json.Marshal(copy)
	return b, e
}

func decodeStateV2(data []byte, s *state) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(s); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("unexpected trailing probe state")
	}
	return nil
}
