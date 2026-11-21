package hub

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func serverAnnouncement(client string) protocol.ProbeEnrollment {
	id, secret := protocol.RuntimeIdentityKey(strings.Repeat("h", 64), "rtc", client)
	return protocol.ProbeEnrollment{Host: protocol.ProbeHostInfo{ID: "server-one", Name: "Probe server", Address: "192.0.2.80", Location: "test"}, HostSecret: strings.Repeat("h", 64), RuntimeID: id, RuntimeSecret: secret, ServiceType: "rtc", ClientType: client, Driver: map[string]string{"native": "agora", "web": "agora-web"}[client]}
}
func TestServerActiveAnnouncementsGroupClientsAndPersistIdentities(t *testing.T) {
	dir := t.TempDir()
	h, err := New(dir, Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	native, web := serverAnnouncement("native"), serverAnnouncement("web")
	for _, a := range []protocol.ProbeEnrollment{native, web} {
		if err = h.EnrollProbe(a, "192.0.2.80"); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.state.ProbeHosts) != 1 || len(h.state.Runtimes) != 2 {
		t.Fatal("clients did not group under one server")
	}
	if h.runtimeForTask(protocol.Task{HostID: "server-one", ServiceType: "rtc", ClientType: "native"}) != native.RuntimeID || h.runtimeForTask(protocol.Task{HostID: "server-one", ServiceType: "rtc", ClientType: "web"}) != web.RuntimeID {
		t.Fatal("task binding did not resolve by server/service/client")
	}
	for _, path := range []string{"/api/v1/probe/connect", "/api/v1/probes/uploads/old-whole-bundle"} {
		request := httptest.NewRequest("PUT", path, strings.NewReader("fixture"))
		request.SetBasicAuth(native.RuntimeID, native.RuntimeSecret)
		response := httptest.NewRecorder()
		h.ServeProbe(response, request)
		if response.Code != 404 {
			t.Fatal("old connection/upload interface remains available", path, response.Code)
		}
	}
	public, _ := json.Marshal(h.Snapshot())
	for _, secret := range []string{native.HostSecret, native.RuntimeSecret, web.RuntimeSecret, credentialHash(native.HostSecret), credentialHash(native.RuntimeSecret)} {
		if strings.Contains(string(public), secret) {
			t.Fatal("credential exposed in public inventory")
		}
	}
	var view struct {
		Hosts []struct {
			Services []struct {
				Clients []any `json:"clients"`
			} `json:"services"`
		} `json:"hosts"`
	}
	if json.Unmarshal(public, &view) != nil || len(view.Hosts) != 1 || len(view.Hosts[0].Services) != 1 || len(view.Hosts[0].Services[0].Clients) != 2 {
		t.Fatal("invalid host/service/client hierarchy")
	}
	if err = h.EnableProbe(native.RuntimeID, false); err != nil {
		t.Fatal(err)
	}
	if err = h.EnrollProbe(native, "192.0.2.80"); err != nil {
		t.Fatal(err)
	}
	if !h.runtimeDisabled(native.RuntimeID) || h.runtimeDisabled(web.RuntimeID) {
		t.Fatal("announcement changed the independent client enable states")
	}
	restored, err := New(dir, Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Authenticate(native.RuntimeID, native.RuntimeSecret) || restored.Authenticate(native.RuntimeID, "wrong") {
		t.Fatal("runtime credential persistence failed")
	}
	if !restored.runtimeDisabled(native.RuntimeID) {
		t.Fatal("disabled state was reset on restart")
	}
}
func TestServerProofAndRuntimeOwnershipCannotBeReplaced(t *testing.T) {
	h, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := serverAnnouncement("native")
	if err = h.EnrollProbe(a, "192.0.2.80"); err != nil {
		t.Fatal(err)
	}
	wrongHost := serverAnnouncement("web")
	wrongHost.HostSecret = strings.Repeat("x", 64)
	wrongRuntime := a
	wrongRuntime.RuntimeSecret = strings.Repeat("x", 64)
	moved := a
	moved.Host.ID = "other-server"
	changed := a
	changed.ServiceType = "rtm"
	changed.Driver = ""
	duplicate := a
	duplicate.RuntimeID = "another-runtime"
	duplicate.RuntimeSecret = strings.Repeat("x", 64)
	for _, v := range []protocol.ProbeEnrollment{wrongHost, wrongRuntime, moved, changed, duplicate} {
		if err = h.EnrollProbe(v, "192.0.2.80"); err == nil {
			t.Fatal("unproved identity replacement accepted")
		}
	}
}
func TestFutureServiceMetadataDoesNotRequireClosedRTCEnums(t *testing.T) {
	h, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := serverAnnouncement("native")
	a.ServiceType = "rtm"
	a.ClientType = "python"
	a.Driver = ""
	if err = h.EnrollProbe(a, "192.0.2.80"); err != nil {
		t.Fatal(err)
	}
	if h.runtimeForTask(protocol.Task{HostID: a.Host.ID, ServiceType: "rtc", ClientType: "native"}) != "" {
		t.Fatal("future service runtime was selected for RTC")
	}
}
func TestV1IdentityBindingsAndManualRegistrationAreNotLoaded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"registered":{"old":{"ID":"old","Secret":"old-secret"}},"plan":{"tasks":[{"task_id":"old-task","probe_id":"old"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := New(dir, Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.state.Runtimes) != 0 || len(h.config.Tasks) != 0 || h.Authenticate("old", "old-secret") {
		t.Fatal("old identities or tasks were imported")
	}
	w := httptest.NewRecorder()
	h.ServeAdmin(w, httptest.NewRequest("POST", "/api/v1/rtc/probes", strings.NewReader(`{}`)))
	if w.Code != 404 {
		t.Fatal("manual registration API remains enabled")
	}
}
func TestDisablingOneClientDoesNotCancelOtherClientSummaries(t *testing.T) {
	h, err := New(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	native, web := serverAnnouncement("native"), serverAnnouncement("web")
	for _, a := range []protocol.ProbeEnrollment{native, web} {
		if err = h.EnrollProbe(a, "192.0.2.80"); err != nil {
			t.Fatal(err)
		}
	}
	for _, client := range []string{"native", "web"} {
		task := protocol.Task{ID: client, HostID: "server-one", ServiceType: "rtc", ClientType: client, TargetID: "rtc", Enabled: true}
		h.config.Tasks = append(h.config.Tasks, task)
		h.state.Runs[client] = &Run{ID: client, TaskID: client, Kind: nativeCoverageKind, Status: "RUNNING", NativePlan: &protocol.NativePlan{Phase: "AP"}, CreatedAt: time.Now(), Endpoints: map[string]*Endpoint{}, Logs: map[string]protocol.LogRequest{}, Cases: []NativeCaseResult{{Status: "PENDING"}}}
	}
	if err = h.EnableProbe(native.RuntimeID, false); err != nil {
		t.Fatal(err)
	}
	if h.state.Runs["native"].Status != "CANCELLED" || h.state.Runs["web"].Status != "RUNNING" {
		t.Fatal("client disable crossed service/client boundary")
	}
}

func TestV2RegistryRejectsAmbiguousOrCorruptStoredIdentities(t *testing.T) {
	for _, kind := range []string{"orphan-client", "duplicate-tuple", "shared-secret", "wrong-key", "old-field", "trailing-document"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			h, err := New(dir, Config{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			a := serverAnnouncement("native")
			if err = h.EnrollProbe(a, "192.0.2.80"); err != nil {
				t.Fatal(err)
			}
			r := h.state.Runtimes[a.RuntimeID]
			switch kind {
			case "orphan-client":
				r.HostID = "missing"
				h.state.Runtimes[r.ID] = r
			case "duplicate-tuple":
				r.ID = "another-runtime"
				r.SecretHash = credentialHash(strings.Repeat("z", 64))
				h.state.Runtimes[r.ID] = r
			case "shared-secret":
				r.SecretHash = credentialHash(a.HostSecret)
				h.state.Runtimes[r.ID] = r
			case "wrong-key":
				delete(h.state.Runtimes, r.ID)
				h.state.Runtimes["wrong-key"] = r
			}
			data, err := json.Marshal(h.state)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "old-field" {
				data = []byte(strings.Replace(string(data), `"runtimes":`, `"registered":{},"runtimes":`, 1))
			}
			if kind == "trailing-document" {
				data = append(data, []byte(` {}`)...)
			}
			if err = os.WriteFile(filepath.Join(dir, "state-v2.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = New(dir, Config{}, nil); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}
