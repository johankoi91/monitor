package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func trustServer(n *Notifier, server *httptest.Server) {
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	n.client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
	n.newClient = func(c Config) *http.Client {
		client := webhookClient(c)
		client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
		return client
	}
}

func TestOfficialSignatureVectors(t *testing.T) {
	body := []byte(`{"eventType":10,"noticeId":"4eb720f0-8da7-11e9-a43e-53f411c2761f","notifyMs":1560408533119,"payload":{"a":"1","b":2},"productId":1}`)
	v1, v2 := signatures("secret", body)
	if v1 != "5a3bb6a6d9fad2ea9ae3fb707a14c9d7f3136df1" || v2 != "de96da5acf03b0021ac3b4fa2225e7ae6f3533a30d50bb02c08ea4fa748bda24" {
		t.Fatal("signature differs from official reference")
	}
}

func TestWebhook200JSONAndThreeRetries(t *testing.T) {
	for _, test := range []struct {
		status  int
		body    string
		success bool
	}{{200, `{"ok":true}`, true}, {202, `{}`, false}, {204, ``, false}, {200, `ok`, false}} {
		calls := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			body, _ := io.ReadAll(r.Body)
			v1, v2 := signatures("shared", body)
			if r.Header.Get("Authorization") != "" || r.Header.Get("Agora-Signature") != v1 || r.Header.Get("Agora-Signature-V2") != v2 {
				t.Error("wrong signature headers")
			}
			w.WriteHeader(test.status)
			w.Write([]byte(test.body))
		}))
		n, err := New(t.TempDir(), Config{Enabled: true, URL: server.URL, Secret: "shared"})
		if err != nil {
			t.Fatal(err)
		}
		trustServer(n, server)
		event(n)
		ready(n)
		n.SendOne(context.Background())
		if !test.success && calls != 2 {
			t.Fatal("first retry was not performed immediately")
		}
		for i := 0; i < 5; i++ {
			ready(n)
			n.SendOne(context.Background())
		}
		if test.success && calls != 1 || !test.success && calls != 4 {
			t.Fatalf("status %d: attempts %d", test.status, calls)
		}
		if !test.success && n.Summary().(map[string]any)["failed"] != 1 {
			t.Fatal("retry exhaustion not recorded")
		}
		n.Close()
		server.Close()
	}
}

func TestEnabledEndpointMustPassHealthCheck(t *testing.T) {
	good := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]any
		json.NewDecoder(r.Body).Decode(&event)
		if event["event_type"] != "WEBHOOK_TEST" {
			t.Error("health check disguised as status change")
		}
		if !good {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	n, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, server)
	request := SettingsRequest{ExpectedRevision: n.Settings().Revision, Enabled: true, URL: server.URL, AuthMode: "set", Secret: "shared"}
	if _, err = n.UpdateSettings(request, nil); err == nil || n.Settings().Enabled {
		t.Fatal("invalid endpoint enabled")
	}
	good = true
	if _, err = n.UpdateSettings(request, nil); err != nil {
		t.Fatal(err)
	}
}

func TestHealthCheckDoesNotBlockSettingsAndRejectsStaleUpdate(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	n, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	trustServer(n, server)
	revision := n.Settings().Revision
	done := make(chan error, 1)
	go func() {
		_, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: revision, Enabled: true, URL: server.URL, AuthMode: "set", Secret: "shared"}, nil)
		done <- err
	}()
	<-started
	defer close(release)
	changed := make(chan error, 1)
	go func() {
		_, err := n.UpdateSettings(SettingsRequest{ExpectedRevision: revision, URL: "https://example.com/new", AuthMode: "set", Secret: "other"}, nil)
		changed <- err
	}()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("health check blocks config updates")
	}
	// Release without closing twice on the deferred cleanup path.
	release <- struct{}{}
	if err := <-done; err != ErrSettingsConflict {
		t.Fatalf("stale health check result accepted: %v", err)
	}
	if n.Settings().URL != "https://example.com/new" {
		t.Fatal("new configuration overwritten")
	}
}
