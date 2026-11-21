package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAutomaticIdentityIsStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadIdentity(dir)
	if err != nil || first != second || first.ID == "" || len(first.Secret) < 24 {
		t.Fatal("unstable identity", err)
	}
	info, _ := os.Stat(filepath.Join(dir, "agent-identity.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("identity file permissions")
	}
	other, _ := LoadIdentity(t.TempDir())
	if reflect.DeepEqual(first, other) {
		t.Fatal("machines share an identity")
	}
	os.WriteFile(filepath.Join(dir, "agent-identity.json"), []byte("broken"), 0600)
	if _, err = LoadIdentity(dir); err == nil {
		t.Fatal("corrupt identity silently regenerated")
	}
}
func TestReportedHostUsesDockerNameAndObservedIP(t *testing.T) {
	host := HostInfo("physical-host", "", "203.0.113.20")
	if host.HostName != "physical-host" || host.HostnameSource != "docker_info" || host.HostAddress != "203.0.113.20" || host.AddressSource != "center_observed" || host.Validate() != nil {
		t.Fatal(host)
	}
	override := HostInfo("physical-host", "111.230.192.59", "203.0.113.20")
	if override.HostAddress != "111.230.192.59" || override.AddressSource != "configured" {
		t.Fatal(override)
	}
}

func TestSpecifiedCloudIPIsIncludedInReportedAddresses(t *testing.T) {
	host := HostInfo("cloud-server", "111.230.192.59", "203.0.113.20")
	found := false
	for _, ip := range host.IPAddresses {
		if ip == "111.230.192.59" {
			found = true
		}
	}
	if !found || host.HostAddress != "111.230.192.59" || host.AddressSource != "configured" || host.Validate() != nil {
		t.Fatal("specified cloud IP not reported", host)
	}
}
