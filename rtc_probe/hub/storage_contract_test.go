package hub

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func TestRetiredStateOperationHistoryIsNotStoredOrExposed(t *testing.T) {
	h, task := healthFixture(t)
	now := time.Now().UTC()
	for _, status := range []string{"SUCCESS", "FAILED", "FAILED", "FAILED", "SUCCESS", "SUCCESS"} {
		now = now.Add(time.Second)
		run := &Run{ID: protocol.ID(), TaskID: task.ID, TargetID: task.TargetID, Revision: revision(task, h.target(task.TargetID)), Driver: "agora", Status: status, CreatedAt: now, FinishedAt: &now}
		if err := h.change(func(s *state) error { h.applyHealth(s, run, now); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if h.state.Health[task.ID].State != "HEALTHY" {
		t.Fatal("current health calculation regressed")
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	usage, err := h.Usage()
	if err != nil {
		t.Fatal(err)
	}
	encodedUsage, _ := json.Marshal(usage)
	for _, retired := range []string{`"pending_audits"`, `"audit_days"`, `"audit_id"`, `"before"`, `"after"`} {
		if bytes.Contains(data, []byte(retired)) {
			t.Fatal("retired data persisted", retired)
		}
	}
	if bytes.Contains(encodedUsage, []byte(`"audits"`)) {
		t.Fatal("retired usage field returned")
	}
	for _, method := range []string{"GET", "POST"} {
		response := httptest.NewRecorder()
		h.ServeAdmin(response, httptest.NewRequest(method, "/api/v1/rtc/audits", nil))
		if response.Code != 404 {
			t.Fatal("retired endpoint still exists", method, response.Code)
		}
	}
	settings, _ := json.Marshal(h.state.Settings)
	var payload map[string]any
	_ = json.Unmarshal(settings, &payload)
	payload["expected_version"] = h.state.Settings.Version
	payload["audit_days"] = 30
	body, _ := json.Marshal(payload)
	response := httptest.NewRecorder()
	h.ServeAdmin(response, httptest.NewRequest("POST", "/api/v1/rtc/settings", bytes.NewReader(body)))
	if response.Code != 400 {
		t.Fatal("retired retention input accepted")
	}
	if _, err = os.Stat(filepath.Join(h.dir, "history", "audit")); !os.IsNotExist(err) {
		t.Fatal("retired directory created")
	}
}
func TestUpgradePurgesRetiredQueuedStateAndFiles(t *testing.T) {
	h, _ := healthFixture(t)
	data, _ := json.Marshal(h.state)
	var legacy map[string]any
	_ = json.Unmarshal(data, &legacy)
	legacy["pending_audits"] = []any{map[string]any{"audit_id": "old-history", "action": "RTC_HEALTH_CHANGED"}}
	legacy["settings"].(map[string]any)["audit_days"] = 30
	if err := durable.Write(filepath.Join(h.dir, "state.json"), legacy); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.dir, "history", "audit")
	if err := durable.Write(filepath.Join(dir, "old.json"), map[string]string{"audit_id": "old-history"}); err != nil {
		t.Fatal(err)
	}
	restored, err := New(h.dir, h.config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.config.Tasks) != 1 {
		t.Fatal("upgrade lost task")
	}
	data, err = os.ReadFile(filepath.Join(h.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("pending_audits")) || bytes.Contains(data, []byte("audit_days")) || bytes.Contains(data, []byte("old-history")) {
		t.Fatal("retired state survived upgrade")
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("retired files survived upgrade")
	}
}
