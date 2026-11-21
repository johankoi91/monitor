package probe

import (
	"context"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSharedHostIdentityGroupsConcurrentClientsAndPersists(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	hosts := make([]HostIdentity, 8)
	errs := make([]error, 8)
	for i := range hosts {
		wg.Add(1)
		go func(i int) { defer wg.Done(); hosts[i], errs[i] = LoadHostIdentity(dir) }(i)
	}
	wg.Wait()
	for i, h := range hosts {
		if errs[i] != nil || h.Host.ID != hosts[0].Host.ID || h.Secret != hosts[0].Secret {
			t.Fatal("shared identity raced", errs[i])
		}
	}
	native, err := loadRuntimeIdentity(t.TempDir(), hosts[0], "rtc", "native")
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := loadRuntimeIdentity(t.TempDir(), hosts[0], "rtc", "native")
	if err != nil {
		t.Fatal(err)
	}
	web, err := loadRuntimeIdentity(t.TempDir(), hosts[0], "rtc", "web")
	if err != nil {
		t.Fatal(err)
	}
	if native.ID != rebuilt.ID || native.Secret != rebuilt.Secret || native.ID == web.ID || native.Secret == web.Secret {
		t.Fatal("client identity is not scoped to server/service/client")
	}
	info, err := os.Stat(filepath.Join(dir, "server-identity-v2.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("server identity permissions")
	}
}
func TestOldIdentityEnvironmentAndDataAreRejected(t *testing.T) {
	t.Setenv("RTC_PROBE_ID", "old-runtime")
	if _, err := EnvironmentConfig("agora"); err == nil {
		t.Fatal("old identity environment accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "outbox.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	host, err := LoadHostIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loadRuntimeIdentity(dir, host, "rtc", "native"); err == nil {
		t.Fatal("old runtime directory accepted")
	}
}

func TestAssignmentCannotCrossServerServiceOrClient(t *testing.T) {
	c := &Client{identity: RuntimeIdentity{ID: "runtime-a"}, host: HostIdentity{Host: protocol.ProbeHostInfo{ID: "host-a"}}, config: Config{ServiceType: "rtc", ClientType: "native"}}
	base := protocol.Job{RuntimeID: "runtime-a", HostID: "host-a", ServiceType: "rtc", ClientType: "native"}
	for _, field := range []string{"server", "service", "client"} {
		job := base
		switch field {
		case "server":
			job.HostID = "host-b"
		case "service":
			job.ServiceType = "rtm"
		case "client":
			job.ClientType = "web"
		}
		if err := c.prepare(context.Background(), job); err == nil || err.Error() != "assignment owner mismatch" {
			t.Fatal("foreign assignment accepted", field, err)
		}
	}
}

func TestExecutionRejectsMissingEdgeFormat(t *testing.T) {
	c := &Client{identity: RuntimeIdentity{ID: "runtime-a"}, host: HostIdentity{Host: protocol.ProbeHostInfo{ID: "host-a"}}, config: Config{Driver: "agora", ServiceType: "rtc", ClientType: "native"}, records: map[string]*record{}}
	job := protocol.Job{RunID: "run-a", RuntimeID: "runtime-a", HostID: "host-a", ServiceType: "rtc", ClientType: "native", Target: protocol.Target{Driver: "agora"}, Role: "sender", Revision: "v2", Deadline: time.Now().Add(time.Minute), NativeCase: &protocol.NativeCase{}}
	if err := c.prepare(context.Background(), job); err == nil || err.Error() != "unsupported Edge address format" {
		t.Fatal("missing execution format accepted", err)
	}
}
