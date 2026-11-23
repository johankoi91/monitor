package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func readTrace(t *testing.T, dir, role string) []protocol.Event {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, role, "sdk-api.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	var events []protocol.Event
	for scan.Scan() {
		var e protocol.Event
		if err = json.Unmarshal(scan.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	if err = scan.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}
func TestSDKArgumentsAreSnapshottedRedactedAndRelated(t *testing.T) {
	dir := t.TempDir()
	secret := "007-sensitive-test-value"
	r, err := New(dir, "worker-one", []string{secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Bind(protocol.Job{RunID: "run-one", TaskID: "task-one", Target: protocol.Target{ID: "rtc-service"}, RuntimeID: "probe-one", Revision: "revision-one"})
	args := map[string]any{"channel": "channel-one", "token": secret, "nested": map[string]any{"message": "rejected: " + secret}, "buffer": []byte{1, 2, 3, 4}}
	s := r.Begin("sender", "JOIN", "Connect", args, false)
	args["channel"] = "mutated-after-call"
	r.Callback("sender", "CONNECTION", "OnConnected", "Connect", map[string]any{"connection_info": map[string]any{"channel_id": "channel-one", "state": 3}, "reason": 0}, false)
	code := 0
	r.End(s, "int", &code, nil)
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	events := readTrace(t, dir, "sender")
	if len(events) != 3 {
		t.Fatalf("unexpected events %d", len(events))
	}
	if events[0].CallID != events[2].CallID || events[1].RelatedCallID != events[0].CallID {
		t.Fatal("callback/call correlation lost")
	}
	for _, e := range events {
		if e.RunID != "run-one" || e.TaskID != "task-one" || e.RuntimeID != "probe-one" || e.TargetID != "rtc-service" || e.ConnectionID != "worker-one/sender" || e.ServiceID != "worker-one/service" {
			t.Fatal("event context missing")
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "sender", "sdk-api.jsonl"))
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "mutated-after-call") || strings.Contains(string(data), "AQIDBA==") {
		t.Fatal("secret, mutable input or binary payload leaked")
	}
	if !strings.Contains(string(data), "payload_omitted") {
		t.Fatal("buffer metadata missing")
	}
}
func TestEveryMediaCallIsLocalWhileUplinkIsSummarized(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var forwarded []protocol.Event
	r, err := New(dir, "worker", nil, func(e protocol.Event) { mu.Lock(); forwarded = append(forwarded, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		s := r.Begin("sender", "MEDIA", "PushAudioPcmData", map[string]any{"sample_rate": 16000, "start_pts_in_ms": 1000 + i*10}, true)
		code := 0
		r.End(s, "int", &code, nil)
	}
	r.Callback("receiver", "RECEIVE_VIDEO", "OnRenderVideoFrame", "SubscribeVideo", map[string]any{"accepted": false, "frame": map[string]any{"width": 352, "height": 288}}, true)
	r.Callback("receiver", "RECEIVE_VIDEO", "OnRenderVideoFrame", "SubscribeVideo", map[string]any{"accepted": true, "first_valid_frame": true, "frame": map[string]any{"width": 352, "height": 288}}, true)
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(readTrace(t, dir, "sender")); got != 1002 {
		t.Fatalf("lost full call detail: %d", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(forwarded) > 12 {
		t.Fatalf("media flooded uplink: %d", len(forwarded))
	}
	firstValid := false
	summary := false
	for _, e := range forwarded {
		if e.Kind == "sdk_trace_summary" {
			summary = true
		}
		if p, ok := e.Fields["parameters"].(map[string]any); ok && p["first_valid_frame"] == true {
			firstValid = true
		}
	}
	if !firstValid || !summary {
		t.Fatal("missing first valid frame or summary")
	}
}
