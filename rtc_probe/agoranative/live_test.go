//go:build linux && agora

package agoranative

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/tests/testkit"
)

// The real SDK remains inside the same isolated process boundary as production.
// Fault injection omits actual send calls; it never manufactures callbacks.
func TestNativeAcceptanceWorker(t *testing.T) {
	if os.Getenv("RTC_NATIVE_QA_WORKER") != "1" {
		return
	}
	pipe := os.NewFile(3, "native-qa-evidence")
	policy := mediaPolicy{}
	switch os.Getenv("RTC_NATIVE_QA_POLICY") {
	case "no_media":
		policy = mediaPolicy{true, true}
	case "no_audio":
		policy.omitAudio = true
	case "no_video":
		policy.omitVideo = true
	}
	err := run(context.Background(), os.Stdin, pipe, policy)
	pipe.Close()
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func liveParameters(t *testing.T) *protocol.RTCParameters {
	t.Helper()
	if os.Getenv("RTC_NATIVE_LIVE_QA") != "1" {
		t.Skip("explicit real RTC acceptance required")
	}
	access := os.Getenv("RTC_NATIVE_QA_ACCESS_IP")
	verify := os.Getenv("RTC_NATIVE_QA_VERIFY_DOMAIN")
	if access == "" || verify == "" {
		t.Skip("explicit QA access IP and verify domain required")
	}
	p := &protocol.RTCParameters{AppID: os.Getenv("RTC_NATIVE_QA_APP_ID"), AuthMode: "none", Private: true, AccessIPs: []string{access}, VerifyDomain: verify, ProxyMode: 0, DisableAut: true}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	return p
}

type nativeFixture struct {
	hub    *hub.Hub
	dir    string
	client *probe.Client
}

func newNativeFixture(t *testing.T, tasks int, policy string) *nativeFixture {
	t.Helper()
	p := liveParameters(t)
	root := t.TempDir()
	cfg := hub.Config{MaxConcurrentRuns: 2, Targets: []protocol.Target{{ID: "qa-overall-rtc", Name: "isolated RTC functional acceptance", Driver: "agora", RTC: p}}}
	for i := 0; i < tasks; i++ {
		copy := *p
		cfg.Tasks = append(cfg.Tasks, protocol.Task{ID: fmt.Sprintf("qa-av-%02d", i), TargetID: "qa-overall-rtc", HostID: "host-qa-native", Enabled: true, IntervalSeconds: 3600, TimeoutSeconds: 15, RTC: &copy, ServiceType: "rtc", ClientType: "native"})
	}
	h, err := hub.New(filepath.Join(root, "center"), hub.Config{MaxConcurrentRuns: cfg.MaxConcurrentRuns}, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientConfig, err := testkit.ConfigureClient(h, root, cfg, "agora")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hubDone := make(chan struct{})
	go func() { defer close(hubDone); h.Run(ctx) }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if testkit.ServeEnrollment(h, w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/probes/") {
			h.ServeProbe(w, r)
		} else {
			h.ServeAdmin(w, r)
		}
	}))
	clientConfig.URL = "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probes/connect"
	clientConfig.Capacity = 2
	clientConfig.WorkerCommand = func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeAcceptanceWorker$")
		cmd.Env = append(os.Environ(), "RTC_NATIVE_QA_WORKER=1", "RTC_NATIVE_QA_POLICY="+policy)
		return cmd
	}
	c, err := probe.New(clientConfig)
	if err != nil {
		server.Close()
		cancel()
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-clientDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("native probe did not stop")
		}
		<-hubDone
		server.Close()
	})
	return &nativeFixture{hub: h, dir: root, client: c}
}
func waitNative(t *testing.T, f *nativeFixture, id string, timeout time.Duration) hub.Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := f.hub.Detail(id)
		if err != nil {
			t.Fatal(err)
		}
		var r hub.Run
		if err = json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		switch r.Status {
		case "SUCCESS", "FAILED", "ERROR", "CANCELLED":
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("real RTC round did not finish")
	return hub.Run{}
}
func startNative(t *testing.T, f *nativeFixture, task string) hub.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.hub.Start(task)
		if err == nil {
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("real probe did not register or free its slot")
	return hub.Run{}
}
func saveNativeReport(t *testing.T, name string, runs []hub.Run) {
	t.Helper()
	dir := os.Getenv("RTC_NATIVE_QA_REPORT_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Errors                        []protocol.Event
		RunID, TaskID, Status, Reason string
		DurationMS                    int64
		Results                       map[string]*protocol.Result
	}
	report := []row{}
	for _, r := range runs {
		v := row{RunID: r.ID, TaskID: r.TaskID, Status: r.Status, Reason: r.Reason, Results: map[string]*protocol.Result{}}
		if r.FinishedAt != nil {
			v.DurationMS = r.FinishedAt.Sub(r.CreatedAt).Milliseconds()
		}
		for role, e := range r.Endpoints {
			v.Results[role] = e.Result
			for _, event := range e.Events {
				if event.Name == "stage_failed" || event.Name == "OnConnectionFailure" || event.Name == "OnError" || event.Code != nil && *event.Code != 0 {
					v.Errors = append(v.Errors, event)
				}
			}
		}
		report = append(report, v)
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, name+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestNativeLiveMissingMediaCannotPass(t *testing.T) {
	liveParameters(t)
	for _, policy := range []string{"no_media", "no_audio", "no_video"} {
		t.Run(policy, func(t *testing.T) {
			f := newNativeFixture(t, 1, policy)
			run := startNative(t, f, "qa-av-00")
			r := waitNative(t, f, run.ID, 35*time.Second)
			saveNativeReport(t, policy, []hub.Run{r})
			preserveNativeEvidence(t, f, policy, []hub.Run{r})
			if r.Status != "FAILED" {
				t.Fatalf("missing media should be a real business failure: %s/%s", r.Status, r.Reason)
			}
			a, b := r.Endpoints["sender"].Result, r.Endpoints["receiver"].Result
			if a == nil || b == nil || !a.Evidence.Connected || !b.Evidence.Connected {
				t.Fatal("negative case did not actually join RTC")
			}
			if policy == "no_audio" && (b.Evidence.MediaDurationMS != 0 || b.Evidence.ReceivedVideoFrames < 5) {
				t.Fatal("audio-only fault not isolated", b.Evidence)
			}
			if policy == "no_video" && (b.Evidence.MediaDurationMS < 1000 || b.Evidence.ReceivedVideoFrames != 0) {
				t.Fatal("video-only fault not isolated", b.Evidence)
			}
			if policy == "no_media" && (b.Evidence.MediaDurationMS != 0 || b.Evidence.ReceivedVideoFrames != 0) {
				t.Fatal("no-media case received unexpected data")
			}
			if !a.TraceComplete || !b.TraceComplete || a.TraceDropped != 0 || b.TraceDropped != 0 {
				t.Fatal("negative acceptance trace incomplete")
			}
		})
	}
}
func TestNativeLiveTenTasksTwoWorkers(t *testing.T) {
	liveParameters(t)
	f := newNativeFixture(t, 10, "")
	var runs []hub.Run
	t.Log("two simultaneous tasks per batch; 30 seconds between batches to respect observed target admission limits")
	for batch := 0; batch < 5; batch++ {
		if batch > 0 {
			time.Sleep(30 * time.Second)
		}
		a := startNative(t, f, fmt.Sprintf("qa-av-%02d", batch*2))
		b := startNative(t, f, fmt.Sprintf("qa-av-%02d", batch*2+1))
		if _, err := f.hub.Start(a.TaskID); err == nil {
			t.Fatal("same RTC task re-entered")
		}
		pair := []hub.Run{waitNative(t, f, a.ID, 35*time.Second), waitNative(t, f, b.ID, 35*time.Second)}
		preserveNativeEvidence(t, f, "ten-tasks", pair)
		for _, r := range pair {
			runs = append(runs, r)
			if r.Status != "SUCCESS" {
				saveNativeReport(t, "ten-tasks", runs)
				t.Fatalf("real concurrent task failed: %s/%s", r.Status, r.Reason)
			}
			a, b := r.Endpoints["sender"].Result, r.Endpoints["receiver"].Result
			if a.Cleanup != "SUCCESS" || b.Cleanup != "SUCCESS" || !a.TraceComplete || !b.TraceComplete || a.TraceDropped != 0 || b.TraceDropped != 0 || b.Evidence.MediaDurationMS < 1000 || b.Evidence.ReceivedVideoFrames < 5 {
				t.Fatal("real concurrent media evidence incomplete")
			}
		}
	}
	saveNativeReport(t, "ten-tasks", runs)
	preserveNativeEvidence(t, f, "ten-tasks", runs)
	t.Log("10 distinct real RTC tasks passed with two isolated workers in one probe service")
}

func preserveNativeEvidence(t *testing.T, f *nativeFixture, name string, runs []hub.Run) {
	t.Helper()
	dir := os.Getenv("RTC_NATIVE_QA_REPORT_DIR")
	if dir == "" {
		return
	}
	for _, run := range runs {
		source := filepath.Join(f.dir, "probe", "runs", run.ID)
		err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "bundles" {
				return filepath.SkipDir
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dir, name, "runs", run.ID, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeLiveMediaEdgeSweep(t *testing.T) {
	p := liveParameters(t)
	rangeText := os.Getenv("RTC_NATIVE_QA_EDGE_RANGE")
	if rangeText == "" {
		t.Skip("explicit media edge range required")
	}
	parts := strings.Split(rangeText, "-")
	if len(parts) != 2 {
		t.Fatal("range must be start-end")
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	f := newNativeFixture(t, 1, "")
	var cfg struct {
		Revision string            `json:"config_revision"`
		Targets  []protocol.Target `json:"targets"`
		Tasks    []protocol.Task   `json:"tasks"`
	}
	raw, _ := json.Marshal(f.hub.Snapshot())
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Targets {
		if cfg.Targets[i].ID == cfg.Tasks[0].TargetID {
			cfg.Targets[i].RTC.MediaEdges = &protocol.MediaEdgeRange{Host: p.AccessIPs[0], StartPort: start, EndPort: end}
		}
	}
	cfg.Tasks[0].IntervalSeconds = 600
	if err = f.hub.UpdatePlan(cfg.Revision, hub.Plan{Targets: cfg.Targets, Tasks: cfg.Tasks}); err != nil {
		t.Fatal(err)
	}
	parent := startNative(t, f, "qa-av-00")
	final := waitNative(t, f, parent.ID, time.Duration(end-start+1)*35*time.Second+10*time.Second)
	var children []hub.Run
	for _, edge := range final.Edges {
		if edge.RunID == "" {
			t.Fatal("configured edge never executed", edge.Endpoint)
		}
		raw, err := f.hub.Detail(edge.RunID)
		if err != nil {
			t.Fatal(err)
		}
		var child hub.Run
		if err = json.Unmarshal(raw, &child); err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	saveNativeReport(t, "media-edges", children)
	preserveNativeEvidence(t, f, "media-edges", children)
	parentJSON, _ := json.MarshalIndent(final, "", "  ")
	if dir := os.Getenv("RTC_NATIVE_QA_REPORT_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "media-edge-sweep.json"), parentJSON, 0600)
	}
	expected := "SUCCESS"
	if os.Getenv("RTC_NATIVE_QA_EXPECT_FAILURE") == "1" {
		expected = "FAILED"
	}
	if final.Status != expected {
		t.Fatalf("edge sweep outcome: %s/%s, expected %s", final.Status, final.Reason, expected)
	}
	expectedPorts := map[string]string{}
	for _, item := range strings.Split(os.Getenv("RTC_NATIVE_QA_PORT_OUTCOMES"), ",") {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			expectedPorts[os.Getenv("RTC_NATIVE_QA_ACCESS_IP")+":"+parts[0]] = parts[1]
		}
	}
	for _, child := range children {
		if expected, ok := expectedPorts[child.EdgeEndpoint]; ok && child.Status != expected {
			t.Fatalf("edge %s: got %s/%s, expected %s", child.EdgeEndpoint, child.Status, child.Reason, expected)
		}
		found := false
		for _, event := range child.Endpoints["sender"].Events {
			if event.Name == "SetParameters" && event.Kind == "api_call_begin" {
				args, _ := event.Fields["args"].(map[string]any)
				value, _ := args["parameters"].(string)
				wanted, _ := protocol.VOSParameters(child.EdgeEndpoint)
				if value == wanted {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("selected VOS not recorded in actual SDK call", child.EdgeEndpoint)
		}
	}
	t.Logf("checked %d edge endpoints, parent outcome %s", len(children), final.Status)
}
