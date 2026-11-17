package hub

import (
	"encoding/json"
	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"strings"
	"testing"
)

func targetPlanFixture(t *testing.T) *Hub {
	t.Helper()
	cfg := testConfig()
	cfg.Tasks = nil
	cfg.Targets = nil
	h, err := New(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"probe-a", "probe-b"} {
		session := h.Session(id)
		m := protocol.Msg("hello")
		m.Driver = "agora"
		m.Capacity = 4
		if _, err = h.Receive(id, session, m); err != nil {
			t.Fatal(err)
		}
	}
	return h
}
func targetTask(id, target string) protocol.Task {
	return protocol.Task{ID: id, TargetID: target, ProbeID: "probe-a", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 30}
}
func TestTargetOwnsSharedRTCAndCredentials(t *testing.T) {
	h := targetPlanFixture(t)
	certificate := strings.Repeat("c", 32)
	plan := Plan{Targets: []protocol.Target{{ID: "rtc", Name: "RTC", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "certificate"}, Credentials: &protocol.AppCredentials{AppCertificate: certificate}}}, Tasks: []protocol.Task{targetTask("a", "rtc"), targetTask("b", "rtc")}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), plan); err != nil {
		t.Fatal(err)
	}
	if len(h.state.Credentials) != 0 || h.state.TargetCredentials["rtc"].AppCertificate != certificate {
		t.Fatal("credentials are not target-owned")
	}
	for _, task := range h.state.Plan.Tasks {
		if task.RTC != nil || task.Credentials != nil {
			t.Fatal("duplicated task settings persisted")
		}
	}
	public := h.planLocked()
	if !public.Targets[0].HasCertificate || public.Tasks[0].RTC != nil {
		t.Fatal("public configuration ownership incorrect")
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), certificate) {
		t.Fatal("target certificate leaked")
	}
	for _, id := range []string{"a", "b"} {
		r, err := h.Start(id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Endpoints["sender"].Job.RTC.AppID != strings.Repeat("a", 32) {
			t.Fatal("task did not inherit target")
		}
		m, ok := h.pairCommand(h.state.Runs[r.ID])
		if !ok || m.Job.Credentials.SenderToken == "" || m.Job.Credentials.AppCertificate != "" {
			t.Fatal("target credentials not converted to scoped tokens")
		}
	}
}
func TestTargetChangeOnlyCancelsAssociatedTasksAndKeepsHistorySnapshot(t *testing.T) {
	h := targetPlanFixture(t)
	p := Plan{Targets: []protocol.Target{{ID: "one", Name: "one", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "none"}}, {ID: "two", Name: "two", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("b", 32), AuthMode: "none"}}}, Tasks: []protocol.Task{targetTask("a", "one"), targetTask("b", "two")}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), p); err != nil {
		t.Fatal(err)
	}
	first, err := h.Start("a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Start("b")
	if err != nil {
		t.Fatal(err)
	}
	next := h.planLocked()
	next.Targets[0].RTC.AccessIPs = []string{"111.230.192.59"}
	next.Targets[0].RTC.AccessDomains = []string{"111.230.192.59"}
	next.Targets[0].RTC.Private = true
	if err = h.UpdatePlan(planRevision(h.planLocked()), next); err != nil {
		t.Fatal(err)
	}
	if h.state.Runs[first.ID].Status != "CANCELLED" || terminal(h.state.Runs[second.ID].Status) {
		t.Fatal("target edit cancelled unrelated task")
	}
	if h.state.Runs[first.ID].Endpoints["sender"].Job.RTC.Private {
		t.Fatal("historical execution settings were overwritten")
	}
	task, _ := h.task("a")
	if !task.RTC.Private {
		t.Fatal("associated task did not inherit updated target")
	}
}
func TestLegacyTargetMigrationSplitsConflictingSettingsAndPreservesSecrets(t *testing.T) {
	cfg := testConfig()
	cfg.Targets = nil
	cfg.Tasks = nil
	dir := t.TempDir()
	tasks := []protocol.Task{targetTask("a", "legacy"), targetTask("b", "legacy")}
	tasks[0].RTC = &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "certificate"}
	tasks[1].RTC = &protocol.RTCParameters{AppID: strings.Repeat("b", 32), AuthMode: "certificate"}
	old := state{Plan: &Plan{Targets: []protocol.Target{{ID: "legacy", Name: "legacy", Driver: "agora"}}, Tasks: tasks}, Credentials: map[string]protocol.Credentials{"a": {AppCertificate: strings.Repeat("c", 32)}, "b": {AppCertificate: strings.Repeat("d", 32)}}, Runs: map[string]*Run{}, Enabled: map[string]bool{}, Settings: defaultSettings()}
	if err := durable.Write(dir+"/state.json", old); err != nil {
		t.Fatal(err)
	}
	h, err := New(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.config.Targets) != 2 || h.config.Tasks[0].TargetID == h.config.Tasks[1].TargetID {
		t.Fatal("conflicting legacy RTC settings overwritten")
	}
	for i, task := range h.config.Tasks {
		expected := strings.Repeat([]string{"c", "d"}[i], 32)
		if h.state.TargetCredentials[task.TargetID].AppCertificate != expected {
			t.Fatal("migration lost credential")
		}
	}
	reloaded, err := New(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.config.Tasks[1].TargetID != h.config.Tasks[1].TargetID || len(reloaded.config.Targets) != 2 {
		t.Fatal("migration not idempotent")
	}
}
func TestTaskCannotOverrideTargetOrWriteTargetCredentials(t *testing.T) {
	h := targetPlanFixture(t)
	p := Plan{Targets: []protocol.Target{{ID: "rtc", Name: "rtc", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "none"}}}, Tasks: []protocol.Task{targetTask("task", "rtc")}}
	p.Tasks[0].RTC = &protocol.RTCParameters{AppID: strings.Repeat("b", 32), AuthMode: "none"}
	if err := h.UpdatePlan(planRevision(h.planLocked()), p); err != nil {
		t.Fatal(err)
	}
	task, _ := h.task("task")
	if task.RTC.AppID != strings.Repeat("a", 32) {
		t.Fatal("task overrode canonical target settings")
	}
	next := h.planLocked()
	next.Tasks[0].Credentials = &protocol.AppCredentials{AppCertificate: strings.Repeat("c", 32)}
	if err := h.UpdatePlan(planRevision(h.planLocked()), next); err == nil {
		t.Fatal("task credential override accepted")
	}
}

func TestTargetCredentialScopeChangeRequiresReplacementAndIsAtomic(t *testing.T) {
	h := targetPlanFixture(t)
	p := Plan{Targets: []protocol.Target{{ID: "rtc", Name: "RTC", Driver: "agora", RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "certificate"}, Credentials: &protocol.AppCredentials{AppCertificate: strings.Repeat("c", 32)}}}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), p); err != nil {
		t.Fatal(err)
	}
	before := planRevision(h.planLocked())
	oldSecret := h.state.TargetCredentials["rtc"]
	next := h.planLocked()
	next.Targets[0].RTC.AppID = strings.Repeat("b", 32)
	if err := h.UpdatePlan(before, next); err == nil {
		t.Fatal("changed project reused old certificate")
	}
	if planRevision(h.planLocked()) != before || h.state.TargetCredentials["rtc"] != oldSecret {
		t.Fatal("rejected update changed state")
	}
	next.Targets[0].Credentials = &protocol.AppCredentials{AppCertificate: strings.Repeat("d", 32)}
	if err := h.UpdatePlan(before, next); err != nil {
		t.Fatal(err)
	}
	if h.state.TargetCredentials["rtc"] == oldSecret {
		t.Fatal("replacement not applied")
	}
}
