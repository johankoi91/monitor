package center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/rtc-probe/hub"
)

func TestRTCProbeMachineAndManagementCredentialsStaySeparate(t *testing.T) {
	h, err := hub.New(t.TempDir(), hub.Config{Probes: []hub.ProbeConfig{{ID: "probe-a", Secret: strings.Repeat("p", 32), Location: "A"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: setup(t), Probes: h, AdminID: "admin", AdminSecret: strings.Repeat("x", 32)}
	for _, tc := range []struct {
		path, id, secret string
		want             int
	}{
		{"/api/v1/rtc/status", "admin", strings.Repeat("x", 32), 200},
		{"/api/v1/rtc/status", "probe-a", strings.Repeat("p", 32), 401},
		{"/api/v1/probe/connect", "admin", strings.Repeat("x", 32), 401},
		{"/api/v1/probe/connect", "rtc-a", strings.Repeat("a", 32), 401},
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
