package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func TestProbeServerEnrollmentRequiresIndependentSiteCredential(t *testing.T) {
	h, err := hub.New(t.TempDir(), hub.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("e", 64)
	server := (&Server{Store: setup(t), Probes: h, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), AgentEnrollmentToken: strings.Repeat("a", 64), ProbeEnrollmentToken: token}).Handler()
	hostSecret := strings.Repeat("h", 64)
	id, secret := protocol.RuntimeIdentityKey(hostSecret, "rtc", "native")
	input := protocol.ProbeEnrollment{Host: protocol.ProbeHostInfo{ID: "server-new", Name: "New server"}, HostSecret: hostSecret, RuntimeID: id, RuntimeSecret: secret, ServiceType: "rtc", ClientType: "native", Driver: "agora"}
	call := func(value protocol.ProbeEnrollment, credential, origin string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(value)
		r := httptest.NewRequest("POST", "http://center/api/v1/probes/enroll", bytes.NewReader(b))
		r.SetBasicAuth("probe-enroll", credential)
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	if call(input, "wrong", "").Code != 401 {
		t.Fatal("unapproved server accepted")
	}
	if call(input, token, "http://center").Code != 403 {
		t.Fatal("browser enrollment accepted")
	}
	response := call(input, token, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), hostSecret) {
		t.Fatal("enrollment response exposed private credentials")
	}
	if !h.Authenticate(id, secret) {
		t.Fatal("new runtime could not authenticate")
	}
	reused := input
	reused.RuntimeSecret = token
	if call(reused, token, "").Code != 400 {
		t.Fatal("bootstrap token reused as runtime credential")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRequest(method, "http://center/api/v1/rtc/probes", strings.NewReader(`{}`))
		r.SetBasicAuth("admin", strings.Repeat("z", 32))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("old manual registration route survived", method, w.Code)
		}
	}
	if routePermission("/api/v1/probes/runtimes/id/enabled", "POST") != "probes.manage" {
		t.Fatal("generic server management lacks its own permission")
	}
}
