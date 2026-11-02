package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func TestNormalInitialRecoveryRestartAndSubscriptionChanges(t *testing.T) {
	var mu sync.Mutex
	received := []model.Notification{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e model.Notification
		json.NewDecoder(r.Body).Decode(&e)
		if e.Type != "WEBHOOK_TEST" {
			mu.Lock()
			received = append(received, e)
			mu.Unlock()
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	n, err := New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	trustServer(n, server)
	n.Observe("1", []Fact{fact("HEALTHY")})
	requests := []EndpointRequest{{Enabled: true, URL: server.URL, Events: []string{Normal}, AuthMode: "set", Secret: "independent-normal"}}
	settings, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observe := func(status string) { n.Observe("1", []Fact{fact(status)}); n.SendOne(context.Background()) }
	observe("HEALTHY")
	observe("HEALTHY")
	mu.Lock()
	if len(received) != 1 || received[0].Type != "STATUS_SNAPSHOT" || received[0].Category != Normal || received[0].CurrentStatus != "HEALTHY" {
		t.Fatal("initial normal snapshot missing or repeated")
	}
	mu.Unlock()
	n.Close()
	n, err = New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { n.Close() }()
	trustServer(n, server)
	observe("HEALTHY")
	observe("UNHEALTHY")
	observe("HEALTHY")
	mu.Lock()
	if len(received) != 2 || received[1].Category != Recovered {
		t.Fatal("restart repeated initialization or normal-only subscription missed recovery")
	}
	mu.Unlock()
	e := settings.Endpoints[0]
	requests = []EndpointRequest{{ID: e.ID, Enabled: true, URL: e.URL, Events: []string{Normal, Recovered}, AuthMode: "retain"}}
	settings, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observe("HEALTHY")
	observe("UNHEALTHY")
	observe("HEALTHY")
	mu.Lock()
	if len(received) != 3 || received[2].Category != Recovered {
		t.Fatal("normal and recovery subscriptions duplicated an event")
	}
	mu.Unlock()
	requests = append(requests, EndpointRequest{Enabled: false, URL: "https://other.example.com", Events: []string{ContainerAbnormal}, AuthMode: "set", Secret: "other-independent"})
	settings, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: settings.Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observe("HEALTHY")
	mu.Lock()
	if len(received) != 3 {
		t.Fatal("unrelated configuration repeated initial snapshot")
	}
	mu.Unlock()
	requests[1].ID = settings.Endpoints[1].ID
	requests[1].AuthMode = "retain"
	requests[1].Secret = ""
	requests[0].Enabled = false
	settings, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: settings.Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observe("HEALTHY")
	requests[0].Enabled = true
	_, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: settings.Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observe("HEALTHY")
	observe("HEALTHY")
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 || received[3].Type != "STATUS_SNAPSHOT" {
		t.Fatal("re-enabling normal did not reinitialize once")
	}
}

func TestNormalWaitsForFreshFactsAndQueueCapacity(t *testing.T) {
	n, err := New(t.TempDir(), Config{MaxPending: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	// Configure without a network request; the state used by Observe is the
	// same persisted endpoint configuration produced by UpdateSettings.
	n.config.EndpointMode = true
	n.config.Enabled = true
	n.config.Endpoints[0] = Endpoint{ID: "a", Enabled: true, URL: "https://example.com", Secret: "test", Events: [4]bool{false, false, false, true}, NormalEpoch: "activation-1"}
	old := fact("HEALTHY")
	at := time.Now().Add(-time.Minute)
	old.CollectedAt = &at
	n.Observe("1", []Fact{old})
	if len(n.deliveries) != 0 {
		t.Fatal("stale normal facts initialized subscription")
	}
	first := fact("HEALTHY")
	second := first
	second.Key = "node/b"
	second.Node = "b"
	n.Observe("1", []Fact{first, second})
	if len(n.deliveries) != 1 {
		t.Fatal("queue budget not respected")
	}
	// Persist completion to free one pending slot, then initialize the other
	// node without losing it or duplicating the already initialized node.
	for _, d := range n.deliveries {
		d.Status = "DELIVERED"
		if !n.persist(record{Deliveries: []Delivery{d}}) {
			t.Fatal("fixture completion failed")
		}
	}
	n.Observe("1", []Fact{first, second})
	if len(n.deliveries) != 2 || n.pending() != 1 {
		t.Fatal("normal snapshot lost while queue was full")
	}
}
