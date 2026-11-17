package hub

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
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
	task := map[string]any{"task_id": "one", "target_id": target.ID, "probe_id": "probe-a", "interval_seconds": 60, "timeout_seconds": 30}
	tasks := []any{task}
	if w := configRequest(t, h, targets, tasks); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, _ := h.task("one")
	if h.enabled(current) {
		t.Fatal("creation enabled task through configuration")
	}
	for _, key := range []string{"enabled", "sender_probe_id", "receiver_probe_id"} {
		task[key] = true
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
func TestLegacySingleProbeBindingMigration(t *testing.T) {
	h := targetPlanFixture(t)
	p := Plan{Targets: []protocol.Target{{ID: "target", Name: "target", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "none"}}}, Tasks: []protocol.Task{targetTask("task", "target")}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(h.state)
	var root map[string]any
	_ = json.Unmarshal(raw, &root)
	task := root["plan"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	task["sender_probe_id"], task["receiver_probe_id"] = "probe-a", "probe-a"
	delete(task, "probe_id")
	if err := durable.Write(h.dir+"/state.json", root); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Targets = nil
	cfg.Tasks = nil
	for i := 0; i < 2; i++ {
		restored, err := New(h.dir, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		current, _ := restored.task("task")
		if current.ProbeID != "probe-a" || !restored.enabled(current) {
			t.Fatal("migration lost binding or enabled state")
		}
		stored, _ := json.Marshal(restored.state.Plan)
		if bytes.Contains(stored, []byte("sender_probe_id")) || bytes.Contains(stored, []byte("receiver_probe_id")) {
			t.Fatal("legacy binding retained")
		}
	}
}
