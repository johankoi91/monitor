package hub

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func testConfig() Config {
	return Config{Probes: []ProbeConfig{{ID: "probe-a", Secret: strings.Repeat("a", 32), Location: "A"}, {ID: "probe-b", Secret: strings.Repeat("b", 32), Location: "B"}}, Targets: []protocol.Target{{ID: "service", Name: "overall RTC", Driver: "mock"}}, Tasks: []protocol.Task{{ID: "audio", TargetID: "service", Sender: "probe-a", Receiver: "probe-b", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 5, Scenario: "success"}}}
}
func online(t *testing.T, h *Hub, id string) *session {
	t.Helper()
	s := h.Session(id)
	m := protocol.Msg("hello")
	m.Driver = "mock"
	m.Capacity = 2
	if _, e := h.Receive(id, s, m); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestOwnershipReplaySessionReplacementAndRestart(t *testing.T) {
	dir := t.TempDir()
	h, e := New(dir, testConfig(), nil)
	if e != nil {
		t.Fatal(e)
	}
	a := online(t, h, "probe-a")
	b := online(t, h, "probe-b")
	r, e := h.Start("audio")
	if e != nil {
		t.Fatal(e)
	}
	ev := protocol.Event{ID: protocol.ID(), WorkerID: protocol.ID(), Seq: 1, At: time.Now().UTC(), Kind: "sdk_callback", Name: "OnConnected"}
	m := protocol.Msg("telemetry_batch")
	m.RunID = r.ID
	m.Revision = r.Revision
	m.Role = "sender"
	m.Events = []protocol.Event{ev}
	if _, e = h.Receive("probe-b", b, m); e == nil {
		t.Fatal("cross-probe evidence accepted")
	}
	for i := 0; i < 2; i++ {
		ack, e := h.Receive("probe-a", a, m)
		if e != nil || len(ack.EventIDs) != 1 {
			t.Fatal("idempotent evidence failed", e)
		}
	}
	if len(h.state.Runs[r.ID].Endpoints["sender"].Events) != 1 {
		t.Fatal("replay duplicated evidence")
	}
	m.Events[0].Name = "different"
	if _, e = h.Receive("probe-a", a, m); e == nil {
		t.Fatal("conflicting replay accepted")
	}
	_ = online(t, h, "probe-a")
	if _, e = h.Receive("probe-a", a, m); e == nil {
		t.Fatal("old session accepted")
	}
	restored, e := New(dir, testConfig(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if restored.state.Runs[r.ID].Status != "ERROR" || restored.state.Runs[r.ID].Reason != "CENTER_RESTARTED" {
		t.Fatal("restart retained live execution")
	}
	bstate, _ := json.Marshal(restored.Snapshot())
	if !strings.Contains(string(bstate), `"rtc_service_status":"UNKNOWN"`) {
		t.Fatal("restart restored fake health")
	}
}

type brokenDocuments struct {
	fail  bool
	value []byte
}

func (d *brokenDocuments) LoadDocument(string) ([]byte, bool, error) {
	return d.value, len(d.value) > 0, nil
}
func (d *brokenDocuments) SaveDocument(_ string, b []byte) error {
	if d.fail {
		return errors.New("database offline")
	}
	d.value = append([]byte(nil), b...)
	return nil
}
func TestFailedPersistenceDoesNotPublishRunOrAck(t *testing.T) {
	d := &brokenDocuments{}
	h, e := New(t.TempDir(), testConfig(), d)
	if e != nil {
		t.Fatal(e)
	}
	a := online(t, h, "probe-a")
	_ = online(t, h, "probe-b")
	d.fail = true
	if _, e = h.Start("audio"); e == nil || len(h.state.Runs) != 0 {
		t.Fatal("failed commit published run")
	}
	d.fail = false
	r, e := h.Start("audio")
	if e != nil {
		t.Fatal(e)
	}
	m := protocol.Msg("run_ready")
	m.RunID = r.ID
	m.Revision = r.Revision
	m.Role = "sender"
	d.fail = true
	if _, e = h.Receive("probe-a", a, m); e == nil || h.state.Runs[r.ID].Endpoints["sender"].Ready {
		t.Fatal("failed commit acknowledged state")
	}
}

func TestEditablePlanPersistsAndCancelsOldExecution(t *testing.T) {
	dir := t.TempDir()
	h, err := New(dir, testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	online(t, h, "probe-a")
	online(t, h, "probe-b")
	run, err := h.Start("audio")
	if err != nil {
		t.Fatal(err)
	}
	p := h.planLocked()
	revision := planRevision(p)
	p.Targets = append([]protocol.Target(nil), p.Targets...)
	p.Targets[0].Name = "Updated overall RTC"
	if err = h.UpdatePlan(revision, p); err != nil {
		t.Fatal(err)
	}
	if h.state.Runs[run.ID].Status != "CANCELLED" || h.state.Runs[run.ID].Reason != "CONFIG_CHANGED" {
		t.Fatal("config change left old execution active")
	}
	if err = h.UpdatePlan(revision, p); err == nil {
		t.Fatal("stale configuration accepted")
	}
	restored, err := New(dir, testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if restored.config.Targets[0].Name != "Updated overall RTC" {
		t.Fatal("saved configuration lost on restart")
	}
	p = restored.planLocked()
	revision = planRevision(p)
	p.Tasks[0].Receiver = p.Tasks[0].Sender
	if restored.UpdatePlan(revision, p) == nil {
		t.Fatal("same probe used as both identities")
	}
}
