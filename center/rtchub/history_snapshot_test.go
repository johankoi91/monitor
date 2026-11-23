package hub

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func TestHistoryContextOmitsExecutionEvidenceWithoutChangingDetail(t *testing.T) {
	h, err := newTestHub(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.state.Runs["large-run"] = &Run{ID: "large-run", Status: "SUCCESS", Endpoints: map[string]*Endpoint{
		"sender": {Events: []protocol.Event{{Name: strings.Repeat("sdk-event", 40000)}}},
	}}
	h.state.Native = map[string]*NativeState{"audio": {Version: "coverage-evidence"}}
	response := httptest.NewRecorder()
	h.ServeAdmin(response, httptest.NewRequest("GET", "/api/v1/rtc/status?view=history", nil))
	var context struct {
		Tasks   []protocol.Task   `json:"tasks"`
		Targets []protocol.Target `json:"targets"`
		Runs    []Run             `json:"runs"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &context) != nil || len(context.Tasks) != 1 || len(context.Targets) != 1 || len(context.Runs) != 0 {
		t.Fatal("history context lost configuration or included executions", response.Code)
	}
	if response.Body.Len() > 16<<10 || bytes.Contains(response.Body.Bytes(), []byte("coverage-evidence")) || bytes.Contains(response.Body.Bytes(), []byte("sdk-event")) {
		t.Fatal("history context included media coverage or SDK events")
	}
	detail, err := h.Detail("large-run")
	if err != nil || !bytes.Contains(detail, []byte("sdk-event")) {
		t.Fatal("lightweight history context discarded execution detail", err)
	}
}

func TestRunDetailViewKeepsMediaAndPreservesStoredSDKEvents(t *testing.T) {
	h, err := newTestHub(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "sdk-evidence", Status: "SUCCESS", Endpoints: map[string]*Endpoint{
		"receiver": {
			Events: []protocol.Event{{Name: strings.Repeat("OnRenderVideoFrame", 20000)}},
			Result: &protocol.Result{Outcome: "SUCCESS", Cleanup: "SUCCESS", Evidence: protocol.Evidence{MediaDurationMS: 4640, ReceivedVideoFrames: 73}},
		},
	}}
	if err = h.archive.RTCPutRun(run, 4<<20); err != nil {
		t.Fatal(err)
	}
	for _, cached := range []bool{false, true} {
		if cached {
			h.state.Runs[run.ID] = &run
		}
		response := httptest.NewRecorder()
		h.ServeAdmin(response, httptest.NewRequest("GET", "/api/v1/rtc/runs/sdk-evidence?view=detail", nil))
		var view Run
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Endpoints["receiver"] == nil || view.Endpoints["receiver"].Result == nil {
			t.Fatal("detail view lost execution result", response.Code)
		}
		endpoint := view.Endpoints["receiver"]
		if len(endpoint.Events) != 0 || endpoint.Result.Evidence.MediaDurationMS != 4640 || endpoint.Result.Evidence.ReceivedVideoFrames != 73 || endpoint.Result.Cleanup != "SUCCESS" || response.Body.Len() > 8<<10 {
			t.Fatal("detail view included SDK events or changed media evidence")
		}
		full, err := h.Detail(run.ID)
		if err != nil || !bytes.Contains(full, []byte("OnRenderVideoFrame")) {
			t.Fatal("compact response mutated original SDK events", err)
		}
	}
}

func TestTaskSnapshotKeepsActivityAndCoverageWithoutExecutionPayloads(t *testing.T) {
	h, err := newTestHub(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.state.Runs["round"] = &Run{ID: "round", TaskID: "audio", Kind: nativeCoverageKind, Status: "RUNNING"}
	h.state.Runs["heavy-child"] = &Run{ID: "heavy-child", TaskID: "audio", ParentRunID: "round", Status: "RUNNING", Endpoints: map[string]*Endpoint{
		"receiver": {Events: []protocol.Event{{Name: strings.Repeat("sdk-frame", 100000)}}},
	}}
	h.state.Native = map[string]*NativeState{"audio": {Version: "coverage-version", Phases: map[string]*NativePhaseState{"AP": {CaseCount: 2, ActiveRunID: "round"}}}}
	response := httptest.NewRecorder()
	h.ServeAdmin(response, httptest.NewRequest("GET", "/api/v1/rtc/status?view=tasks", nil))
	var view struct {
		Tasks    []protocol.Task         `json:"tasks"`
		Runs     []Run                   `json:"runs"`
		Activity map[string]taskActivity `json:"task_activity"`
		Coverage map[string]*NativeState `json:"native_coverage"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || len(view.Tasks) != 1 || len(view.Runs) != 0 || !view.Activity["audio"].Running || view.Activity["audio"].ActiveRunID != "round" || view.Coverage["audio"].Phases["AP"].CaseCount != 2 {
		t.Fatal("task snapshot lost activity or coverage")
	}
	if response.Body.Len() > 32<<10 || bytes.Contains(response.Body.Bytes(), []byte("sdk-frame")) {
		t.Fatal("task snapshot serialized execution evidence")
	}
	full, err := h.Detail("heavy-child")
	if err != nil || !bytes.Contains(full, []byte("sdk-frame")) {
		t.Fatal("task projection changed original execution", err)
	}
}
