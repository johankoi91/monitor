package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSettingsPersistWithoutForwardingOldAuthOrQueue(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("old receiving credential forwarded")
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	base := Config{Enabled: true, URL: "https://old.example.com/notify", ID: "receiver", Secret: "private-old-secret"}
	n, err := New(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if server != nil {
		trustServer(n, server)
	}
	event(n)
	settings := n.Settings()
	if _, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: settings.Revision, Enabled: true, URL: server.URL, AuthMode: "retain"}, nil); err == nil {
		t.Fatal("changed target retained old credential")
	}
	settings, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: settings.Revision, Enabled: false, URL: server.URL, AuthMode: "set", Secret: "new-signing-secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AuthConfigured {
		t.Fatal("old auth preserved")
	}
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "private-old-secret") {
		t.Fatal("secret returned in settings")
	}
	n.SendOne(context.Background())
	if calls.Load() != 0 {
		t.Fatal("old queued event redirected to new target")
	}
	n.mu.Lock()
	n.config.Enabled = true
	n.mu.Unlock()
	n.Observe("baseline-1", []Fact{fact("HEALTHY")})
	n.SendOne(context.Background())
	if calls.Load() != 1 {
		t.Fatal("new event not sent")
	}
	n.Close()
	n, err = New(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if n.Settings().URL != server.URL || !n.Settings().AuthConfigured || n.Settings().Revision != settings.Revision {
		t.Fatal("settings not recovered")
	}
	info, err := os.Stat(filepath.Join(dir, "notification-settings.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions")
	}
}
func TestSettingsConflictNoopAndWriteFailure(t *testing.T) {
	n, err := New(t.TempDir(), Config{Enabled: true, URL: "https://127.0.0.1:1", ID: "receiver", Secret: "old-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	event(n)
	original := n.Settings()
	request := SettingsRequest{ExpectedRevision: original.Revision, Enabled: true, URL: original.URL, AuthMode: "retain"}
	same, err := n.UpdateSettings(request, nil)
	if err != nil || same.Revision != original.Revision || n.pending() != 1 {
		t.Fatal("no-op discarded queue")
	}
	request.ExpectedRevision = "stale"
	if _, err = n.UpdateSettings(request, nil); !errors.Is(err, ErrSettingsConflict) {
		t.Fatal("stale update accepted")
	}
	request.ExpectedRevision = original.Revision
	request.URL = "https://127.0.0.1:2"
	request.AuthMode = "clear"
	request.Enabled = false
	n.writeSettings = func(settingsRecord) error { return errors.New("disk full") }
	if _, err = n.UpdateSettings(request, nil); !errors.Is(err, ErrSettingsWrite) {
		t.Fatal("write failure hidden")
	}
	if n.Settings().Revision != original.Revision || n.Settings().URL != original.URL || n.pending() != 1 {
		t.Fatal("failed update altered config or queue")
	}
}
func TestSettingsValidationAndIndependentCredentials(t *testing.T) {
	n, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	for _, url := range []string{"http://example.com/x", "ftp://example.com/x", "https://user:secret@example.com/x", "https://example.com/#x", "not-a-url"} {
		_, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Enabled: true, URL: url, AuthMode: "clear"}, nil)
		if err == nil {
			t.Fatal("invalid URL accepted")
		}
	}
	_, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Enabled: true, URL: "https://example.com/x", AuthMode: "set", ID: "receiver", Secret: "management-secret"}, []string{"management-secret"})
	if err == nil {
		t.Fatal("management secret reused for outbound auth")
	}
}
func TestReconfigurationCancelsInflightWithoutResurrectingQueue(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	n, err := New(t.TempDir(), Config{Enabled: true, URL: server.URL, Secret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if server != nil {
		trustServer(n, server)
	}
	event(n)
	done := make(chan struct{})
	go func() { defer close(done); n.SendOne(context.Background()) }()
	<-started
	_, err = n.UpdateSettings(SettingsRequest{ExpectedRevision: n.Settings().Revision, Enabled: false, URL: "https://127.0.0.1:2", AuthMode: "clear"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("inflight not cancelled")
	}
	if n.pending() != 0 {
		t.Fatal("old response resurrected old queue")
	}
}
