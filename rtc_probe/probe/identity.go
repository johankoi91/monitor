package probe

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type HostIdentity struct {
	Version int                    `json:"version"`
	Host    protocol.ProbeHostInfo `json:"host"`
	Secret  string                 `json:"secret"`
}
type RuntimeIdentity struct {
	Version     int    `json:"version"`
	ID          string `json:"runtime_id"`
	Secret      string `json:"secret"`
	HostID      string `json:"host_id"`
	ServiceType string `json:"service_type"`
	ClientType  string `json:"client_type"`
}

func LoadHostIdentity(dir string) (HostIdentity, error) {
	if dir == "" {
		return HostIdentity{}, errors.New("shared server identity directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return HostIdentity{}, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "identity.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return HostIdentity{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return HostIdentity{}, err
	}
	path := filepath.Join(dir, "server-identity-v2.json")
	var identity HostIdentity
	if err = durable.Read(path, 8192, &identity); err == nil {
		if identity.Version != 2 || !protocol.ValidID(identity.Host.ID) || len(identity.Secret) < 24 || identity.Host.Name == "" {
			return identity, errors.New("invalid v2 server identity")
		}
		return identity, nil
	} else if !os.IsNotExist(err) {
		return identity, err
	}
	name, err := os.Hostname()
	if err != nil || name == "" {
		return identity, errors.New("server hostname unavailable")
	}
	identity = HostIdentity{Version: 2, Host: protocol.ProbeHostInfo{ID: "server-" + protocol.ID(), Name: name}, Secret: protocol.ID() + protocol.ID()}
	if err = durable.Write(path, identity); err != nil {
		return HostIdentity{}, err
	}
	return identity, nil
}
func loadRuntimeIdentity(dir string, host HostIdentity, service, client string) (RuntimeIdentity, error) {
	var identity RuntimeIdentity
	for _, name := range []string{"outbox.json", "agent-identity.json", "probe-identity.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return identity, errors.New("old probe directory unsupported; use a fresh v2 data directory")
		} else if !os.IsNotExist(err) {
			return identity, err
		}
	}
	expectedID, expectedSecret := protocol.RuntimeIdentityKey(host.Secret, service, client)
	path := filepath.Join(dir, "runtime-identity-v2.json")
	if err := durable.Read(path, 8192, &identity); err == nil {
		if identity.Version != 2 || identity.ID != expectedID || identity.Secret != expectedSecret || identity.HostID != host.Host.ID || identity.ServiceType != service || identity.ClientType != client {
			return identity, errors.New("runtime identity does not match its server/service/client")
		}
		return identity, nil
	} else if !os.IsNotExist(err) {
		return identity, err
	}
	identity = RuntimeIdentity{Version: 2, ID: expectedID, Secret: expectedSecret, HostID: host.Host.ID, ServiceType: service, ClientType: client}
	if err := durable.Write(path, identity); err != nil {
		return RuntimeIdentity{}, err
	}
	return identity, nil
}
func validHostOverride(name, address string) bool {
	return len(name) <= 200 && (address == "" || net.ParseIP(address) != nil)
}
