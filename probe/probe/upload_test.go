package probe

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/center/rtchub"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
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
	cfg := hub.Config{Targets: []protocol.Target{{ID: "target", Name: "protocol fixture", Driver: "mock"}}, Tasks: []protocol.Task{protocol.Task{ID: "task", TargetID: "target", HostID: "host-a", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 30, ServiceType: "rtc", ClientType: "native"}}}
	h, err := newUploadHub(t, dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		session := h.Session(id)
		m := protocol.Msg("hello")
		m.Driver = "mock"
		m.HostID = "host-" + id
		m.ServiceType = "rtc"
		m.ClientType = "native"
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
	c := &Client{config: Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probes/connect"}, identity: RuntimeIdentity{ID: "a", Secret: secret}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = c.uploadBundle(ctx, server.Client(), request, archive.Bytes()); err == nil {
		t.Fatal("interrupted transfer unexpectedly finished")
	}
	sum := sha256.Sum256(archive.Bytes())
	bundleDigest := hex.EncodeToString(sum[:])
	sendPart := func(user, secret, index string, body []byte, digest string) int {
		t.Helper()
		r, _ := http.NewRequest("PUT", server.URL+"/api/v1/probes/uploads/"+request.ID+"/parts/"+index, bytes.NewReader(body))
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
	h, err = newUploadHub(t, dir, cfg)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.uploadBundle(ctx, server.Client(), request, archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := counts["PUT /api/v1/probes/uploads/"+request.ID+"/parts/0"]
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
	response, err := http.Get(server.URL + "/api/v1/probes/uploads/" + request.ID + "/parts")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated chunks accepted")
	}
}
func TestChunkClientRejectsUnsupportedUploadProtocol(t *testing.T) {
	for _, status := range []int{404, 405} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/parts") {
					t.Error("unexpected compatibility upload")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			c := &Client{config: Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probes/connect"}, identity: RuntimeIdentity{ID: "runtime-a", Secret: "fixture-secret"}}
			if err := c.uploadBundle(context.Background(), server.Client(), protocol.LogRequest{ID: "request"}, []byte("fixture")); err == nil {
				t.Fatal("unsupported upload protocol accepted")
			}
			if calls != 1 {
				t.Fatal("client attempted an old upload endpoint")
			}
		})
	}
}

func newUploadHub(t *testing.T, dir string, cfg hub.Config) (*hub.Hub, error) {
	t.Helper()
	h, err := hub.New(dir, hub.Config{}, nil)
	if err != nil {
		return nil, err
	}
	for _, id := range []string{"a", "b"} {
		secret := strings.Repeat("p", 32)
		if id == "b" {
			secret = strings.Repeat("q", 32)
		}
		if err = h.EnrollProbe(protocol.ProbeEnrollment{Host: protocol.ProbeHostInfo{ID: "host-" + id, Name: "fixture-" + id, Address: "127.0.0.1"}, HostSecret: strings.Repeat("server-"+id, 4), RuntimeID: id, RuntimeSecret: secret, ServiceType: "rtc", ClientType: "native"}, "127.0.0.1"); err != nil {
			return nil, err
		}
	}
	if len(cfg.Tasks) > 0 {
		var value struct {
			Revision string `json:"config_revision"`
		}
		b, _ := json.Marshal(h.Snapshot())
		json.Unmarshal(b, &value)
		if err = h.UpdatePlan(value.Revision, hub.Plan{Targets: cfg.Targets, Tasks: cfg.Tasks}); err != nil {
			return nil, err
		}
	}
	return h, nil
}
