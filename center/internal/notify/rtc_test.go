package notify

import (
	"context"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/shared/model"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRTCSeparateCategoriesSignatureRetryAndBaseline(t *testing.T) {
	var received []model.Notification
	var bodies []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		v1, v2 := signatures("rtc-secret", b)
		if r.Header.Get("Agora-Signature") != v1 || r.Header.Get("Agora-Signature-V2") != v2 {
			t.Error("RTC notification signatures missing")
		}
		var e model.Notification
		if json.Unmarshal(b, &e) != nil {
			t.Error("bad event")
		}
		received = append(received, e)
		bodies = append(bodies, string(b))
		if len(received) == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	c := Config{Enabled: true, EndpointMode: true}
	c.Endpoints[0] = Endpoint{ID: "rtc", Enabled: true, URL: server.URL, Secret: "rtc-secret", Events: [7]bool{false, false, false, false, true, true, true}}
	n, err := New(t.TempDir(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, server)
	now := time.Now().UTC()
	f := Fact{Key: "rtc/task", Business: true, TaskID: "task", TargetID: "whole-service", ConfigRevision: "v1", RunID: "run", Path: "A → RTC → B", Status: "HEALTHY", Notify: true, CollectedAt: &now}
	n.ObserveRTC([]Fact{f})
	n.SendOne(context.Background())
	if len(received) != 0 {
		t.Fatal("first normal baseline notified")
	}
	f.Status = "UNHEALTHY"
	f.ReasonCode = "FAILURE_CONFIRMED"
	n.ObserveRTC([]Fact{f})
	n.SendOne(context.Background())
	if len(received) != 2 || bodies[0] != bodies[1] {
		t.Fatal("RTC retry changed identity or payload")
	}
	if received[0].Category != RTCAbnormal || received[0].Type != "RTC_BUSINESS_STATUS_CHANGED" || received[0].Node != "" || received[0].Layer != "BLACKBOX" || received[0].TargetID != "whole-service" {
		t.Fatal("RTC event used container identity")
	}
	f.Status = "UNKNOWN"
	f.Notify = false
	n.ObserveRTC([]Fact{f})
	n.SendOne(context.Background())
	if len(received) != 2 {
		t.Fatal("unknown grace ignored")
	}
	f.Notify = true
	n.ObserveRTC([]Fact{f})
	n.SendOne(context.Background())
	if received[len(received)-1].Category != RTCUnknown {
		t.Fatal("sustained unknown event missing")
	}
	f.Status = "HEALTHY"
	n.ObserveRTC([]Fact{f})
	n.SendOne(context.Background())
	if received[len(received)-1].Category != RTCRecovered {
		t.Fatal("RTC recovery event missing")
	}
	n.Observe("baseline", []Fact{fact("HEALTHY")})
	if _, ok := n.facts[f.Key]; !ok {
		t.Fatal("container observation removed RTC facts")
	}
	before := len(received)
	n.ObserveRTC(nil)
	n.SendOne(context.Background())
	if len(received) != before {
		t.Fatal("task removal generated recovery")
	}
}

func TestRTCInflightCancellationIsNotOverwrittenByLateHTTPResult(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(503) }))
	defer server.Close()
	c := Config{Enabled: true, EndpointMode: true}
	c.Endpoints[0] = Endpoint{ID: "rtc", Enabled: true, URL: server.URL, Secret: "fixture-secret", Events: [7]bool{false, false, false, false, true, true, true}}
	n, err := New(t.TempDir(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, server)
	now := time.Now().UTC()
	f := Fact{Key: "rtc/task", Business: true, TaskID: "task", TargetID: "service", ConfigRevision: "v1", Status: "UNHEALTHY", Notify: true, CollectedAt: &now}
	n.ObserveRTC([]Fact{f})
	done := make(chan struct{})
	go func() { defer close(done); n.SendOne(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("HTTP send did not start")
	}
	n.ObserveRTC(nil)
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled send did not finish")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, d := range n.deliveries {
		if d.Status != "CANCELLED" || d.Attempts != 1 {
			t.Fatal("late response resurrected cancelled notification", d.Status, d.Attempts)
		}
	}
}
