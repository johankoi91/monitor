package center

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"gopkg.in/yaml.v3"
)

const agentRegistryDocument = "agent-registry"

type AgentRegistration struct {
	ID             string         `json:"agent_id"`
	CredentialHash string         `json:"credential_hash"`
	Host           model.HostInfo `json:"host_info"`
	RegisteredAt   time.Time      `json:"registered_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
type agentRegistry struct {
	Agents map[string]AgentRegistration `json:"agents"`
}

func agentCredentialHash(secret string) string {
	v := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(v[:])
}
func (s *Store) openAgentRegistry() error {
	var data []byte
	var err error
	if s.backend != nil {
		var found bool
		data, found, err = s.backend.LoadDocument(agentRegistryDocument)
		if !found && err == nil {
			return nil
		}
	} else {
		data, err = os.ReadFile(filepath.Join(s.dir, "agent-registry.json"))
		if os.IsNotExist(err) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	var r agentRegistry
	if json.Unmarshal(data, &r) != nil || r.Agents == nil || len(r.Agents) > 512 {
		return errors.New("invalid Agent registry")
	}
	for id, entry := range r.Agents {
		if id != entry.ID || !identifier.MatchString(id) || len(entry.CredentialHash) != 64 || entry.Host.Validate() != nil {
			return errors.New("invalid Agent registry record")
		}
	}
	s.registry = r.Agents
	return nil
}
func (s *Store) saveAgentRegistryLocked(next map[string]AgentRegistration) error {
	data, err := json.Marshal(agentRegistry{Agents: next})
	if err != nil {
		return err
	}
	if s.backend != nil {
		err = s.backend.SaveDocument(agentRegistryDocument, data)
	} else {
		err = durable.Write(filepath.Join(s.dir, "agent-registry.json"), agentRegistry{Agents: next})
	}
	if err != nil {
		return err
	}
	s.registry = next
	return nil
}
func (s *Store) EnrollAgent(id, secret string, host model.HostInfo) error {
	if !identifier.MatchString(id) || len(secret) < 24 || len(secret) > 256 || host.Validate() != nil {
		return errors.New("invalid Agent enrollment")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := agentCredentialHash(secret)
	if old, ok := s.registry[id]; ok {
		if old.CredentialHash != hash {
			return errors.New("Agent identity already owned")
		}
		return nil
	}
	if len(s.registry) >= 512 {
		return errors.New("Agent registry capacity reached")
	}
	for _, old := range s.registry {
		if old.CredentialHash == hash {
			return errors.New("Agent credentials must be independent")
		}
	}
	next := clone(s.registry)
	now := time.Now().UTC()
	next[id] = AgentRegistration{ID: id, CredentialHash: hash, Host: host, RegisteredAt: now, UpdatedAt: now}
	return s.saveAgentRegistryLocked(next)
}
func (s *Store) AuthenticateAgent(id, secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.registry[id]
	if !ok {
		return false
	}
	a, b := []byte(r.CredentialHash), []byte(agentCredentialHash(secret))
	return subtle.ConstantTimeCompare(a, b) == 1
}
func (s *Store) AgentCredentialMatches(secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hash := agentCredentialHash(secret)
	for _, r := range s.registry {
		if r.CredentialHash == hash {
			return true
		}
	}
	return false
}
func (s *Store) agentLocked(id string) (AgentRegistration, bool) {
	r, ok := s.registry[id]
	return r, ok
}
func (s *Store) Agent(id string) (AgentRegistration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.agentLocked(id)
	return clone(r), ok
}

// RetireAgent removes an offline host after its baseline memberships are removed.
// Historical reports stay archived; only registered hosts are restored at startup.
func (s *Store) RetireAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registry[id]; !ok {
		return &Problem{Status: 404, Code: "AGENT_NOT_FOUND", Message: "Agent 不存在"}
	}
	if a := s.agents[id]; a != nil && a.Connected {
		return &Problem{Status: 409, Code: "AGENT_CONNECTED", Message: "请先停止 Agent"}
	}
	for _, cluster := range s.recordClusters() {
		for _, service := range cluster.Services {
			for _, node := range service.Nodes {
				if node.AgentID == id {
					return &Problem{Status: 409, Code: "AGENT_IN_BASELINE", Message: "请先移除该 Agent 的基准成员"}
				}
			}
		}
	}
	next := clone(s.registry)
	delete(next, id)
	if err := s.saveAgentRegistryLocked(next); err != nil {
		return err
	}
	delete(s.agents, id)
	delete(s.lastStartups, id)
	return nil
}
func (s *Store) registeredAgentsLocked() []AgentRegistration {
	out := make([]AgentRegistration, 0, len(s.registry))
	for _, r := range s.registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Store) updateReportedHostLocked(id string, host model.HostInfo) error {
	old, ok := s.registry[id]
	if !ok {
		return errors.New("unregistered Agent")
	}
	// Persist changes in host identity and addresses, not a write every heartbeat.
	if old.Host.HostName == host.HostName && old.Host.HostAddress == host.HostAddress && sameHostAddresses(old.Host.IPAddresses, host.IPAddresses) && old.Host.HostnameSource == host.HostnameSource && old.Host.AddressSource == host.AddressSource {
		return s.syncBaselineHostLocked(id, host)
	}
	next := clone(s.registry)
	old.Host = clone(host)
	old.UpdatedAt = time.Now().UTC()
	next[id] = old
	if err := s.saveAgentRegistryLocked(next); err != nil {
		return err
	}
	return s.syncBaselineHostLocked(id, host)
}
func sameHostAddresses(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (s *Store) syncBaselineHostLocked(id string, host model.HostInfo) error {
	if s.record.Baseline.Definition == nil {
		return nil
	}
	next := clone(s.record)
	changed := false
	for ci := range next.Baseline.Definition.Clusters {
		for si := range next.Baseline.Definition.Clusters[ci].Services {
			for ni := range next.Baseline.Definition.Clusters[ci].Services[si].Nodes {
				node := &next.Baseline.Definition.Clusters[ci].Services[si].Nodes[ni]
				if node.AgentID == id && (node.HostName != host.HostName || node.HostAddress != host.HostAddress) {
					for i := range node.Checks {
						if node.Checks[i].Host == node.HostAddress {
							node.Checks[i].Host = host.HostAddress
						}
					}
					node.HostName = host.HostName
					node.HostAddress = host.HostAddress
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	data, err := yaml.Marshal(next.Baseline.Definition)
	if err != nil {
		return err
	}
	next.YAML = string(data)
	next.Baseline.Revision = ID()
	now := time.Now().UTC()
	next.Baseline.SavedAt = &now
	url := "/api/v1/baseline/yaml?revision=" + next.Baseline.Revision
	next.Baseline.YAMLURL = &url
	next.Source = "agent:" + id
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = s.write(encoded); err != nil {
		return err
	}
	s.record = next
	return nil
}
