package hub

import (
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func nativeHubFixture(t *testing.T) (*Hub, protocol.Task) {
	h := targetPlanFixture(t)
	cfg, inventory := nativeInventoryFixture()
	h.SetNativeInventoryProvider(func() (protocol.NativeInventory, error) { return inventory, nil })
	plan := Plan{Targets: []protocol.Target{{ID: "native", Name: "Native service", Driver: "agora", RTC: &protocol.RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", Private: true, AccessIPs: []string{"111.230.192.59"}, NativeCoverage: &cfg}}}, Tasks: []protocol.Task{targetTask("native-task", "native")}}
	plan.Tasks[0].IntervalSeconds = 180
	if err := h.UpdatePlan(planRevision(h.planLocked()), plan); err != nil {
		t.Fatal(err)
	}
	h.sessions["probe-a"].Capabilities = []string{protocol.ProbeEdgeAddressCapability, "rtc_audio_v1", "rtc_video_v1", "native_routes_v1"}
	task, _ := h.task("native-task")
	return h, task
}
func finishNativeChild(t *testing.T, h *Hub, child *Run, status string) {
	t.Helper()
	for _, role := range []string{"sender", "receiver"} {
		evidence := protocol.Evidence{Connected: true}
		if role == "sender" {
			evidence.Published = true
			evidence.VideoPublished = true
			evidence.SentFrames = 500
			evidence.SentVideoFrames = 75
		} else {
			evidence.Subscribed = true
			evidence.VideoSubscribed = true
			evidence.MediaDurationMS = 1200
			evidence.ReceivedVideoFrames = 15
		}
		m := protocol.Msg("run_result")
		m.RunID = child.ID
		m.Revision = child.Revision
		m.Role = role
		m.Result = &protocol.Result{ID: protocol.ID(), WorkerID: protocol.ID(), Driver: "agora", Outcome: status, Reason: status, Cleanup: "SUCCESS", FinishedAt: time.Now().UTC(), Evidence: evidence, TraceComplete: true, Route: &protocol.RouteEvidence{Guarded: true, Status: "VERIFIED", Scope: "test_fixture"}}
		if _, err := receiveTest(h, "probe-a", h.sessions["probe-a"], m); err != nil {
			t.Fatal(err)
		}
	}
}
func activeNativeChild(h *Hub) *Run {
	for _, r := range h.state.Runs {
		if r.NativeCase != nil && !terminal(r.Status) {
			return r
		}
	}
	return nil
}
func TestNativeCoverageRunsEveryPhaseAndNeverCountsAChildAsWholeService(t *testing.T) {
	h, task := nativeHubFixture(t)
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(task.ID); err == nil {
		t.Fatal("native task reentered")
	}
	count := 0
	for step := 0; step < 150; step++ {
		if child := activeNativeChild(h); child != nil {
			if child.Endpoints["sender"].Job.NativeCase == nil {
				t.Fatal("route specification missing")
			}
			finishNativeChild(t, h, child, "SUCCESS")
			count++
		}
		h.advanceNative(time.Now().UTC().Add(6 * time.Second))
		if h.nativeHealth(task, time.Now().UTC()).State == "HEALTHY" {
			break
		}
	}
	if count != 14 {
		t.Fatal("wrong Native case coverage", count)
	}
	if h.nativeHealth(task, time.Now().UTC()).State != "HEALTHY" {
		t.Fatal("complete coverage did not establish health")
	}
	for _, phase := range nativePhases {
		p := h.state.Native[task.ID].Phases[phase]
		if p.Round != 1 || p.Health.State != "HEALTHY" {
			t.Fatal("phase health missing", phase, p)
		}
	}
	for _, r := range h.state.Runs {
		if r.Kind == nativeCoverageKind && len(r.Endpoints) != 0 {
			t.Fatal("summary produced fake SDK endpoints")
		}
	}
}
func TestNativePhaseFailureBeatsUnknownAndOldCoverageCannotRestoreIt(t *testing.T) {
	h, task := nativeHubFixture(t)
	_, _ = h.Start(task.ID)
	current := h.state.Native[task.ID]
	for _, phase := range nativePhases {
		current.Phases[phase].Health = h.newHealth(task, time.Now().UTC(), "PENDING")
	}
	current.Phases["DIRECT"].Health.State = "UNHEALTHY"
	now := time.Now().UTC()
	current.Phases["DIRECT"].Health.LastResultAt = &now
	current.Phases["DIRECT"].Health.Reason = "FAILURE_CONFIRMED"
	if value := h.nativeHealth(task, time.Now().UTC()); value.State != "UNHEALTHY" {
		t.Fatal("unknown phase masked confirmed failure", value)
	}
	var parent *Run
	for _, r := range h.state.Runs {
		if r.Kind == nativeCoverageKind && r.NativePlan.Phase == "DIRECT" {
			parent = r
		}
	}
	current.Version = "new-inventory"
	parent.Status = "SUCCESS"
	h.applyNativePhaseResult(&h.state, parent, time.Now().UTC())
	if current.Phases["DIRECT"].Health.State != "UNHEALTHY" {
		t.Fatal("old coverage restored health")
	}
}
func TestNativeCancelStopsAllRemainingCases(t *testing.T) {
	h, task := nativeHubFixture(t)
	_, _ = h.Start(task.ID)
	if err := h.Enable(task.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range h.state.Runs {
		if r.TaskID != task.ID {
			continue
		}
		if !terminal(r.Status) {
			t.Fatal("disabled native execution left active", r.ID)
		}
		if r.Kind == nativeCoverageKind {
			for _, row := range r.Cases {
				if !terminal(row.Status) {
					t.Fatal("queued native case survived disable")
				}
			}
		}
	}
}

func TestNativeFailureExpiryAndUnknownAging(t *testing.T) {
	h, task := nativeHubFixture(t)
	_, _ = h.Start(task.ID)
	now := time.Now().UTC()
	current := h.state.Native[task.ID]
	p := current.Phases["DIRECT"]
	p.Health = h.newHealth(task, now, "FAILURE_CONFIRMED")
	p.Health.State = "UNHEALTHY"
	p.Health.LastRunID = "failed-direct"
	p.Health.LastResultAt = &now
	value := h.nativeHealth(task, now)
	if value.State != "UNHEALTHY" || value.LastRunID != "failed-direct" {
		t.Fatal("failure evidence lost", value)
	}
	later := now.Add(phaseInterval(task, "DIRECT")*2 + nativeBudget(task, p.CaseCount) + time.Second)
	value = h.nativeHealth(task, later)
	if value.State != "UNKNOWN" {
		t.Fatal("expired failure still active", value)
	}
	h.state.Health[task.ID] = value
	again := h.nativeHealth(task, later.Add(time.Minute))
	if again.UnknownSince == nil || value.UnknownSince == nil || !again.UnknownSince.Equal(*value.UnknownSince) {
		t.Fatal("unknown aging reset", value, again)
	}
}

func TestNativeFailureRetainsEvidenceWhenProbeOffline(t *testing.T) {
	h, task := nativeHubFixture(t)
	_, _ = h.Start(task.ID)
	now := time.Now().UTC()
	p := h.state.Native[task.ID].Phases["DIRECT"]
	p.Health = h.newHealth(task, now, "FAILURE_CONFIRMED")
	p.Health.State = "UNHEALTHY"
	p.Health.LastRunID = "failed-direct"
	p.Health.LastResultAt = &now
	h.sessions[h.runtimeForTask(task)].Seen = now.Add(-time.Minute * 2)
	value := h.effectiveHealth(task, now)
	if value.State != "UNHEALTHY" || value.LastRunID != "failed-direct" || value.InfrastructureReason != "PROBE_OFFLINE" {
		t.Fatal("probe offline masked fresh failure", value)
	}
}

func TestNativeParallelismFollowsInventoryAndResourceCeilings(t *testing.T) {
	h, task := nativeHubFixture(t)
	h.config.MaxConcurrentRuns = 32
	h.sessions[h.runtimeForTask(task)].Capacity = 64
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	active := func() int {
		n := 0
		for _, r := range h.state.Runs {
			if r.NativeCase != nil && !terminal(r.Status) {
				n++
			}
		}
		return n
	}
	if got := active(); got != 6 {
		t.Fatal("worker count must follow six actual Native members", got)
	}
	h.advanceNative(time.Now().UTC())
	if active() != 6 {
		t.Fatal("duplicate or excess dispatch")
	}
	if got := h.nativeWorkerLimit(task, 3); got != 3 {
		t.Fatal(got)
	}
	h.sessions[h.runtimeForTask(task)].Capacity = 4
	if got := h.nativeWorkerLimit(task, 9); got != 4 {
		t.Fatal("probe ceiling ignored", got)
	}
	h.sessions[h.runtimeForTask(task)].Capacity = 64
	h.config.MaxConcurrentRuns = 3
	if got := h.nativeWorkerLimit(task, 9); got != 3 {
		t.Fatal("global ceiling ignored", got)
	}
	h.config.MaxConcurrentRuns = 32
	child := activeNativeChild(h)
	finishNativeChild(t, h, child, "SUCCESS")
	h.advanceNative(time.Now().UTC())
	if active() != 5 {
		t.Fatal("cooling worker slot reused too soon", active())
	}
	past := time.Now().Add(-6 * time.Second)
	h.state.Runs[child.ID].FinishedAt = &past
	h.advanceNative(time.Now().UTC())
	if active() != 6 {
		t.Fatal("one cooling slot blocked unrelated slots", active())
	}
	if err := h.Enable(task.ID, false); err != nil {
		t.Fatal(err)
	}
	if active() != 0 {
		t.Fatal("parallel cancellation left active cases")
	}
}

func TestNativeParallelFailureDoesNotStopOtherCases(t *testing.T) {
	h, task := nativeHubFixture(t)
	h.config.MaxConcurrentRuns = 32
	h.sessions[h.runtimeForTask(task)].Capacity = 64
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	count := 0
	failedParent := ""
	for step := 0; step < 50; step++ {
		var children []*Run
		for _, r := range h.state.Runs {
			if r.NativeCase != nil && !terminal(r.Status) {
				children = append(children, r)
			}
		}
		for _, child := range children {
			status := "SUCCESS"
			if count == 0 {
				status = "FAILED"
				failedParent = child.ParentRunID
			}
			finishNativeChild(t, h, child, status)
			count++
		}
		h.advanceNative(time.Now().Add(6 * time.Second))
		complete := true
		for _, r := range h.state.Runs {
			if r.Kind == nativeCoverageKind && !terminal(r.Status) {
				complete = false
			}
		}
		if complete {
			break
		}
	}
	if count != 14 {
		t.Fatal("failure stopped sibling cases", count)
	}
	if h.state.Runs[failedParent].Status != "FAILED" {
		t.Fatal("failure lost")
	}
	for _, r := range h.state.Runs {
		if r.Kind == nativeCoverageKind && r.ID != failedParent && r.Status != "SUCCESS" {
			t.Fatal("unrelated phase failed", r.Status)
		}
	}
}

func TestSharedServiceCollectRequestsOneBundleAndDeduplicatesRequests(t *testing.T) {
	h, task := nativeHubFixture(t)
	h.sessions[h.runtimeForTask(task)].Capabilities = append(h.sessions[h.runtimeForTask(task)].Capabilities, protocol.SharedServiceLogsCapability)
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	child := activeNativeChild(h)
	if _, ok := logJob(child, "pair"); !ok {
		t.Fatal("shared assignment scope not captured")
	}
	if err := h.Collect(child.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.Collect(child.ID); err != nil {
		t.Fatal(err)
	}
	current := h.state.Runs[child.ID]
	if len(current.Logs) != 1 {
		t.Fatal("duplicate sender/receiver collection", current.Logs)
	}
	for _, log := range current.Logs {
		if log.Role != "pair" || log.Scope != protocol.ServiceLogScope {
			t.Fatal(log)
		}
	}
	finishNativeChild(t, h, current, "SUCCESS")
	if len(h.state.Runs[child.ID].Logs) != 1 {
		t.Fatal("completion duplicated a pending shared request")
	}
}

func TestExecutionJobsKeepInventoryOnlyAtCenter(t *testing.T) {
	h, task := nativeHubFixture(t)
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	h.advanceNative(time.Now().UTC())
	for _, run := range h.state.Runs {
		if run.NativeCase == nil {
			continue
		}
		if len(run.NativeCase.Candidates) == 0 || run.NativeCase.Candidates[0].AgentID == "" {
			t.Fatal("center lost the source identity")
		}
		for _, ep := range run.Endpoints {
			job := ep.Job
			if job.EdgeFormat != protocol.ProbeEdgeAddressFormat || job.NativeCase == nil {
				t.Fatal("missing compact execution format")
			}
			if job.RTC.NativeCoverage != nil || job.Target.RTC.NativeCoverage != nil {
				t.Fatal("inventory configuration leaked into job")
			}
			for _, edge := range append([]protocol.NativeEdge{job.NativeCase.Sender, job.NativeCase.Receiver}, job.NativeCase.Candidates...) {
				if edge.AgentID != "" || edge.ContainerName != "" || edge.ContainerID != "" || edge.SnapshotID != "" || edge.Stale {
					t.Fatal("identity leaked into execution Edge", edge)
				}
			}
		}
		return
	}
	t.Fatal("no child execution")
}

func TestTargetAPIngressComesFromDiscovery(t *testing.T) {
	h, task := nativeHubFixture(t)
	plan := h.planLocked()
	target := &plan.Targets[0]
	target.RTC.NativeCoverage.Groups[0].EntryPoints = []string{"203.0.113.55"}
	if err := h.UpdatePlan(planRevision(h.planLocked()), plan); err != nil {
		t.Fatal(err)
	}
	saved := h.target(task.TargetID).RTC.NativeCoverage.Groups[0]
	if saved.AgentID != "agent" || len(saved.EntryPoints) != 1 || saved.EntryPoints[0] != "111.230.192.59" {
		t.Fatal("client-supplied AP address overrode discovery", saved)
	}
	plan = h.planLocked()
	plan.Targets[0].RTC.NativeCoverage.Groups[0].AgentID = "unknown-ap"
	if err := h.UpdatePlan(planRevision(h.planLocked()), plan); err == nil {
		t.Fatal("non-AP machine accepted")
	}
}

func TestTransientBudgetDoesNotOverwriteVerifiedPhaseEvidence(t *testing.T) {
	h, task := nativeHubFixture(t)
	now := time.Now().UTC()
	h.config.Tasks[0].IntervalSeconds = 180
	h.config.Tasks[0].SweepIntervalSeconds = 300
	h.config.Tasks[0].CrossIntervalSeconds = 600
	h.config.Tasks[0].TimeoutSeconds = 90
	task, _ = h.task(task.ID)
	h.sessions[h.runtimeForTask(task)].Capacity = 8
	_, err := h.Start(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range h.state.Runs {
		if !terminal(run.Status) {
			run.Status = "CANCELLED"
			run.FinishedAt = &now
		}
	}
	state := h.state.Native[task.ID]
	for phase, p := range state.Phases {
		p.Health = h.newHealth(task, now, "MEDIA_VERIFIED")
		p.Health.State = "HEALTHY"
		p.Health.LastResultAt = &now
		p.Health.LastSuccessAt = &now
		p.Health.LastRunID = "verified-" + phase
		p.NextAt = now.Add(time.Hour)
	}
	// A temporary low capacity blocks scheduling but is not a new media result.
	h.sessions[h.runtimeForTask(task)].Capacity = 1
	h.tickNative(now)
	if h.state.Native[task.ID].SchedulingError == "" || h.nativeHealth(task, now).State != "UNKNOWN" {
		t.Fatal("insufficient budget was not visible")
	}
	for _, p := range h.state.Native[task.ID].Phases {
		if p.Health.State != "HEALTHY" || !p.Health.LastResultAt.Equal(now) {
			t.Fatal("budget error overwrote media evidence")
		}
	}
	h.sessions[h.runtimeForTask(task)].Capacity = 8
	h.tickNative(now.Add(time.Second))
	health := h.nativeHealth(task, now.Add(time.Second))
	if health.State != "HEALTHY" || !health.LastResultAt.Equal(now) {
		t.Fatal("recovered budget did not restore unmodified fresh evidence", health)
	}
	later := now.Add(4 * time.Hour)
	if h.nativeHealth(task, later).State != "UNKNOWN" {
		t.Fatal("budget recovery refreshed expired media evidence")
	}
}
