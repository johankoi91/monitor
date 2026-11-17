package hub_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/testkit"
)

// The test binary becomes the same isolated JSON-pipe worker used by the CLI.
func TestMockWorkerProcess(t *testing.T) {
	if os.Getenv("RTC_TEST_WORKER") != "1" {
		return
	}
	pipe := os.NewFile(3, "worker-evidence")
	err := testkit.Run(context.Background(), os.Stdin, pipe)
	pipe.Close()
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func config(scenario string) hub.Config {
	return hub.Config{Probes: []hub.ProbeConfig{{ID: "probe-a", Secret: strings.Repeat("a", 32), Location: "A"}, {ID: "probe-b", Secret: strings.Repeat("b", 32), Location: "B"}}, Targets: []protocol.Target{{ID: "rtc-whole-service", Name: "RTC overall service", Driver: "mock"}}, Tasks: []protocol.Task{{ID: "audio", TargetID: "rtc-whole-service", ProbeID: "probe-a", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 4, Scenario: scenario}}}
}
func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition did not become true before deadline")
}

type fixture struct {
	hub    *hub.Hub
	server *httptest.Server
	dirs   []string
}

func setup(t *testing.T, scenario string) fixture {
	t.Helper()
	h, e := hub.New(t.TempDir(), config(scenario), nil)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.Run(ctx) }()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/probe/") {
			h.ServeProbe(w, r)
		} else {
			h.ServeAdmin(w, r)
		}
	}))
	var dirs []string
	clientDone := []chan error{}
	for _, identity := range config(scenario).Probes[:1] {
		dir := t.TempDir()
		dirs = append(dirs, dir)
		c, e := probe.New(probe.Config{URL: "ws" + strings.TrimPrefix(s.URL, "http") + "/api/v1/probe/connect", ID: identity.ID, Secret: identity.Secret, DataDir: dir, Capacity: 1, AllowPlaintextLoopback: true, WorkerCommand: func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMockWorkerProcess$")
			cmd.Env = append(os.Environ(), "RTC_TEST_WORKER=1")
			return cmd
		}})
		if e != nil {
			t.Fatal(e)
		}
		finished := make(chan error, 1)
		clientDone = append(clientDone, finished)
		go func() { finished <- c.Run(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		for _, d := range clientDone {
			select {
			case e := <-d:
				if e != nil {
					t.Error(e)
				}
			case <-time.After(8 * time.Second):
				t.Error("probe did not stop")
			}
		}
		<-done
		s.Close()
	})
	return fixture{h, s, dirs}
}
func start(t *testing.T, h *hub.Hub) hub.Run {
	t.Helper()
	var r hub.Run
	eventually(t, 8*time.Second, func() bool { var e error; r, e = h.Start("audio"); return e == nil })
	return r
}
func detail(t *testing.T, h *hub.Hub, id string) hub.Run {
	t.Helper()
	b, e := h.Detail(id)
	if e != nil {
		t.Fatal(e)
	}
	var r hub.Run
	if e = json.Unmarshal(b, &r); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRealWorkersEvidenceAcknowledgementAndLogRetrieval(t *testing.T) {
	f := setup(t, "success")
	r := start(t, f.hub)
	if _, e := f.hub.Start("audio"); e == nil {
		t.Fatal("overlapping task accepted")
	}
	eventually(t, 10*time.Second, func() bool { return detail(t, f.hub, r.ID).Status == "SUCCESS" })
	r = detail(t, f.hub, r.ID)
	if r.TargetID != "rtc-whole-service" || r.Driver != "mock" || r.Endpoints["receiver"].Result.Evidence.ValidSamples < 16000 || r.Endpoints["sender"].Result.Evidence.SentBytes == 0 {
		t.Fatalf("invalid evidence: %+v", r)
	}
	seen := map[string]bool{}
	for _, ep := range r.Endpoints {
		for _, e := range ep.Events {
			seen[e.Kind] = true
		}
	}
	for _, kind := range []string{"api_call_begin", "api_call_end", "sdk_callback", "media_summary"} {
		if !seen[kind] {
			t.Fatalf("missing %s", kind)
		}
	}
	state, _ := json.Marshal(f.hub.Snapshot())
	if !bytes.Contains(state, []byte(`"rtc_service_status":"UNKNOWN"`)) {
		t.Fatal("mock evidence established real health")
	}
	for _, dir := range f.dirs {
		eventually(t, 5*time.Second, func() bool {
			b, e := os.ReadFile(filepath.Join(dir, "outbox.json"))
			if e != nil {
				return false
			}
			var rows map[string]struct {
				Ack    bool             `json:"result_acked"`
				Events []protocol.Event `json:"pending_events"`
			}
			if json.Unmarshal(b, &rows) != nil {
				return false
			}
			for key, v := range rows {
				if strings.HasPrefix(key, r.ID+"--") {
					return v.Ack && len(v.Events) == 0
				}
			}
			return false
		})
	}
	if e := f.hub.Collect(r.ID); e != nil {
		t.Fatal(e)
	}
	eventually(t, 8*time.Second, func() bool {
		r = detail(t, f.hub, r.ID)
		if len(r.Logs) != 1 {
			return false
		}
		for _, v := range r.Logs {
			if v.Status != "AVAILABLE" {
				return false
			}
		}
		return true
	})
	for id, v := range r.Logs {
		resp, e := http.Get(f.server.URL + "/api/v1/rtc/logs/" + id)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 || int64(len(b)) != v.Bytes {
			t.Fatal("diagnostic download failed")
		}
		z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, file := range z.File {
			if file.Name == "manifest.json" {
				reader, _ := file.Open()
				manifest, _ := io.ReadAll(reader)
				reader.Close()
				found = bytes.Contains(manifest, []byte(r.ID)) && bytes.Contains(manifest, []byte(`"simulation_only":true`))
			}
		}
		if !found {
			t.Fatal("diagnostic attribution missing")
		}
	}
}
func TestNoMediaAndCancellationDoNotSucceed(t *testing.T) {
	t.Run("no_media", func(t *testing.T) {
		f := setup(t, "no_media")
		r := start(t, f.hub)
		eventually(t, 10*time.Second, func() bool { return detail(t, f.hub, r.ID).Status == "FAILED" })
		r = detail(t, f.hub, r.ID)
		if r.Endpoints["receiver"].Result.Evidence.ValidSamples != 0 {
			t.Fatal("no-media scenario invented frames")
		}
		if len(r.Logs) != 1 {
			t.Fatal("failure did not trigger a shared Service diagnostic")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		f := setup(t, "no_media")
		r := start(t, f.hub)
		eventually(t, 5*time.Second, func() bool { return detail(t, f.hub, r.ID).Status == "RUNNING" })
		// Replacing the live control session must preserve the already-running worker.
		_ = f.hub.Session("probe-a")
		if e := f.hub.Cancel(r.ID); e != nil {
			t.Fatal(e)
		}
		eventually(t, 7*time.Second, func() bool {
			r = detail(t, f.hub, r.ID)
			return r.Endpoints["sender"].Result != nil && r.Endpoints["receiver"].Result != nil
		})
		if r.Status != "CANCELLED" {
			t.Fatal("late evidence overwrote cancellation")
		}
		workers := map[string]bool{}
		for _, event := range r.Endpoints["sender"].Events {
			if event.Name == "worker_started" {
				workers[event.WorkerID] = true
			}
		}
		if len(workers) != 1 {
			t.Fatal("control reconnect started a duplicate worker")
		}
	})
}
