package center

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/cadvisor"
	"github.com/johankoi91/monitor/runtime/internal/credential"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"github.com/johankoi91/monitor/runtime/internal/storage"
	"gopkg.in/yaml.v3"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type AgentConfig struct {
	ID          string `yaml:"id"`
	Secret      string `yaml:"secret"`
	HostName    string `yaml:"host_name"`
	HostAddress string `yaml:"host_address"`
}
type Config struct {
	Site          model.Site          `yaml:"site"`
	Agents        []AgentConfig       `yaml:"agents"`
	RestartRules  []model.RestartRule `yaml:"restart_rules"`
	Notifications notify.Config       `yaml:"notifications"`
}
type agentState struct {
	Session    string
	Connected  bool
	Sequence   uint64
	ReceivedAt time.Time
	Report     model.Report
}
type diskRecord struct {
	Baseline model.Baseline  `json:"baseline"`
	YAML     string          `json:"yaml"`
	Source   string          `json:"source"`
	Change   model.Selection `json:"change"`
}
type Store struct {
	mu           sync.RWMutex
	config       Config
	agents       map[string]*agentState
	record       diskRecord
	dir          string
	lock         *os.File
	storageBad   bool
	checks       map[string]*debounce
	bursts       map[string]*restartBurst
	write        func([]byte) error // injected only by tests
	opJournal    storage.Log
	backend      storage.Backend
	operations   map[string]model.Operation
	requestKeys  map[string]string
	executeReady map[string]string
	acks         map[string]map[string]bool
	opWrite      func(model.AuditRecord, int64) error
}
type Problem struct {
	Status        int
	Code, Message string
}

func (p *Problem) Error() string                     { return p.Message }
func problem(status int, code, message string) error { return &Problem{status, code, message} }
func ID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}

func ReadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	dec.KnownFields(true)
	var c Config
	if err = dec.Decode(&c); err != nil {
		return c, errors.New("invalid center configuration")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return c, errors.New("center configuration must contain one document")
	}
	return c, nil
}
func NewStore(dir string, config Config, backends ...storage.Backend) (*Store, error) {
	if !identifier.MatchString(config.Site.Code) || config.Site.Name == "" || len(config.Agents) == 0 {
		return nil, errors.New("site and registered Agents are required")
	}
	seen := map[string]bool{}
	for _, a := range config.Agents {
		if !identifier.MatchString(a.ID) || seen[a.ID] || credential.ValidateBasic(credential.KindAgent, a.ID, a.Secret) != nil || len(a.Secret) < 24 || a.HostName == "" || a.HostAddress == "" {
			return nil, errors.New("invalid registered Agent configuration")
		}
		seen[a.ID] = true
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another center is using the data directory")
	}
	s := &Store{config: config, agents: map[string]*agentState{}, checks: map[string]*debounce{}, bursts: map[string]*restartBurst{}, dir: dir, lock: lock}
	if len(backends) > 0 {
		s.backend = backends[0]
	}
	s.write = s.atomicWrite
	data, err := os.ReadFile(filepath.Join(dir, "current-baseline.json"))
	if s.backend != nil {
		var found bool
		data, found, err = s.backend.LoadDocument("baseline")
		if err == nil && !found {
			err = os.ErrNotExist
		}
		s.write = func(b []byte) error { return s.backend.SaveDocument("baseline", b) }
	}
	if err != nil && !os.IsNotExist(err) {
		s.Close()
		return nil, err
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if dec.Decode(&s.record) != nil || dec.Decode(&struct{}{}) != io.EOF || !s.record.Baseline.Active || s.record.Baseline.Revision == "" || s.record.Baseline.SavedAt == nil || s.record.Baseline.Definition == nil {
			s.Close()
			return nil, errors.New("baseline record is corrupt; retained for repair")
		}
		definition := s.record.Baseline.Definition
		if validateDefinition(*definition) != nil {
			s.Close()
			return nil, errors.New("invalid saved baseline")
		}
		yamlBytes, err := yaml.Marshal(definition)
		if err != nil || string(yamlBytes) != s.record.YAML {
			s.Close()
			return nil, errors.New("baseline YAML and definition disagree")
		}
	}
	if err = s.openOperations(); err != nil {
		s.Close()
		return nil, err
	}
	if s.backend != nil {
		snapshots, e := s.backend.LoadSnapshots()
		if e != nil {
			s.Close()
			return nil, e
		}
		for id, raw := range snapshots {
			if _, ok := s.Agent(id); !ok {
				continue
			}
			var r model.Report
			if json.Unmarshal(raw, &r) != nil {
				s.Close()
				return nil, errors.New("corrupt stored snapshot")
			}
			s.agents[id] = &agentState{Report: r, Connected: false}
		}
	}
	return s, nil
}
func (s *Store) Close() {
	if s.opJournal != nil {
		s.opJournal.Close()
	}
	if s.lock != nil {
		syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
	}
}
func (s *Store) atomicWrite(data []byte) error {
	if len(data) > 2<<20 {
		return errors.New("baseline exceeds 2 MiB budget")
	}
	path := filepath.Join(s.dir, "current-baseline.json")
	old, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	write := func(content []byte) error {
		f, err := os.CreateTemp(s.dir, ".baseline-*")
		if err != nil {
			return err
		}
		name := f.Name()
		defer os.Remove(name)
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(content)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err = os.Rename(name, path); err != nil {
			return err
		}
		d, err := os.Open(s.dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
	if err := write(data); err != nil {
		// A directory sync can fail after rename. Restore the previous durable view;
		// if rollback fails, readiness and further writes are disabled.
		current, _ := os.ReadFile(path)
		if bytes.Equal(current, data) {
			var rollback error
			if os.IsNotExist(readErr) {
				rollback = os.Remove(path)
				if rollback == nil {
					d, e := os.Open(s.dir)
					if e != nil {
						rollback = e
					} else {
						rollback = d.Sync()
						d.Close()
					}
				}
			} else {
				rollback = write(old)
			}
			if rollback != nil {
				s.storageBad = true
			}
		}
		return err
	}
	return nil
}
func clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var result T
	json.Unmarshal(b, &result)
	return result
}
func (s *Store) Baseline() model.Baseline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.record.Baseline)
}
func (s *Store) YAML(revision string) (string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.record.Baseline.Active {
		return "", "", problem(404, "BASELINE_NOT_FOUND", "尚未保存基线")
	}
	if revision != "" && revision != s.record.Baseline.Revision {
		return "", "", problem(409, "BASELINE_CONFLICT", "基线已变化，请刷新后下载")
	}
	return s.record.YAML, s.record.Baseline.Revision, nil
}
func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.storageBad && (s.opJournal == nil || s.opJournal.Healthy())
}
func (s *Store) Agent(id string) (AgentConfig, bool) {
	for _, a := range s.config.Agents {
		if a.ID == id {
			return a, true
		}
	}
	return AgentConfig{}, false
}
func (s *Store) AgentSummaries() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []map[string]any{}
	for _, cfg := range s.config.Agents {
		a := s.agents[cfg.ID]
		var received *time.Time
		online := false
		if a != nil {
			online = a.Connected
			if !a.ReceivedAt.IsZero() {
				at := a.ReceivedAt
				received = &at
			}
		}
		result = append(result, map[string]any{"agent_id": cfg.ID, "host_name": cfg.HostName, "host_address": cfg.HostAddress, "agent_online": online, "stale": !fresh(a, time.Now()), "received_at": received})
	}
	return map[string]any{"agents": result}
}
func (s *Store) Connect(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := ID()
	state := &agentState{Session: session, Connected: true}
	if previous := s.agents[id]; previous != nil {
		state.Report = previous.Report
	}
	// An offline channel was UNKNOWN; recovery needs new successful probes.
	if d := s.record.Baseline.Definition; d != nil {
		for _, c := range d.Clusters {
			for _, v := range c.Services {
				for _, n := range v.Nodes {
					if n.AgentID == id {
						delete(s.checks, n.ID)
					}
				}
			}
		}
	}
	s.agents[id] = state
	return session
}
func (s *Store) Disconnect(id, session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.agents[id]; a != nil && a.Session == session {
		a.Connected = false
	}
}
func (s *Store) Receive(id, session string, r model.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.agents[id]
	if a == nil || a.Session != session {
		return errors.New("superseded Agent session")
	}
	if r.Type != "snapshot" || r.Sequence <= a.Sequence {
		return errors.New("invalid or reordered snapshot")
	}
	a.Sequence = r.Sequence
	if !r.Complete {
		return nil
	} // Errors/heartbeats never refresh container freshness.
	now := time.Now().UTC()
	wasFresh := fresh(a, now)
	if r.CollectedAt.Before(now.Add(-45*time.Second)) || r.CollectedAt.After(now.Add(5*time.Second)) || len(r.Containers) > 512 {
		return errors.New("invalid snapshot freshness or size")
	}
	names, ids := map[string]bool{}, map[string]bool{}
	for i := range r.Containers {
		o := &r.Containers[i]
		c := &o.Container
		if c.ContainerID == "" || c.ContainerName == "" || stringsInvalidName(c.ContainerName) || names[c.ContainerName] || ids[c.ContainerID] || o.Startup == nil || c.SnapshotID == "" || o.Startup.SnapshotID != c.SnapshotID {
			return errors.New("invalid container identity or startup snapshot")
		}
		if c.CollectedAt.Before(r.CollectedAt) || c.CollectedAt.After(now.Add(5*time.Second)) {
			return errors.New("invalid container collection time")
		}
		cfg, _ := s.Agent(id)
		c.AgentID = id
		c.HostName = cfg.HostName
		c.HostAddress = cfg.HostAddress
		c.ReceivedAt = now
		c.Managed = false
		c.NodeID = ""
		// Bad optional metrics cannot discard a valid Docker health observation.
		if !cadvisor.Valid(c.Resources) {
			c.Resources = nil
		}
		names[c.ContainerName] = true
		ids[c.ContainerID] = true
	}
	a.ReceivedAt = now
	a.Report = r
	if !wasFresh && s.record.Baseline.Definition != nil {
		for _, c := range s.record.Baseline.Definition.Clusters {
			for _, v := range c.Services {
				for _, n := range v.Nodes {
					if n.AgentID == id {
						delete(s.checks, n.ID)
					}
				}
			}
		}
	}
	s.updateChecks(id, a)
	s.updateBursts(id, a, now)
	if s.backend != nil {
		data, _ := json.Marshal(r)
		if err := s.backend.SaveSnapshot(id, data); err != nil {
			s.storageBad = true
			return nil
		}
	}
	return nil
}
func stringsInvalidName(name string) bool {
	return !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`).MatchString(name)
}
func fresh(a *agentState, now time.Time) bool {
	return a != nil && a.Connected && !a.ReceivedAt.IsZero() && now.Sub(a.ReceivedAt) <= 45*time.Second && now.Sub(a.Report.CollectedAt) <= 45*time.Second
}
func (s *Store) managed() map[string]string {
	result := map[string]string{}
	if d := s.record.Baseline.Definition; d != nil {
		for _, c := range d.Clusters {
			for _, v := range c.Services {
				for _, n := range v.Nodes {
					result[n.AgentID+"/"+n.ContainerName] = n.ID
				}
			}
		}
	}
	return result
}
func (s *Store) Containers() []model.Container {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []model.Container{}
	managed := s.managed()
	now := time.Now().UTC()
	for id, a := range s.agents {
		for _, o := range a.Report.Containers {
			c := o.Container
			c.AgentOnline = a.Connected
			c.Stale = !fresh(a, now)
			c.Resources = resourceView(c.Resources, a, now)
			c.Lifecycle = lifecycleView(c.Lifecycle, a, now)
			c.NodeID = managed[id+"/"+c.ContainerName]
			c.Managed = c.NodeID != ""
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].AgentID != result[j].AgentID {
			return result[i].AgentID < result[j].AgentID
		}
		return result[i].ContainerName < result[j].ContainerName
	})
	return result
}
func (s *Store) Detail(id, cid string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.agents[id]
	if a != nil {
		for _, o := range a.Report.Containers {
			if o.Container.ContainerID == cid {
				c := o.Container
				c.AgentOnline = a.Connected
				c.Stale = !fresh(a, time.Now())
				c.Resources = resourceView(c.Resources, a, time.Now().UTC())
				c.Lifecycle = lifecycleView(c.Lifecycle, a, time.Now().UTC())
				c.NodeID = s.managed()[id+"/"+c.ContainerName]
				c.Managed = c.NodeID != ""
				return map[string]any{"container": c, "startup": clone(o.Startup), "started_at": o.StartedAt, "exit_code": o.ExitCode, "restart_count": o.RestartCount, "docker_health": o.DockerHealth}, nil
			}
		}
	}
	return nil, problem(404, "CONTAINER_NOT_FOUND", "容器已不在最新发现快照，请刷新")
}
func (s *Store) Plan(id string) model.Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := model.Plan{Type: "checks", Revision: s.record.Baseline.Revision, Nodes: []model.PlanNode{}}
	if d := s.record.Baseline.Definition; d != nil {
		for _, c := range d.Clusters {
			for _, v := range c.Services {
				for _, n := range v.Nodes {
					if n.AgentID == id {
						p.Nodes = append(p.Nodes, model.PlanNode{ContainerName: n.ContainerName, Checks: clone(n.Checks)})
					}
				}
			}
		}
	}
	return p
}

func (s *Store) Save(selection model.Selection, source string) (model.Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(status int, code, message string) (model.Baseline, error) {
		return model.Baseline{}, problem(status, code, message)
	}
	if s.storageBad {
		return fail(503, "STORAGE_UNAVAILABLE", "存储不可用，未改变当前基线")
	}
	if selection.ExpectedRevision != s.record.Baseline.Revision {
		return fail(409, "BASELINE_CONFLICT", "基线已被更新，请刷新后重新确认差异")
	}
	if selection.Additions == nil || selection.Removals == nil || len(selection.Additions)+len(selection.Removals) == 0 {
		return fail(400, "EMPTY_SELECTION", "必须提交明确新增或移出的成员")
	}
	d := model.Definition{SchemaVersion: 1, Site: s.config.Site, Clusters: []model.Cluster{}}
	if old := s.record.Baseline.Definition; old != nil {
		d = clone(*old)
	}
	original := map[string]model.Node{}
	originalMapping := map[string]string{}
	for _, c := range d.Clusters {
		for _, v := range c.Services {
			for _, n := range v.Nodes {
				original[n.ID] = n
				originalMapping[n.ID] = c.Code + "/" + v.Code
			}
		}
	}
	removals := map[string]bool{}
	for _, id := range selection.Removals {
		if s.busyOperationLocked(id) != "" {
			return fail(409, "OPERATION_IN_PROGRESS", "目标有执行中或结果未知的重启，不能移出或改绑定")
		}
		if removals[id] {
			return fail(400, "DUPLICATE_REMOVAL", "移出成员重复")
		}
		if _, ok := original[id]; !ok {
			return fail(400, "NODE_NOT_FOUND", "移出成员不在基线中")
		}
		removals[id] = true
	}
	for ci := range d.Clusters {
		for si := range d.Clusters[ci].Services {
			service := &d.Clusters[ci].Services[si]
			nodes := []model.Node{}
			for _, node := range service.Nodes {
				if !removals[node.ID] {
					nodes = append(nodes, node)
				}
			}
			service.Nodes = nodes
		}
	}
	bindings := map[string]bool{}
	for _, c := range d.Clusters {
		for _, v := range c.Services {
			for _, n := range v.Nodes {
				bindings[n.AgentID+"/"+n.ContainerName] = true
			}
		}
	}
	for _, add := range selection.Additions {
		if !identifier.MatchString(add.ClusterCode) || len(add.ClusterName) > 128 {
			return fail(400, "INVALID_MAPPING", "请填写合法的集群与服务归属")
		}
		agent := s.agents[add.AgentID]
		if !fresh(agent, time.Now()) {
			return fail(409, "CANDIDATE_STALE", "新增候选离线或已过期，请刷新")
		}
		var candidate *model.Observation
		for i := range agent.Report.Containers {
			if agent.Report.Containers[i].Container.ContainerID == add.ContainerID {
				candidate = &agent.Report.Containers[i]
				break
			}
		}
		if candidate == nil || add.SnapshotID == "" || candidate.Container.SnapshotID != add.SnapshotID {
			return fail(409, "CANDIDATE_CHANGED", "候选容器身份或配置已变化，请刷新")
		}
		container := candidate.Container
		binding := add.AgentID + "/" + container.ContainerName
		if add.ServiceCode == "" && add.ServiceName == "" {
			add.ServiceCode, add.ServiceName = automaticService(container.ContainerName)
			for _, cluster := range s.recordClusters() {
				for _, service := range cluster.Services {
					for _, old := range service.Nodes {
						if old.AgentID == add.AgentID && old.ContainerName == container.ContainerName {
							add.ServiceCode, add.ServiceName = service.Code, service.Name
						}
					}
				}
			}
			for _, cluster := range d.Clusters {
				if cluster.Code == add.ClusterCode {
					for _, service := range cluster.Services {
						if service.Code == add.ServiceCode {
							add.ServiceName = service.Name
						}
					}
				}
			}
		}
		if !identifier.MatchString(add.ServiceCode) || add.ServiceName == "" || len(add.ServiceName) > 128 {
			return fail(400, "INVALID_MAPPING", "服务归属无法生成")
		}
		if bindings[binding] {
			return fail(400, "DUPLICATE_NODE", "同一容器不能重复纳管")
		}
		bindings[binding] = true
		cfg, ok := s.Agent(add.AgentID)
		if !ok {
			return fail(400, "UNKNOWN_AGENT", "未登记的 Agent")
		}
		if err := validateChecks(add.Checks, cfg.HostAddress); err != nil {
			return model.Baseline{}, err
		}
		node := model.Node{ID: binding, AgentID: add.AgentID, HostName: cfg.HostName, HostAddress: cfg.HostAddress, ContainerName: container.ContainerName, Checks: add.Checks}
		if node.Checks == nil {
			node.Checks = []model.Check{}
		}
		for _, old := range original {
			if old.AgentID == node.AgentID && old.ContainerName == node.ContainerName {
				node.ID = old.ID
				node.RestartEnabled = old.RestartEnabled && old.HostName == node.HostName && old.HostAddress == node.HostAddress && originalMapping[old.ID] == add.ClusterCode+"/"+add.ServiceCode
				break
			}
		}
		ci := -1
		for i, c := range d.Clusters {
			if c.Code == add.ClusterCode {
				ci = i
				break
			}
		}
		if ci < 0 {
			name := add.ClusterName
			if name == "" {
				name = add.ClusterCode
			}
			d.Clusters = append(d.Clusters, model.Cluster{Code: add.ClusterCode, Name: name, Product: "RTC", Services: []model.Service{}})
			ci = len(d.Clusters) - 1
		} else if add.ClusterName != "" && d.Clusters[ci].Name != add.ClusterName {
			return fail(400, "MAPPING_CONFLICT", "相同集群编码不能使用不同名称")
		}
		si := -1
		for i, v := range d.Clusters[ci].Services {
			if v.Code == add.ServiceCode {
				si = i
				break
			}
		}
		if si < 0 {
			d.Clusters[ci].Services = append(d.Clusters[ci].Services, model.Service{Code: add.ServiceCode, Name: add.ServiceName, Nodes: []model.Node{}})
			si = len(d.Clusters[ci].Services) - 1
		} else if d.Clusters[ci].Services[si].Name != add.ServiceName {
			return fail(400, "MAPPING_CONFLICT", "相同服务编码不能使用不同名称")
		}
		d.Clusters[ci].Services[si].Nodes = append(d.Clusters[ci].Services[si].Nodes, node)
	}
	sort.Slice(d.Clusters, func(i, j int) bool { return d.Clusters[i].Code < d.Clusters[j].Code })
	for ci := range d.Clusters {
		c := &d.Clusters[ci]
		sort.Slice(c.Services, func(i, j int) bool { return c.Services[i].Code < c.Services[j].Code })
		for si := range c.Services {
			nodes := c.Services[si].Nodes
			sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
		}
	}
	if err := validateDefinition(d); err != nil {
		return fail(400, "INVALID_BASELINE", err.Error())
	}
	yamlBytes, err := yaml.Marshal(d)
	if err != nil {
		return fail(400, "INVALID_YAML", "无法生成 YAML")
	}
	at := time.Now().UTC()
	revision := ID()
	download := "/api/v1/baseline/yaml?revision=" + revision
	record := diskRecord{Baseline: model.Baseline{Revision: revision, Active: true, SavedAt: &at, Definition: &d, YAMLURL: &download}, YAML: string(yamlBytes), Source: source, Change: selection}
	data, err := json.Marshal(record)
	if err != nil {
		return fail(400, "INVALID_BASELINE", "无法编码基线")
	}
	if err = s.write(data); err != nil {
		return fail(503, "BASELINE_WRITE_FAILED", "保存失败，当前生效基线未改变")
	}
	s.record = record
	return clone(record.Baseline), nil
}
func validateDefinition(d model.Definition) error {
	if d.SchemaVersion != 1 || !identifier.MatchString(d.Site.Code) || d.Site.Name == "" {
		return errors.New("invalid baseline site")
	}
	ids, bindings, clusters := map[string]bool{}, map[string]bool{}, map[string]bool{}
	count := 0
	for _, c := range d.Clusters {
		if !identifier.MatchString(c.Code) || c.Name == "" || c.Product != "RTC" || clusters[c.Code] {
			return errors.New("invalid or duplicate RTC cluster")
		}
		clusters[c.Code] = true
		services := map[string]bool{}
		for _, v := range c.Services {
			if !identifier.MatchString(v.Code) || v.Name == "" || services[v.Code] {
				return errors.New("invalid or duplicate service")
			}
			services[v.Code] = true
			for _, n := range v.Nodes {
				binding := n.AgentID + "/" + n.ContainerName
				if n.ID == "" || ids[n.ID] || bindings[binding] || !identifier.MatchString(n.AgentID) || stringsInvalidName(n.ContainerName) || n.HostName == "" || n.HostAddress == "" {
					return errors.New("invalid or duplicate baseline node")
				}
				ids[n.ID] = true
				bindings[binding] = true
				count++
			}
		}
	}
	if count > 2048 {
		return fmt.Errorf("baseline exceeds 2048 nodes")
	}
	return nil
}
