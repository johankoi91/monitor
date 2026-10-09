package center

import (
	"errors"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func operational(t *testing.T) (*Store, string, model.Baseline) {
	t.Helper()
	s := setup(t)
	s.config.RestartRules = []model.RestartRule{{AgentID: "rtc-a", ContainerName: "ap", ServiceCode: "rtc-ap", Image: "rtc/ap:1", Approved: true, ApprovalRef: "isolated test"}}
	session := connect(t, s, "rtc-a", "ap")
	b, err := s.Save(selection("", "rtc-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyRestartRules(); err != nil {
		t.Fatal(err)
	}
	b = s.Baseline()
	return s, session, b
}
func request() model.RestartRequest {
	return model.RestartRequest{NodeID: "rtc-a/ap", Operator: "test declaration", Reason: "isolated restart validation", RequestKey: "request-1"}
}
func prepared(o model.Operation) model.TaskResult {
	return model.TaskResult{Type: "task_result", OperationID: o.OperationID, LedgerID: "ledger-a", Phase: "PREPARED", ContainerID: o.Task.ContainerID, SnapshotID: o.Task.SnapshotID, BeforeStartedAt: "before"}
}
func TestRestartIdempotencyAndBaselineMutex(t *testing.T) {
	s, _, b := operational(t)
	o, err := s.CreateRestart(request(), "verified-source", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.CreateRestart(request(), "verified-source", "127.0.0.1")
	if err != nil || same.OperationID != o.OperationID {
		t.Fatal("idempotency failed")
	}
	changed := request()
	changed.Reason = "different reason"
	_, err = s.CreateRestart(changed, "source", "")
	code(t, err, "IDEMPOTENCY_CONFLICT")
	changed = request()
	changed.RequestKey = "another"
	_, err = s.CreateRestart(changed, "source", "")
	code(t, err, "NODE_NOT_RESTARTABLE")
	_, err = s.Save(model.Selection{ExpectedRevision: b.Revision, Additions: []model.Addition{}, Removals: []string{"rtc-a/ap"}}, "admin")
	code(t, err, "OPERATION_IN_PROGRESS")
}
func TestRecoveryQueriesBeforeAnyExecute(t *testing.T) {
	s, session, _ := operational(t)
	o, err := s.CreateRestart(request(), "source", "")
	if err != nil {
		t.Fatal(err)
	}
	r := prepared(o)
	if err = s.ReceiveResult("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	dir, cfg := s.dir, s.config
	s.Close()
	restored, err := NewStore(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	session = connect(t, restored, "rtc-a", "ap")
	tasks, _ := restored.Commands("rtc-a", session)
	if len(tasks) != 1 || tasks[0].Action != "QUERY" {
		t.Fatal("recovery blindly sent EXECUTE")
	}
	if err = restored.ReceiveResult("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	tasks, _ = restored.Commands("rtc-a", session)
	if tasks[0].Action != "EXECUTE" {
		t.Fatal("confirmed prepared task didn't resume")
	}
	tasks, _ = restored.Commands("rtc-a", session)
	if tasks[0].Action != "QUERY" {
		t.Fatal("execute wasn't followed by reconciliation")
	}
	same, err := restored.CreateRestart(request(), "source", "")
	if err != nil || same.OperationID != o.OperationID {
		t.Fatal("recovered key lost")
	}
}
func TestExecutionEvidenceVerifiedAgainstFreshSnapshot(t *testing.T) {
	s, session, _ := operational(t)
	o, err := s.CreateRestart(request(), "source", "")
	if err != nil {
		t.Fatal(err)
	}
	r := prepared(o)
	if err = s.ReceiveResult("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	started, finished := time.Now().Add(-time.Second), time.Now()
	r.Phase = "EXECUTED"
	r.ExecutionPossible = true
	r.StartedAt = &started
	r.FinishedAt = &finished
	r.AfterStartedAt = "after"
	if err = s.ReceiveResult("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	s.TickOperations(time.Now())
	item, _ := s.Operation(o.OperationID)
	if item.Status == "SUCCESS" {
		t.Fatal("old snapshot falsely passed verification")
	}
	freshReport := report("ap")
	freshReport.Sequence = 2
	freshReport.Containers[0].StartedAt = "after"
	if err = s.Receive("rtc-a", session, freshReport); err != nil {
		t.Fatal(err)
	}
	s.TickOperations(time.Now())
	item, _ = s.Operation(o.OperationID)
	if item.Status != "SUCCESS" || item.NodeLocked || !item.ResultKnown {
		t.Fatalf("verification: %+v", item)
	}
	records, err := s.Audit(item.OperationID)
	if err != nil || len(records) < 4 {
		t.Fatal("audit stages missing")
	}
}
func TestUnknownResultsRemainLockedUntilHumanEvidence(t *testing.T) {
	s, session, _ := operational(t)
	o, err := s.CreateRestart(request(), "source", "")
	if err != nil {
		t.Fatal(err)
	}
	r := prepared(o)
	s.ReceiveResult("rtc-a", session, r)
	s.TickOperations(o.Task.Deadline.Add(time.Second))
	item, _ := s.Operation(o.OperationID)
	if item.ResultKnown || !item.NodeLocked || item.Phase != "UNCERTAIN" {
		t.Fatal("unknown result unlocked node")
	}
	tasks, _ := s.Commands("rtc-a", session)
	if tasks[0].Action != "QUERY" {
		t.Fatal("unknown result reissued restart")
	}
	_, err = s.Resolve(o.OperationID, model.ResolveRequest{}, "source", "")
	code(t, err, "INVALID_RESOLUTION")
	item, err = s.Resolve(o.OperationID, model.ResolveRequest{Operator: "onsite verifier", Reason: "confirmed ledger and Docker", Outcome: "NOT_EXECUTED", Evidence: "checked original container identity and complete on-site operation evidence"}, "verified-source", "127.0.0.1")
	if err != nil || item.NodeLocked || item.Status == "SUCCESS" {
		t.Fatal("manual resolution falsely reported health success")
	}
}
func TestStoreFailurePreventsDispatch(t *testing.T) {
	s, session, _ := operational(t)
	s.opWrite = func(model.AuditRecord, int64) error { return errors.New("disk unavailable") }
	_, err := s.CreateRestart(request(), "source", "")
	code(t, err, "OPERATION_STORE_UNAVAILABLE")
	tasks, _ := s.Commands("rtc-a", session)
	if len(tasks) != 0 || s.Ready() {
		t.Fatal("failed storage allowed restart")
	}
}
func TestRestartCountWindowDoesNotTreatOldCountAsFailure(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	s.Save(selection("", "rtc-a"), "source")
	r := report("ap")
	r.Sequence = 2
	r.Containers[0].RestartCount = 10
	s.Receive("rtc-a", session, r)
	for i := 3; i < 6; i++ {
		r = report("ap")
		r.Sequence = uint64(i)
		r.Containers[0].RestartCount = 10 + i - 2
		s.Receive("rtc-a", session, r)
	}
	view := s.Statuses("", "").(map[string]any)
	service := view["services"].([]map[string]any)[0]
	if service["status"] != "UNHEALTHY" {
		t.Fatal("running restart storm ignored")
	}
}

func TestContainerExitResetsReadinessRecovery(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	selection := selection("", "rtc-a")
	check := model.Check{Type: "tcp", Host: "127.0.0.1", Port: 443, TimeoutMS: 2000}
	selection.Additions[0].Checks = []model.Check{check}
	b, err := s.Save(selection, "source")
	if err != nil {
		t.Fatal(err)
	}
	seq := uint64(1)
	pass := func(runtime string, ok bool) {
		seq++
		r := report("ap")
		r.Sequence = seq
		r.BaselineRevision = b.Revision
		r.Containers[0].Container.RuntimeStatus = runtime
		r.Containers[0].Checks = []model.CheckResult{{Check: check, OK: ok, CollectedAt: r.CollectedAt}}
		if err := s.Receive("rtc-a", session, r); err != nil {
			t.Fatal(err)
		}
	}
	pass("running", true)
	pass("running", true)
	pass("exited", false)
	pass("running", true)
	view := s.Statuses("", "").(map[string]any)
	if view["services"].([]map[string]any)[0]["status"] == "HEALTHY" {
		t.Fatal("container recovery reused pre-exit readiness")
	}
	pass("running", true)
	view = s.Statuses("", "").(map[string]any)
	if view["services"].([]map[string]any)[0]["status"] != "HEALTHY" {
		t.Fatal("two new successes did not recover")
	}
}
