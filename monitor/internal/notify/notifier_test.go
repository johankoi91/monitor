package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fact(status string) Fact {
	at := time.Now().UTC()
	return Fact{Key: "node/a/ap", Node: "a/ap", Cluster: "rtc", Service: "rtc-ap", Status: status, ReasonCode: "TEST_STATE", Reason: "isolated notification test", CollectedAt: &at}
}
func event(n *Notifier) {
	n.Observe("baseline-1", []Fact{fact("HEALTHY")})
	n.Observe("baseline-1", []Fact{fact("UNHEALTHY")})
}
func ready(n *Notifier) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, d := range n.deliveries {
		d.NextAt = time.Now().Add(-time.Second)
		n.deliveries[id] = d
	}
}
func TestRetryKeepsIDAndPayloadAcrossRestart(t *testing.T) {
	var mu sync.Mutex
	payloads := [][]byte{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		v1, v2 := signatures("independent-secret", body)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Agora-Signature") != v1 || r.Header.Get("Agora-Signature-V2") != v2 {
			t.Error("incorrect notification signature")
		}
		mu.Lock()
		payloads = append(payloads, body)
		count := len(payloads)
		mu.Unlock()
		if count == 1 {
			w.WriteHeader(503)
		} else {
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	config := Config{Enabled: true, URL: server.URL, Secret: "independent-secret"}
	n, err := New(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	trustServer(n, server)
	event(n)
	n.SendOne(context.Background())
	n.Close()
	n, err = New(dir, config)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, server)
	ready(n)
	n.SendOne(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 2 || string(payloads[0]) != string(payloads[1]) {
		t.Fatal("retries changed event identity/payload")
	}
	var data map[string]any
	json.Unmarshal(payloads[0], &data)
	if _, exists := data["startup"]; exists || strings.Contains(string(payloads[0]), "independent-secret") {
		t.Fatal("notification leaked configuration")
	}
	if n.Summary().(map[string]any)["delivered"] != 1 {
		t.Fatal("delivery not retained")
	}
}
func TestRedirectNeverForwardsCredentials(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	n, err := New(t.TempDir(), Config{Enabled: true, URL: redirect.URL, ID: "receiver", Secret: "independent-secret", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, redirect)
	event(n)
	for i := 0; i < 4; i++ {
		ready(n)
		n.SendOne(context.Background())
	}
	if targetCalls != 0 || n.Summary().(map[string]any)["failed"] != 1 {
		t.Fatal("redirect followed or false acknowledgement")
	}
}
func TestHTTPSRejectsWrongCertificateIdentity(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("wrong certificate identity received event") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	n, err := New(t.TempDir(), Config{Enabled: true, URL: server.URL, Secret: "test-secret", TLSServerName: "wrong.invalid", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	n.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	trustServer(n, server)
	event(n)
	for i := 0; i < 4; i++ {
		ready(n)
		n.SendOne(context.Background())
	}
	if n.Summary().(map[string]any)["failed"] != 1 {
		t.Fatal("invalid TLS identity accepted")
	}
}
func TestQueueBoundAndConfigurationRemovalDoesNotFakeRecovery(t *testing.T) {
	n, err := New(t.TempDir(), Config{Enabled: true, URL: "https://127.0.0.1:1", Secret: "test-secret", MaxPending: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	event(n)
	n.Observe("baseline-1", []Fact{fact("HEALTHY")})
	summary := n.Summary().(map[string]any)
	if summary["pending"] != 1 || summary["dropped"] != 1 {
		t.Fatal("unbounded queue")
	}
	before := len(n.deliveries)
	n.Observe("baseline-2", nil)
	if len(n.deliveries) != before {
		t.Fatal("removed node became recovery")
	}
	service := fact("DEGRADED")
	service.Node = ""
	service.Key = "service/rtc/rtc-ap"
	n.Observe("baseline-2", []Fact{service})
	service.Status = "HEALTHY"
	n.Observe("baseline-3", []Fact{service})
	if len(n.deliveries) != before {
		t.Fatal("configuration-based service recovery notified")
	}
}
