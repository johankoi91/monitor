package probe

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChunksResumeAfterFailureAndCenterRestart(t *testing.T) {
	dir := t.TempDir()
	secret := strings.Repeat("p", 32)
	cfg := hub.Config{Probes: []hub.ProbeConfig{{ID: "a", Secret: secret, Location: "A"}, {ID: "b", Secret: strings.Repeat("q", 32), Location: "B"}}, Targets: []protocol.Target{{ID: "target", Name: "protocol fixture", Driver: "mock"}}, Tasks: []protocol.Task{{ID: "task", TargetID: "target", ProbeID: "a", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 30}}}
	h, err := hub.New(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		session := h.Session(id)
		m := protocol.Msg("hello")
		m.Driver = "mock"
		m.Capacity = 1
		if _, err = h.Receive(id, session, m); err != nil {
			t.Fatal(err)
		}
	}
	run, err := h.Start("task")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Collect(run.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := h.Detail(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	var request protocol.LogRequest
	for _, v := range run.Logs {
		if v.Role == "sender" {
			request = v
		}
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	f, _ := writer.Create("manifest.json")
	json.NewEncoder(f).Encode(map[string]any{"request_id": request.ID, "job": run.Endpoints["sender"].Job})
	f, _ = writer.Create("process.stdout.log")
	data := make([]byte, 700<<10)
	if _, err = rand.Read(data); err != nil {
		t.Fatal(err)
	}
	f.Write(data)
	writer.Close()
	var mu sync.Mutex
	counts := map[string]int{}
	reject := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		counts[r.Method+" "+r.URL.Path]++
		if reject && r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/parts/1") {
			reject = false
			w.WriteHeader(503)
			return
		}
		h.ServeProbe(w, r)
	}))
	defer server.Close()
	c := &Client{config: Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probe/connect", ID: "a", Secret: secret}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = c.uploadBundle(ctx, server.Client(), request, archive.Bytes()); err == nil {
		t.Fatal("interrupted transfer unexpectedly finished")
	}
	sum := sha256.Sum256(archive.Bytes())
	bundleDigest := hex.EncodeToString(sum[:])
	sendPart := func(user, secret, index string, body []byte, digest string) int {
		t.Helper()
		r, _ := http.NewRequest("PUT", server.URL+"/api/v1/probe/uploads/"+request.ID+"/parts/"+index, bytes.NewReader(body))
		r.SetBasicAuth(user, secret)
		r.Header.Set("X-Bundle-SHA256", bundleDigest)
		r.Header.Set("X-Bundle-Bytes", strconv.Itoa(archive.Len()))
		r.Header.Set("X-Content-SHA256", digest)
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response.StatusCode
	}
	last := archive.Bytes()[2*protocol.LogChunkBytes:]
	lastHash := sha256.Sum256(last)
	lastDigest := hex.EncodeToString(lastHash[:])
	if code := sendPart("a", secret, "2", last, strings.Repeat("0", 64)); code != 400 {
		t.Fatal("corrupt chunk accepted", code)
	}
	if code := sendPart("b", strings.Repeat("q", 32), "2", last, lastDigest); code != 404 {
		t.Fatal("other probe wrote a chunk", code)
	}
	if code := sendPart("a", secret, "32", last, lastDigest); code != 400 {
		t.Fatal("out-of-range chunk accepted", code)
	}
	if code := sendPart("a", secret, "2", last, lastDigest); code != 201 {
		t.Fatal("out-of-order chunk rejected", code)
	}
	if code := sendPart("a", secret, "2", last, lastDigest); code != 200 {
		t.Fatal("duplicate chunk not idempotent", code)
	}
	other := append([]byte(nil), last...)
	other[0] ^= 0xff
	otherHash := sha256.Sum256(other)
	if code := sendPart("a", secret, "2", other, hex.EncodeToString(otherHash[:])); code != 409 {
		t.Fatal("conflicting chunk accepted", code)
	}
	mu.Lock()
	h, err = hub.New(dir, cfg, nil)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.uploadBundle(ctx, server.Client(), request, archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := counts["PUT /api/v1/probe/uploads/"+request.ID+"/parts/0"]
	mu.Unlock()
	if first != 1 {
		t.Fatal("confirmed first chunk retransmitted", first)
	}
	if err = c.uploadBundle(ctx, server.Client(), request, archive.Bytes()); err != nil {
		t.Fatal("completed transfer not idempotent", err)
	}
	raw, err = h.Detail(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &run)
	if run.Logs[request.ID].Status != "AVAILABLE" {
		t.Fatal("assembled bundle not published")
	}
	response, err := http.Get(server.URL + "/api/v1/probe/uploads/" + request.ID + "/parts")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated chunks accepted")
	}
}
func TestChunkClientFallsBackToExistingCenter(t *testing.T) {
	payload := []byte("immutable diagnostic zip fixture")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/parts") {
			w.WriteHeader(404)
			return
		}
		if r.Method != "PUT" {
			t.Error("unexpected method")
		}
		b, _ := io.ReadAll(r.Body)
		called = bytes.Equal(b, payload)
		w.WriteHeader(201)
	}))
	defer server.Close()
	c := &Client{config: Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probe/connect", ID: "a", Secret: "fixture-secret"}}
	if err := c.uploadBundle(context.Background(), server.Client(), protocol.LogRequest{ID: "request"}, payload); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("legacy upload lost")
	}
}
