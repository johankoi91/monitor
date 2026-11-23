package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRawMessageRecoveryAndHistoryPagination(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	h := handler(s, "diagnostic", "password")
	var firstBody string
	for _, id := range []string{"first", "second", "third"} {
		body, _ := json.MarshalIndent(sampleEvent(id), "", "  ")
		body = append(body, '\n')
		if id == "first" {
			firstBody = string(body)
		}
		if w := request(h, body, "password"); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	s.file.Close()
	s, err = openStore(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.file.Close()
	r := httptest.NewRequest("GET", "/api/v1/notifications/recent?limit=2&offset=1", nil)
	r.SetBasicAuth("diagnostic", "password")
	w := httptest.NewRecorder()
	handler(s, "diagnostic", "password").ServeHTTP(w, r)
	var result struct {
		Events []record `json:"events"`
		Total  int      `json:"total"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("history query failed")
	}
	if result.Total != 3 || len(result.Events) != 2 || result.Events[0].Event.ID != "second" || result.Events[1].RawBody != firstBody {
		t.Fatal("history pagination or exact raw body lost on restart")
	}
}

func TestViewerSourceCannotPostCallbacksAndPageRequiresAuth(t *testing.T) {
	s, err := openStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.file.Close()
	h, err := receiverSourceGuard(handler(s, "diagnostic", "password"), "192.0.2.1", "192.0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		auth         bool
		status       int
	}{
		{"GET", "/", false, 401}, {"GET", "/", true, 200},
		{"GET", "/api/v1/notifications/recent", true, 200},
		{"POST", "/api/v1/notifications", true, 403},
		{"GET", "/health/ready", true, 403},
	} {
		r := httptest.NewRequest(test.method, test.path, nil)
		r.RemoteAddr = "192.0.2.2:12345"
		if test.auth {
			r.SetBasicAuth("diagnostic", "password")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d, want %d", test.method, test.path, w.Code, test.status)
		}
		if w.Code == http.StatusOK && test.path == "/" && w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("page protections missing")
		}
	}
}
