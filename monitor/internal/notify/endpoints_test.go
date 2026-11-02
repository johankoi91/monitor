package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestEndpointMigrationRetainsSecretAndEvents(t *testing.T) {
	dir := t.TempDir()
	n, err := New(dir, Config{Enabled: true, URL: "https://example.com/notify", Secret: "existing-private-secret"})
	if err != nil {
		t.Fatal(err)
	}
	initial := n.Settings()
	if len(initial.Endpoints) != 1 || len(initial.Endpoints[0].Events) != 3 || !initial.Endpoints[0].AuthConfigured {
		t.Fatal("legacy configuration not exposed")
	}
	raw, _ := json.Marshal(initial)
	if strings.Contains(string(raw), "existing-private-secret") {
		t.Fatal("signing secret exposed")
	}
	e := initial.Endpoints[0]
	requests := []EndpointRequest{{ID: e.ID, Enabled: true, URL: e.URL, Events: []string{ContainerAbnormal, Recovered}, AuthMode: "retain"}}
	saved, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: initial.Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Endpoints) != 1 || len(saved.Endpoints[0].Events) != 2 || !n.config.EndpointMode {
		t.Fatal("migration lost selections")
	}
	n.Close()
	n, err = New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if endpoints(n.config)[0].Secret != "existing-private-secret" || n.Settings().Revision != saved.Revision {
		t.Fatal("endpoint migration not durably recovered")
	}
	requests[0].URL = "https://different.example.com"
	if _, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: saved.Revision, Endpoints: &requests}, nil); err == nil {
		t.Fatal("new destination retained another receiver's secret")
	}
	requests = []EndpointRequest{}
	empty, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: saved.Revision, Endpoints: &requests}, nil)
	if err != nil || len(empty.Endpoints) != 0 || empty.Enabled {
		t.Fatal("last endpoint deletion failed", err)
	}
}

func TestEndpointFanoutIndependentRetriesSecretsAndRecovery(t *testing.T) {
	var mu sync.Mutex
	fail := false
	ids := map[string][]string{}
	payloads := map[string][]string{}
	receiver := func(name, secret string) *httptest.Server {
		return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			v1, v2 := signatures(secret, b)
			if r.Header.Get("Agora-Signature") != v1 || r.Header.Get("Agora-Signature-V2") != v2 {
				t.Error("wrong per-receiver secret")
			}
			var e struct {
				ID   string `json:"event_id"`
				Type string `json:"event_type"`
			}
			json.Unmarshal(b, &e)
			mu.Lock()
			defer mu.Unlock()
			if e.Type != "WEBHOOK_TEST" {
				ids[name] = append(ids[name], e.ID)
				payloads[name] = append(payloads[name], string(b))
				if fail && name == "a" {
					w.WriteHeader(503)
					return
				}
			}
			w.Write([]byte(`{}`))
		}))
	}
	a := receiver("a", "secret-a")
	defer a.Close()
	b := receiver("b", "secret-b")
	defer b.Close()
	dir := t.TempDir()
	n, err := New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	trust := func(n *Notifier) {
		roots := x509.NewCertPool()
		roots.AddCert(a.Certificate())
		roots.AddCert(b.Certificate())
		n.newClient = func(c Config) *http.Client {
			client := webhookClient(c)
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
			return client
		}
		n.client = n.newClient(n.config)
	}
	trust(n)
	requests := []EndpointRequest{{Enabled: true, URL: a.URL, Events: []string{ContainerAbnormal}, AuthMode: "set", Secret: "secret-a"}, {Enabled: true, URL: b.URL, Events: []string{ContainerAbnormal, Recovered}, AuthMode: "set", Secret: "secret-b"}}
	settings, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Endpoints: &requests}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "secret-a") || strings.Contains(string(raw), "secret-b") {
		t.Fatal("endpoint secret returned")
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	event(n)
	n.SendOne(context.Background())
	n.SendOne(context.Background())
	if len(n.deliveries) != 2 {
		t.Fatal("fanout event overwritten")
	}
	delivered, pending := 0, 0
	for _, d := range n.deliveries {
		if d.Status == "DELIVERED" {
			delivered++
		}
		if d.Status == "PENDING" {
			pending++
		}
	}
	if delivered != 1 || pending != 1 {
		t.Fatal("failure affected another endpoint")
	}
	n.Close()
	n, err = New(dir, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trust(n)
	if len(n.deliveries) != 2 {
		t.Fatal("fanout deliveries lost on recovery")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	ready(n)
	n.SendOne(context.Background())
	for _, d := range n.deliveries {
		if d.Status != "DELIVERED" {
			t.Fatal("retry did not complete")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids["a"]) != 3 || len(ids["b"]) != 1 || ids["a"][0] != ids["b"][0] || payloads["a"][0] != payloads["a"][2] {
		t.Fatalf("duplicate, retry or event identity mismatch: %v", ids)
	}
}
