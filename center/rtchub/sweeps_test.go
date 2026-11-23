package hub

import (
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"testing"
	"time"
)

func edgeFixture(t *testing.T) (*Hub, protocol.Task) {
	h, task := healthFixture(t)
	task.RTC.MediaEdges = &protocol.MediaEdgeRange{Host: "111.230.192.59", StartPort: 4001, EndPort: 4003}
	if err := task.RTC.Normalize(); err != nil {
		t.Fatal(err)
	}
	h.config.Tasks[0] = task
	h.config.Targets[0].RTC = cloneRTC(task.RTC)
	h.sessions[h.runtimeForTask(task)].Capabilities = []string{"rtc_audio_v1", "rtc_video_v1", "rtc_vos_list_v1"}
	return h, task
}
func currentSweepChild(t *testing.T, h *Hub, parentID string) *Run {
	t.Helper()
	for _, r := range h.state.Runs {
		if r.ParentRunID == parentID && !terminal(r.Status) {
			return r
		}
	}
	t.Fatal("sweep has no active child")
	return nil
}
func finishEdge(t *testing.T, h *Hub, child *Run, outcome string) {
	t.Helper()
	worker := protocol.ID()
	for _, role := range []string{"sender", "receiver"} {
		e := protocol.Evidence{Connected: true}
		if role == "sender" {
			e.Published = true
			e.VideoPublished = true
			e.SentFrames = 500
			e.SentVideoFrames = 75
		} else {
			e.Subscribed = true
			e.VideoSubscribed = true
			e.MediaDurationMS = 1000
			e.ValidSamples = 16000
			e.ReceivedVideoFrames = 5
		}
		if outcome == "FAILED" && role == "receiver" {
			e.ReceivedVideoFrames = 0
		}
		m := protocol.Msg("run_result")
		m.RunID = child.ID
		m.Revision = child.Revision
		m.Role = role
		m.Result = &protocol.Result{ID: protocol.ID(), WorkerID: worker, Driver: "agora", Outcome: outcome, Reason: map[string]string{"SUCCESS": "MEDIA_VERIFIED", "FAILED": "VIDEO_RECEIVE_TIMEOUT", "CANCELLED": "CANCELLED"}[outcome], Evidence: e, Cleanup: "SUCCESS", TraceComplete: true, FinishedAt: time.Now().UTC()}
		id := child.Endpoints[role].Job.RuntimeID
		if _, err := receiveTest(h, id, h.sessions[id], m); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSweepRunsEveryPortAndCannotHideEarlierFailure(t *testing.T) {
	h, task := edgeFixture(t)
	parent, err := h.Start(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Kind != edgeSweepKind || len(parent.Edges) != 3 {
		t.Fatal("missing sweep execution")
	}
	if _, err = h.Start(task.ID); err == nil {
		t.Fatal("sweep re-entered")
	}
	statuses := []string{"SUCCESS", "FAILED", "SUCCESS"}
	for index, status := range statuses {
		child := currentSweepChild(t, h, parent.ID)
		if child.EdgeEndpoint != task.RTC.MediaEdges.Endpoints()[index] {
			t.Fatal("ports out of order", child.EdgeEndpoint)
		}
		commands := h.Commands(h.runtimeForTask(task), h.sessions[h.runtimeForTask(task)])
		prepared := 0
		for _, m := range commands {
			if m.Type == "prepare_run" {
				prepared++
				if m.Job.EdgeEndpoint != child.EdgeEndpoint || m.Job.ParentRunID != parent.ID {
					t.Fatal("selected endpoint missing from worker assignment")
				}
			}
		}
		if prepared != 1 {
			t.Fatal("sweep parent dispatched as a fake SDK worker", prepared)
		}
		finishEdge(t, h, child, status)
		if index < 2 {
			if terminal(h.state.Runs[parent.ID].Status) {
				t.Fatal("sweep ended before checking all ports")
			}
			if v := h.state.Health[task.ID]; v.LastRunID == child.ID {
				t.Fatal("single port replaced whole-task health")
			}
			h.advanceSweeps(time.Now().UTC())
		}
	}
	final := h.state.Runs[parent.ID]
	if final.Status != "FAILED" || final.Reason != "MEDIA_EDGE_FAILED" {
		t.Fatal("later port success hid failure", final.Status)
	}
	if v := h.state.Health[task.ID]; v.LastRunID != parent.ID || v.FailureCount != 1 {
		t.Fatal("threshold counted individual ports", v)
	}
	history, err := h.History(task.ID, 20, 0)
	if err != nil || history.Total != 1 {
		t.Fatal("child executions leaked into public history", history.Total, err)
	}
	raw, err := h.Detail(final.Edges[0].RunID)
	if err != nil {
		t.Fatal("port detail missing", err)
	}
	var archived Run
	json.Unmarshal(raw, &archived)
	if archived.EdgeEndpoint != final.Edges[0].Endpoint {
		t.Fatal("archived endpoint lost")
	}
}
func TestSweepAllPortsRequiredAndCancellationStopsPendingPorts(t *testing.T) {
	h, task := edgeFixture(t)
	parent, err := h.Start(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		child := currentSweepChild(t, h, parent.ID)
		finishEdge(t, h, child, "SUCCESS")
		if i < 2 {
			h.advanceSweeps(time.Now().UTC())
		}
	}
	if h.state.Runs[parent.ID].Status != "SUCCESS" || h.state.Health[task.ID].State != "HEALTHY" {
		t.Fatal("complete successful range did not establish healthy baseline")
	}
	next, err := h.Start(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	child := currentSweepChild(t, h, next.ID)
	if err = h.Cancel(next.ID); err != nil {
		t.Fatal(err)
	}
	for _, edge := range h.state.Runs[next.ID].Edges {
		if edge.Status != "CANCELLED" {
			t.Fatal("cancelled sweep still has pending ports")
		}
	}
	if h.state.Runs[child.ID].Status != "CANCELLED" {
		t.Fatal("active SDK child not cancelled")
	}
	h.advanceSweeps(time.Now().UTC())
	count := 0
	for _, r := range h.state.Runs {
		if r.ParentRunID == next.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatal("new ports started after cancellation")
	}
}

func TestSweepRestartAndProbeDisableNeverResumeOldPorts(t *testing.T) {
	for _, mode := range []string{"restart", "disable"} {
		t.Run(mode, func(t *testing.T) {
			h, task := edgeFixture(t)
			parent, err := h.Start(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			first := currentSweepChild(t, h, parent.ID)
			finishEdge(t, h, first, "SUCCESS")
			h.advanceSweeps(time.Now().UTC())
			if mode == "restart" {
				h, err = newTestHub(h.dir, h.config, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err = h.EnableProbe(h.runtimeForTask(task), false); err != nil {
					t.Fatal(err)
				}
			}
			final := h.state.Runs[parent.ID]
			if !terminal(final.Status) {
				t.Fatal("interrupted sweep remained active")
			}
			for _, edge := range final.Edges {
				if edge.Status == "PENDING" || edge.Status == "RUNNING" || edge.Status == "PREPARING" {
					t.Fatal("interrupted ports look active", edge)
				}
			}
			h.advanceSweeps(time.Now().UTC())
			if final.Edges[2].RunID != "" {
				t.Fatal("old sweep resumed after interruption")
			}
		})
	}
}
