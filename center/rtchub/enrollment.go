package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

type ProbeHostRegistration struct {
	protocol.ProbeHostInfo
	SecretHash   string    `json:"secret_hash"`
	RegisteredAt time.Time `json:"registered_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var serviceName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func credentialHash(secret string) string {
	v := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(v[:])
}
func probeSecretMatches(p RuntimeRegistration, secret string) bool {
	expected := p.SecretHash
	actual := credentialHash(secret)
	return secret != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
func (h *Hub) ProbeCredentialMatches(secret string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if secret == "" {
		return false
	}
	for _, p := range h.runtimesLocked() {
		if probeSecretMatches(p, secret) {
			return true
		}
	}
	return false
}

func (h *Hub) EnrollProbe(input protocol.ProbeEnrollment, observed string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	input.Host.Name = strings.TrimSpace(input.Host.Name)
	input.Host.Address = strings.TrimSpace(input.Host.Address)
	if input.Host.Address == "" {
		input.Host.Address = observed
	}
	if !protocol.ValidID(input.Host.ID) || !protocol.ValidID(input.RuntimeID) || input.Host.ID == input.RuntimeID || len(input.HostSecret) < 24 || len(input.HostSecret) > 256 || len(input.RuntimeSecret) < 24 || len(input.RuntimeSecret) > 256 || input.HostSecret == input.RuntimeSecret || input.Host.Name == "" || len(input.Host.Name) > 200 || len(input.Host.Location) > 200 || net.ParseIP(input.Host.Address) == nil || len(input.Name) > 128 || !serviceName.MatchString(input.ServiceType) || !serviceName.MatchString(input.ClientType) {
		return errors.New("invalid server identity or service/client advertisement")
	}
	if input.Driver != "" && input.Driver != "mock" {
		service, client := protocol.DriverService(input.Driver)
		if service == "" || service != input.ServiceType || client != input.ClientType {
			return errors.New("driver does not match advertised service/client")
		}
	}
	if old, ok := h.state.ProbeHosts[input.Host.ID]; ok {
		hash := credentialHash(input.HostSecret)
		if subtle.ConstantTimeCompare([]byte(old.SecretHash), []byte(hash)) != 1 {
			return errors.New("server identity proof rejected")
		}
	}
	for id, host := range h.state.ProbeHosts {
		if id == input.RuntimeID || host.SecretHash == credentialHash(input.RuntimeSecret) || (id != input.Host.ID && host.SecretHash == credentialHash(input.HostSecret)) {
			return errors.New("server and client identities/credentials must be independent")
		}
	}
	for _, old := range h.runtimesLocked() {
		if old.ID == input.Host.ID {
			return errors.New("server and runtime identities must be distinct")
		}
		if old.ID == input.RuntimeID {
			if !probeSecretMatches(old, input.RuntimeSecret) {
				return errors.New("existing runtime identity cannot be replaced")
			}
			if old.HostID != input.Host.ID {
				return errors.New("runtime belongs to another server")
			}
			if old.ServiceType != input.ServiceType || old.ClientType != input.ClientType {
				return errors.New("runtime service/client cannot change")
			}
		} else if old.HostID == input.Host.ID && old.ServiceType == input.ServiceType && old.ClientType == input.ClientType {
			return errors.New("server client already has an identity; reuse its persisted identity")
		} else if probeSecretMatches(old, input.RuntimeSecret) {
			return errors.New("runtime credentials must be independent")
		}
		if probeSecretMatches(old, input.HostSecret) {
			return errors.New("server and runtime credentials must be independent")
		}
	}
	if _, exists := h.state.ProbeHosts[input.Host.ID]; !exists && len(h.state.ProbeHosts) >= 1024 {
		return errors.New("probe server registry capacity reached")
	}
	if _, exists := h.state.Runtimes[input.RuntimeID]; !exists && len(h.runtimesLocked()) >= 4096 {
		return errors.New("probe runtime registry capacity reached")
	}
	// Repeated announcements do not generate control-document writes or reset an
	// administrator's disabled state, tasks, or the runtime's private credential.
	host := h.state.ProbeHosts[input.Host.ID]
	previous, exists := h.state.Runtimes[input.RuntimeID]
	if exists && host.ProbeHostInfo == input.Host && previous.Name == input.Name && previous.ServiceType == input.ServiceType && previous.ClientType == input.ClientType && previous.Driver == input.Driver {
		return nil
	}
	now := time.Now().UTC()
	return h.change(func(s *state) error {
		if s.ProbeHosts == nil {
			s.ProbeHosts = map[string]ProbeHostRegistration{}
		}
		registration := s.ProbeHosts[input.Host.ID]
		registration.ProbeHostInfo = input.Host
		registration.SecretHash = credentialHash(input.HostSecret)
		registration.UpdatedAt = now
		if registration.RegisteredAt.IsZero() {
			registration.RegisteredAt = now
		}
		s.ProbeHosts[input.Host.ID] = registration
		location := input.Host.Location
		if location == "" {
			location = input.Host.Name
		}
		s.Runtimes[input.RuntimeID] = RuntimeRegistration{ID: input.RuntimeID, Name: input.Name, SecretHash: credentialHash(input.RuntimeSecret), HostID: input.Host.ID, HostName: input.Host.Name, HostAddress: input.Host.Address, Location: location, ServiceType: input.ServiceType, ClientType: input.ClientType, Driver: input.Driver}
		// One host announcement updates metadata for the host's other runtimes too.
		for id, p := range s.Runtimes {
			if p.HostID == input.Host.ID {
				p.HostName = input.Host.Name
				p.HostAddress = input.Host.Address
				p.Location = location
				s.Runtimes[id] = p
			}
		}
		return nil
	})
}

func (h *Hub) hostViewsLocked(runtimes []any) []any {
	ids := make([]string, 0, len(h.state.ProbeHosts))
	for id := range h.state.ProbeHosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		host := h.state.ProbeHosts[id]
		groups := map[string][]any{}
		online := false
		for _, raw := range runtimes {
			runtime := raw.(map[string]any)
			if runtime["host_id"] != id {
				continue
			}
			service := runtime["service_type"].(string)
			groups[service] = append(groups[service], runtime)
			if runtime["online"] == true {
				online = true
			}
		}
		types := make([]string, 0, len(groups))
		for kind := range groups {
			types = append(types, kind)
		}
		sort.Strings(types)
		services := make([]any, 0, len(types))
		for _, kind := range types {
			services = append(services, map[string]any{"service_type": kind, "clients": groups[kind]})
		}
		out = append(out, map[string]any{"host_id": id, "host_name": host.Name, "host_address": host.Address, "location": host.Location, "online": online, "services": services, "registered_at": host.RegisteredAt, "updated_at": host.UpdatedAt})
	}
	return out
}

// A v2 document is authoritative: malformed or ambiguous registrations must fail
// startup rather than silently selecting an execution client.
func validateRegistry(s state) error {
	invalid := func() error { return errors.New("invalid v2 server/client registry") }
	if len(s.ProbeHosts) > 1024 || len(s.Runtimes) > 4096 {
		return invalid()
	}
	hashes := map[string]bool{}
	tuples := map[string]bool{}
	for id, host := range s.ProbeHosts {
		hash, err := hex.DecodeString(host.SecretHash)
		if err != nil || len(hash) != 32 || hashes[host.SecretHash] || !protocol.ValidID(id) || host.ID != id || host.Name == "" || len(host.Name) > 200 || len(host.Location) > 200 || net.ParseIP(host.Address) == nil {
			return invalid()
		}
		hashes[host.SecretHash] = true
	}
	for id, runtime := range s.Runtimes {
		hash, err := hex.DecodeString(runtime.SecretHash)
		host, exists := s.ProbeHosts[runtime.HostID]
		key := clientKey(runtime.HostID, runtime.ServiceType, runtime.ClientType)
		if err != nil || len(hash) != 32 || hashes[runtime.SecretHash] || !protocol.ValidID(id) || runtime.ID != id || !exists || s.ProbeHosts[id].ID != "" || !protocol.ValidServiceType(runtime.ServiceType) || !protocol.ValidServiceType(runtime.ClientType) || tuples[key] || runtime.HostName != host.Name || runtime.HostAddress != host.Address {
			return invalid()
		}
		if runtime.Driver != "" && runtime.Driver != "mock" {
			service, client := protocol.DriverService(runtime.Driver)
			if service != runtime.ServiceType || client != runtime.ClientType {
				return invalid()
			}
		}
		hashes[runtime.SecretHash] = true
		tuples[key] = true
	}
	for key := range s.DisabledClients {
		if !tuples[key] {
			return invalid()
		}
	}
	return nil
}
