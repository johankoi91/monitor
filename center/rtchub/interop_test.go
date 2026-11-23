package hub

import (
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"strings"
	"testing"
	"time"
)

func TestInteropUsesTwoDuplexChannelsAndSplitClientBarrier(t *testing.T) {
	h, err := New(t.TempDir(), Config{MaxConcurrentRuns: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"native", "web"} {
		input := serverAnnouncement(client)
		if err = h.EnrollProbe(input, input.Host.Address); err != nil {
			t.Fatal(err)
		}
		session := h.Session(input.RuntimeID)
		hello := protocol.Msg("hello")
		hello.HostID = input.Host.ID
		hello.ServiceType = "rtc"
		hello.ClientType = client
		hello.Driver = input.Driver
		hello.Capacity = 2
		hello.Capabilities = []string{"native_web_duplex_v1", "native_web_interop_v1", "native_aut_web_interop_v1", protocol.ProbeEdgeAddressCapability, protocol.SharedServiceLogsCapability, "rtc_audio_v1", "rtc_video_v1", "native_routes_v1", protocol.WebMediaCapability}
		if _, err = h.Receive(input.RuntimeID, session, hello); err != nil {
			t.Fatal(err)
		}
	}
	config, inventory := nativeInventoryFixture()
	h.SetNativeInventoryProvider(func() (protocol.NativeInventory, error) { return inventory, nil })
	task := protocol.Task{ID: "interop", TargetID: "service", HostID: "server-one", ServiceType: "rtc", ClientType: protocol.InteropClient, Enabled: true, IntervalSeconds: 600, TimeoutSeconds: 90}
	target := protocol.Target{ID: "service", Name: "service", Driver: "agora", RTC: &protocol.RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", NativeCoverage: &config, WebAccess: &protocol.WebAccessConfiguration{Coverage: &config}}}
	rejected := task
	rejected.ClientType = protocol.InteropAUTClient
	if e := h.UpdatePlan(planRevision(h.planLocked()), Plan{Targets: []protocol.Target{target}, Tasks: []protocol.Task{rejected}}); e == nil || !strings.Contains(e.Error(), "RTC tasks") {
		t.Fatal("standalone AUT task remains available", e)
	}
	if err = h.UpdatePlan(planRevision(h.planLocked()), Plan{Targets: []protocol.Target{target}, Tasks: []protocol.Task{task}}); err != nil {
		t.Fatal(err)
	}
	{
		webTask := task
		webTask.ClientType = "web"
		peer := h.sessions[h.runtimeForTask(webTask)]
		caps := append([]string(nil), peer.Capabilities...)
		peer.Capabilities = nil
		for _, cap := range caps {
			if cap != protocol.InteropCapability(protocol.InteropAUTClient) {
				peer.Capabilities = append(peer.Capabilities, cap)
			}
		}
		if err := h.admitInterop(task, time.Now().UTC(), ""); err == nil || err.Error() != "PROBE_INTEROP_CAPABILITY_UNAVAILABLE" {
			t.Fatal("AUT task admitted an old Web client", err)
		}
		if value := h.effectiveHealth(task, time.Now().UTC()); value.Reason != "PROBE_CAPABILITY_MISMATCH" {
			t.Fatal("old Web client did not invalidate AUT health", value)
		}
		peer.Capabilities = caps
	}
	parent, err := h.Start(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Cases) != 2 || parent.NativePlan.Phase != "AP" || len(h.state.Native[task.ID].Phases) != 1 {
		t.Fatal("interop did not have exactly two duplex AP cases")
	}
	for _, row := range parent.Cases {
		native := row.Case.Sender
		if row.Case.ReceiverClient == "native" {
			native = row.Case.Receiver
		}
		if native.Transport != protocol.InteropNativeTransport(row.Case.ClientType) {
			t.Fatal("wrong Native transport", native)
		}
		if !row.Case.Duplex || row.Case.Validate() != nil || row.Case.Sender.Endpoint != "" || row.Case.Receiver.Endpoint != "" || row.Case.SenderClient == row.Case.ReceiverClient {
			t.Fatal("pinned or invalid interop route", row.Case)
		}
	}
	processed := map[string]bool{}
	for pass := 0; pass < 8; pass++ {
		h.advanceNative(time.Now().UTC().Add(6 * time.Second))
		for _, run := range h.state.Runs {
			if run.ParentRunID != parent.ID || processed[run.ID] {
				continue
			}
			processed[run.ID] = true
			left, right := run.Endpoints["sender"].Job, run.Endpoints["receiver"].Job
			if left.Channel != right.Channel || left.UID == right.UID || left.PublisherUID != right.UID || right.PublisherUID != left.UID {
				t.Fatal("duplex peers not bound to one channel and opposite UIDs")
			}
			if run.Endpoints["sender"].Job.RuntimeID == run.Endpoints["receiver"].Job.RuntimeID {
				t.Fatal("cross clients collapsed into a same-client pair")
			}
			for role, endpoint := range run.Endpoints {
				commands := h.Commands(endpoint.Job.RuntimeID, h.sessions[endpoint.Job.RuntimeID])
				found := false
				for _, command := range commands {
					if command.RunID == run.ID && command.Type == "prepare_run" {
						if command.Role != role || command.Job.Role != role || command.Job.Target.Driver != endpoint.Job.Target.Driver {
							t.Fatal("wrong endpoint assignment")
						}
						found = true
					}
				}
				if !found {
					t.Fatal("split endpoint not dispatched")
				}
			}
			a := protocol.Msg("run_ready")
			a.RunID = run.ID
			a.Revision = run.Revision
			a.Role = "sender"
			sender := run.Endpoints["sender"].Job.RuntimeID
			if _, err = h.Receive(sender, h.sessions[sender], a); err != nil {
				t.Fatal(err)
			}
			if h.state.Runs[run.ID].Status == "RUNNING" {
				t.Fatal("media started before peer ready")
			}
			a.Role = "receiver"
			receiver := run.Endpoints["receiver"].Job.RuntimeID
			if _, err = h.Receive(receiver, h.sessions[receiver], a); err != nil {
				t.Fatal(err)
			}
			if h.state.Runs[run.ID].Status != "RUNNING" {
				t.Fatal("both ready did not start media")
			}
			for role, ep := range run.Endpoints {
				evidence := protocol.Evidence{Connected: true, Published: true, VideoPublished: true, SentFrames: 200, SentVideoFrames: 50, Subscribed: true, VideoSubscribed: true, MediaDurationMS: 1500, ReceivedVideoFrames: 15, BrowserMediaValidated: true}
				if role == "sender" {
					evidence.Published = true
					evidence.VideoPublished = true
					evidence.SentFrames = 200
					evidence.SentVideoFrames = 50
				} else {
					evidence.Subscribed = true
					evidence.VideoSubscribed = true
					evidence.MediaDurationMS = 1500
					evidence.ReceivedVideoFrames = 15
					evidence.BrowserMediaValidated = true
				}
				result := protocol.Msg("run_result")
				result.RunID = run.ID
				result.Revision = run.Revision
				result.Role = role
				result.Result = &protocol.Result{ID: protocol.ID(), WorkerID: protocol.ID(), Driver: ep.Job.Target.Driver, Outcome: "SUCCESS", Cleanup: "SUCCESS", FinishedAt: time.Now(), TraceComplete: true, Evidence: evidence, Route: &protocol.RouteEvidence{Status: "VERIFIED", Guarded: ep.Job.ClientType == "native"}}
				if ep.Job.ClientType == "web" {
					result.Result.Browser = &protocol.BrowserEvidence{}
				}
				if _, err = h.Receive(ep.Job.RuntimeID, h.sessions[ep.Job.RuntimeID], result); err != nil {
					t.Fatal(err)
				}
			}
			if h.state.Runs[run.ID].Status != "SUCCESS" {
				t.Fatal("verified cross-client results rejected", h.state.Runs[run.ID].Reason)
			}
		}

	}
	h.advanceNative(time.Now().UTC().Add(6 * time.Second))
	if len(processed) != 2 || h.state.Runs[parent.ID].Status != "SUCCESS" {
		t.Fatal("two-path duplex parent did not finish", len(processed), h.state.Runs[parent.ID].Status)
	}

}

func TestMissingAUTRemainsVisibleInCombinedPlan(t *testing.T) {
	config, inventory := nativeInventoryFixture()
	task := protocol.Task{ClientType: protocol.InteropClient, RTC: &protocol.RTCParameters{NativeCoverage: &config, WebAccess: &protocol.WebAccessConfiguration{Coverage: &config}}}
	for group, edges := range inventory.Groups {
		kept := []protocol.NativeEdge{}
		for _, edge := range edges {
			if edge.Transport != "aut" {
				kept = append(kept, edge)
			}
		}
		inventory.Groups[group] = kept
	}
	plan := BuildInteropPlan(task, inventory)
	if len(plan.Cases) != 2 {
		t.Fatal("missing AUT paths disappeared")
	}
	for i, c := range plan.Cases {
		if i == 0 && c.Unavailable != "" {
			t.Fatal("missing AUT invalidated UDP path", c)
		}
		if i == 1 && c.Unavailable != "INTEROP_AUT_EDGE_CAPABILITY_UNAVAILABLE" {
			t.Fatal("AUT silently fell back to UDP", c)
		}
	}
}

func TestDuplexRequiresBothEndpointsSendAndDecodeThePeer(t *testing.T) {
	p := &protocol.RTCParameters{MinAudioMS: 1000, MinVideoFrames: 5}
	c := &protocol.NativeCase{Duplex: true}
	r := &Run{NativeCase: c, Endpoints: map[string]*Endpoint{
		"sender":   {Job: protocol.Job{Target: protocol.Target{Driver: "agora"}, RTC: p, Channel: "same-channel", UID: "10001", PublisherUID: "10002"}},
		"receiver": {Job: protocol.Job{Target: protocol.Target{Driver: protocol.WebDriver}, RTC: p, Channel: "same-channel", UID: "10002", PublisherUID: "10001"}},
	}}
	evidence := protocol.Evidence{Connected: true, Published: true, VideoPublished: true, Subscribed: true, VideoSubscribed: true, SentFrames: 100, SentVideoFrames: 15, MediaDurationMS: 1200, ReceivedVideoFrames: 12, BrowserMediaValidated: true}
	a := &protocol.Result{TraceComplete: true, Evidence: evidence, Route: &protocol.RouteEvidence{Status: "VERIFIED", Guarded: true}}
	b := &protocol.Result{TraceComplete: true, Evidence: evidence, Route: &protocol.RouteEvidence{Status: "VERIFIED"}, Browser: &protocol.BrowserEvidence{}}
	if !verifiedEvidence(r, a, b) {
		t.Fatal("valid duplex evidence rejected")
	}
	for _, role := range []string{"sender", "receiver"} {
		result := a
		if role == "receiver" {
			result = b
		}
		for _, field := range []string{"receive-audio", "receive-video", "publish-audio", "publish-video"} {
			saved := result.Evidence
			switch field {
			case "receive-audio":
				result.Evidence.MediaDurationMS = 0
			case "receive-video":
				result.Evidence.ReceivedVideoFrames = 0
			case "publish-audio":
				result.Evidence.Published = false
			case "publish-video":
				result.Evidence.VideoPublished = false
			}
			if verifiedEvidence(r, a, b) {
				t.Fatal("one-way evidence accepted as duplex", role, field)
			}
			result.Evidence = saved
		}
	}
	r.Endpoints["receiver"].Job.Channel = "other-channel"
	if verifiedEvidence(r, a, b) {
		t.Fatal("different channels accepted")
	}
	r.Endpoints["receiver"].Job.Channel = "same-channel"
	r.Endpoints["sender"].Job.PublisherUID = "10001"
	if verifiedEvidence(r, a, b) {
		t.Fatal("self media accepted as peer evidence")
	}
}
