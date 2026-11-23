package hub

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func configRequest(t *testing.T, h *Hub, targets []any, tasks []any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"expected_revision": planRevision(h.planLocked()), "targets": targets, "tasks": tasks})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeAdmin(response, httptest.NewRequest("POST", "/api/v1/rtc/config", bytes.NewReader(body)))
	return response
}
func TestSimplifiedConfigurationContractAndSingleEnablePath(t *testing.T) {
	h := targetPlanFixture(t)
	targets := []any{map[string]any{"name": "服务名称", "driver": "agora", "rtc": map[string]any{"app_id": strings.Repeat("a", 32), "auth_mode": "none"}}}
	if w := configRequest(t, h, targets, nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	target := h.planLocked().Targets[0]
	if !protocol.ValidID(target.ID) || target.Name != "服务名称" {
		t.Fatal("target identity was not generated")
	}
	configTarget := targetInput{ID: target.ID, Name: target.Name, Driver: target.Driver, RTC: target.RTC}
	targets = []any{configTarget}
	task := map[string]any{"task_id": "one", "target_id": target.ID, "host_id": "host-probe-a", "service_type": "rtc", "client_type": "native", "interval_seconds": 60, "timeout_seconds": 30}
	tasks := []any{task}
	if w := configRequest(t, h, targets, tasks); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, _ := h.task("one")
	if h.enabled(current) {
		t.Fatal("creation enabled task through configuration")
	}
	for _, key := range []string{"enabled", "probe_id", "runtime_id", "sender_probe_id", "receiver_probe_id"} {
		task[key] = "old-runtime"
		if key == "enabled" {
			task[key] = true
		}
		if w := configRequest(t, h, targets, tasks); w.Code != 400 {
			t.Fatal("retired task input accepted", key, w.Code)
		}
		delete(task, key)
	}
	enable := httptest.NewRecorder()
	h.ServeAdmin(enable, httptest.NewRequest("POST", "/api/v1/rtc/tasks/one/enabled", strings.NewReader(`{"enabled":true}`)))
	if enable.Code != 202 {
		t.Fatal(enable.Code, enable.Body.String())
	}
	task["description"] = "changed"
	if w := configRequest(t, h, targets, tasks); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, _ = h.task("one")
	if !h.enabled(current) {
		t.Fatal("editing configuration changed enabled state")
	}
	if err := h.Enable("one", false); err != nil {
		t.Fatal(err)
	}
	if w := configRequest(t, h, targets, tasks); w.Code != 200 {
		t.Fatal(w.Code)
	}
	current, _ = h.task("one")
	if h.enabled(current) {
		t.Fatal("editing configuration re-enabled task")
	}
	public, _ := json.Marshal(h.Snapshot())
	for _, field := range []string{"sender_probe_id", "receiver_probe_id", "sender_uid", "receiver_uid", "channel_prefix", "channel_name", "has_sender_token", "has_receiver_token", "latest_result"} {
		if bytes.Contains(public, []byte(`"`+field+`"`)) {
			t.Fatal("retired field returned", field)
		}
	}
	for _, field := range []string{"sender_uid", "receiver_uid", "channel_prefix", "channel_name"} {
		rtc := map[string]any{"app_id": strings.Repeat("a", 32), "auth_mode": "none", field: 10001}
		if w := configRequest(t, h, []any{map[string]any{"target_id": target.ID, "name": target.Name, "driver": "agora", "rtc": rtc}}, tasks); w.Code != 400 {
			t.Fatal("retired RTC input accepted", field, w.Code)
		}
	}
	target.RTC.AuthMode = "token"
	if w := configRequest(t, h, []any{configTarget}, tasks); w.Code != 409 {
		t.Fatal("pre-signed token mode accepted", w.Code)
	}
}
func TestTaskDisplayNamePreservesIdentityAndEnableState(t *testing.T) {
	h := targetPlanFixture(t)
	initial := Plan{Targets: []protocol.Target{{ID: "target", Name: "RTC", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "none"}}}, Tasks: []protocol.Task{targetTask("task", "target")}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), initial); err != nil {
		t.Fatal(err)
	}
	plan := h.planLocked()
	original := plan.Tasks[0]
	task := map[string]any{"task_id": original.ID, "name": "Native 音视频探测", "target_id": original.TargetID, "host_id": original.HostID, "service_type": "rtc", "client_type": "native", "interval_seconds": original.IntervalSeconds, "timeout_seconds": original.TimeoutSeconds}
	targets := make([]any, 0, len(plan.Targets))
	for _, v := range plan.Targets {
		targets = append(targets, targetInput{ID: v.ID, Name: v.Name, Driver: v.Driver, RTC: v.RTC})
	}
	if w := configRequest(t, h, targets, []any{task}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, ok := h.task(original.ID)
	if !ok || current.Name != "Native 音视频探测" || h.enabled(current) != h.enabled(original) {
		t.Fatal("name changed identity or enable state", current)
	}
	delete(task, "name")
	if w := configRequest(t, h, targets, []any{task}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, _ = h.task(original.ID)
	if current.Name != "Native 音视频探测" {
		t.Fatal("legacy client cleared display name")
	}
	task["name"] = strings.Repeat("x", 241)
	if w := configRequest(t, h, targets, []any{task}); w.Code == 200 {
		t.Fatal("oversized name accepted")
	}
}
