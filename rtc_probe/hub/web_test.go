package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"testing"
	"time"
)

func webFixture(t *testing.T) (*Hub, protocol.Task) {
	h := targetPlanFixture(t)
	cfg, inventory := nativeInventoryFixture()
	h.SetNativeInventoryProvider(func() (protocol.NativeInventory, error) { return inventory, nil })
	session := h.sessions["probe-web"]
	hello := protocol.Msg("hello")
	hello.Driver = protocol.WebDriver
	hello.Capacity = 2
	hello.Capabilities = []string{protocol.ProbeEdgeAddressCapability, protocol.WebMediaCapability, protocol.SharedServiceLogsCapability}
	if _, err := receiveTest(h, "probe-web", session, hello); err != nil {
		t.Fatal(err)
	}
	task := targetTask("web-task", "service")
	task.ClientType = "web"
	task.HostID = "host-probe-web"
	task.IntervalSeconds = 600
	task.TimeoutSeconds = 60
	rtc := &protocol.RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", NativeCoverage: &cfg, WebAccess: &protocol.WebAccessConfiguration{IgnoreCertificates: true, Coverage: &cfg}}
	if err := h.UpdatePlan(planRevision(h.planLocked()), Plan{Targets: []protocol.Target{{ID: "service", Name: "one RTC service", Driver: "agora", RTC: rtc}}, Tasks: []protocol.Task{task}}); err != nil {
		t.Fatal(err)
	}
	task, _ = h.task(task.ID)
	return h, task
}
func TestWebPlanSelectsWebOnlyAndUsesDynamicRing(t *testing.T) {
	cfg, inventory := nativeInventoryFixture()
	for phase, want := range map[string]int{"AP": 1, "DIRECT": 3, "CROSS": 3} {
		plan := BuildWebPlan(cfg, inventory, nil, phase, 0)
		if len(plan.Members) != 3 || len(plan.Cases) != want {
			t.Fatal(phase, len(plan.Members), len(plan.Cases))
		}
		for _, c := range plan.Cases {
			if c.ClientType != "web" || c.Sender.Transport != "web" || c.Receiver.Transport != "web" || c.Validate() != nil {
				t.Fatal(c, c.Validate())
			}
		}
	}
}
func TestWebUsesSharedTargetAndContributesRealHealth(t *testing.T) {
	h, task := webFixture(t)
	if task.RTC.NativeCoverage != nil || task.RTC.MediaEdges != nil || task.RTC.WebAccess.Coverage == nil || task.RTC.Codec != 1 {
		t.Fatal("Web runtime inherited Native routes", task.RTC)
	}
	if h.target(task.TargetID).RTC.NativeCoverage == nil {
		t.Fatal("shared target Native configuration lost")
	}
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	count := 0
	for step := 0; step < 80; step++ {
		children := []*Run{}
		for _, r := range h.state.Runs {
			if r.NativeCase != nil && !terminal(r.Status) {
				children = append(children, r)
			}
		}
		for _, child := range children {
			if child.Driver != protocol.WebDriver || child.Revision != revision(task, h.target(task.TargetID)) {
				t.Fatal("wrong Web revision/driver", child.Driver)
			}
			for _, role := range []string{"sender", "receiver"} {
				telemetry := protocol.Msg("telemetry_batch")
				telemetry.RunID = child.ID
				telemetry.Revision = child.Revision
				telemetry.Role = role
				telemetry.Events = []protocol.Event{{ID: protocol.ID(), WorkerID: "browser-worker", Seq: 1, At: time.Now(), RunID: child.ID, ParentRunID: child.ParentRunID, EdgeEndpoint: child.EdgeEndpoint, Role: role, RuntimeID: h.runtimeForTask(task), Revision: child.Revision, Kind: "api_call_begin", Name: "join"}}
				if _, err := receiveTest(h, h.runtimeForTask(task), h.sessions[h.runtimeForTask(task)], telemetry); err != nil {
					t.Fatal(err)
				}
				evidence := protocol.Evidence{Connected: true}
				if role == "sender" {
					evidence.Published = true
					evidence.VideoPublished = true
					evidence.SentFrames = 100
					evidence.SentVideoFrames = 50
				} else {
					evidence.Subscribed = true
					evidence.VideoSubscribed = true
					evidence.ValidSamples = 96000
					evidence.MediaDurationMS = 2000
					evidence.ReceivedVideoFrames = 20
					evidence.BrowserMediaValidated = true
				}
				m := protocol.Msg("run_result")
				m.RunID = child.ID
				m.Revision = child.Revision
				m.Role = role
				m.Result = &protocol.Result{ID: protocol.ID(), WorkerID: "browser-worker", Driver: protocol.WebDriver, Outcome: "SUCCESS", Cleanup: "SUCCESS", Evidence: evidence, TraceComplete: true, FinishedAt: time.Now(), Route: &protocol.RouteEvidence{Status: "VERIFIED", Scope: "web_wss_signaling"}, Browser: &protocol.BrowserEvidence{SDKVersion: "4.24.9", BrowserVersion: "141"}}
				if _, err := receiveTest(h, h.runtimeForTask(task), h.sessions[h.runtimeForTask(task)], m); err != nil {
					t.Fatal(err)
				}
			}
			count++
			if len(h.state.Runs[child.ID].Logs) != 1 {
				t.Fatal("Web diagnostics split into roles")
			}
		}
		h.advanceNative(time.Now().Add(6 * time.Second))
		if h.nativeHealth(task, time.Now()).State == "HEALTHY" {
			break
		}
	}
	if count != 7 || h.nativeHealth(task, time.Now()).State != "HEALTHY" {
		t.Fatal("Web coverage did not establish health", count)
	}
	if _, status, _ := h.healthLocked(); status != "HEALTHY" {
		t.Fatal("Web did not contribute service health", status)
	}
}
func TestWebMediaCannotPassWithSubscriptionOrOnlyOneMedia(t *testing.T) {
	r := &Run{Driver: protocol.WebDriver, Endpoints: map[string]*Endpoint{"receiver": {Job: protocol.Job{Target: protocol.Target{Driver: protocol.WebDriver}}}, "sender": {Job: protocol.Job{Target: protocol.Target{Driver: protocol.WebDriver}, RTC: &protocol.RTCParameters{Template: "audio_video", MinAudioMS: 1000, MinVideoFrames: 5}}}}}
	a := &protocol.Result{Evidence: protocol.Evidence{Connected: true, Published: true, VideoPublished: true, SentFrames: 1, SentVideoFrames: 1}, TraceComplete: true, Browser: &protocol.BrowserEvidence{}}
	b := &protocol.Result{Evidence: protocol.Evidence{Connected: true, Subscribed: true, VideoSubscribed: true, MediaDurationMS: 2000, ReceivedVideoFrames: 20}, TraceComplete: true, Browser: &protocol.BrowserEvidence{}}
	if verifiedEvidence(r, a, b) {
		t.Fatal("unverified decoder accepted")
	}
	b.Evidence.BrowserMediaValidated = true
	b.Evidence.ReceivedVideoFrames = 0
	if verifiedEvidence(r, a, b) {
		t.Fatal("audio only accepted")
	}
	b.Evidence.ReceivedVideoFrames = 20
	b.Evidence.MediaDurationMS = 0
	if verifiedEvidence(r, a, b) {
		t.Fatal("video only accepted")
	}
}
