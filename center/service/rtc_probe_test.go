package center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/center/rtchub"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func TestRTCProbeMachineAndManagementCredentialsStaySeparate(t *testing.T) {
	h, err := hub.New(t.TempDir(), hub.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.EnrollProbe(protocol.ProbeEnrollment{Host: protocol.ProbeHostInfo{ID: "server-machine", Name: "Machine", Address: "127.0.0.1"}, HostSecret: strings.Repeat("h", 64), RuntimeID: "probe-a", RuntimeSecret: strings.Repeat("p", 32), ServiceType: "rtc", ClientType: "native", Driver: "agora"}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: setup(t), Probes: h, AdminID: "admin", AdminSecret: strings.Repeat("x", 32)}
	for _, tc := range []struct {
		path, id, secret string
		want             int
	}{
		{"/api/v1/rtc/status", "admin", strings.Repeat("x", 32), 200},
		{"/api/v1/rtc/status", "probe-a", strings.Repeat("p", 32), 401},
		{"/api/v1/probes/connect", "probe-a", strings.Repeat("p", 32), 400},
		{"/api/v1/probes/connect", "admin", strings.Repeat("x", 32), 401},
		{"/api/v1/probes/connect", "rtc-a", strings.Repeat("a", 32), 401},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.SetBasicAuth(tc.id, tc.secret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s as %s: %d", tc.path, tc.id, w.Code)
		}
	}
	if routePermission("/api/v1/rtc/tasks/audio/run", "POST") != "rtc.manage" || routePermission("/api/v1/rtc/logs/id", "GET") != "rtc.diagnostics" || routePermission("/api/v1/rtc/status", "GET") != "monitor.read" {
		t.Fatal("incorrect RTC permission boundary")
	}
}
