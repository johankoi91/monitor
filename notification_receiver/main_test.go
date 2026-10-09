package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sampleEvent(id string) event {
	now := time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC)
	stale := false
	return event{ID: id, Type: "STATUS_CHANGED", OccurredAt: now, Product: "RTC", Cluster: "rtc-pilot",
		Service: "rtc-ap", Node: "rtc-pilot-01/agora_local_ap", PreviousStatus: "UNKNOWN", CurrentStatus: "HEALTHY",
		ReasonCode: "OK", Reason: "test event", CollectedAt: &now, Stale: &stale}
}

func request(h http.Handler, body []byte, secret string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		r.Header.Set("Agora-Signature-V2", hex.EncodeToString(mac.Sum(nil)))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthenticationAndPayloadValidation(t *testing.T) {
	s, err := openStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.file.Close()
	h := handler(s, "test-id", "test-secret")
	valid, _ := json.Marshal(sampleEvent("event-1"))
	for _, secret := range []string{"", "wrong"} {
		w := request(h, valid, secret)
		if w.Code != 401 {
			t.Fatalf("auth response: %d", w.Code)
		}
	}
	for _, body := range [][]byte{[]byte("not-json"), []byte(`{}`), append(append([]byte(nil), valid...), []byte(` {}`)...)} {
		if w := request(h, body, "test-secret"); w.Code != 400 {
			t.Fatalf("bad payload response: %d", w.Code)
		}
	}
	var fields map[string]any
	json.Unmarshal(valid, &fields)
	delete(fields, "collected_at")
	incomplete, _ := json.Marshal(fields)
	if w := request(h, incomplete, "test-secret"); w.Code != 400 {
		t.Fatalf("missing field response: %d", w.Code)
	}
	if w := request(h, []byte(strings.Repeat(" ", maxBodyBytes+1)), "test-secret"); w.Code != 413 {
		t.Fatalf("oversize response: %d", w.Code)
	}
	if _, count, _ := s.snapshot(0); count != 0 {
		t.Fatal("invalid requests persisted")
	}
	if w := request(h, valid, "test-secret"); w.Code != 200 {
		t.Fatalf("valid payload response: %d %s", w.Code, w.Body.String())
	}
}

func TestDurabilityReplayConflictAndConcurrentDelivery(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	e := sampleEvent("stable-event")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.accept(e); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, count, _ := s.snapshot(20); count != 1 {
		t.Fatalf("stored %d duplicates", count)
	}
	if _, err := openStore(dir, 1<<20); err == nil {
		t.Fatal("second writer acquired file")
	}
	if err := s.file.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := openStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.file.Close()
	duplicate, err := recovered.accept(e)
	if err != nil || !duplicate {
		t.Fatalf("retry after restart: duplicate=%v err=%v", duplicate, err)
	}
	e.CurrentStatus = "UNHEALTHY"
	if _, err := recovered.accept(e); err != errConflict {
		t.Fatalf("changed payload reused ID: %v", err)
	}
	if _, count, _ := recovered.snapshot(20); count != 1 {
		t.Fatal("conflict persisted")
	}
}

func TestCapacityAndCorruptTailFailClosed(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	h := handler(s, "test-id", "test-secret")
	body, _ := json.Marshal(sampleEvent("capacity"))
	if w := request(h, body, "test-secret"); w.Code != 507 {
		t.Fatalf("capacity response: %d", w.Code)
	}
	s.file.Close()
	if data, _ := os.ReadFile(filepath.Join(dir, "events.jsonl")); len(data) != 0 {
		t.Fatal("capacity overflow appended")
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(`{"received_at":`), 0600); err != nil {
		t.Fatal(err)
	}
	if broken, err := openStore(dir, 1<<20); err == nil {
		broken.file.Close()
		t.Fatal("corrupt tail accepted")
	}
}

func TestWriteFailureDoesNotAcknowledge(t *testing.T) {
	s, err := openStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	s.file.Close()
	h := handler(s, "test-id", "test-secret")
	body, _ := json.Marshal(sampleEvent("io-failure"))
	if w := request(h, body, "test-secret"); w.Code != 503 {
		t.Fatalf("write failure response: %d", w.Code)
	}
	if _, count, ready := s.snapshot(0); count != 0 || ready {
		t.Fatal("write failure acknowledged or ready")
	}
}

func TestSourceRestrictionDoesNotTrustForwardedHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h, err := sourceGuard(next, "111.230.108.76,127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		remote string
		status int
	}{
		{"111.230.108.76:50000", 200}, {"127.0.0.1:50000", 200}, {"[::1]:50000", 200}, {"192.0.2.1:50000", 403},
	} {
		r := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", "111.230.108.76")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.remote, w.Code)
		}
	}
	if _, err := sourceGuard(next, "invalid-ip"); err == nil {
		t.Fatal("invalid source list accepted")
	}
}
