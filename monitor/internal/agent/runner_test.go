package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/docker"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

type fakeExecutor struct {
	mu     sync.Mutex
	target docker.Target
	calls  int
	phase  string
	err    error
}

func (e *fakeExecutor) InspectTarget(context.Context, string) (docker.Target, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.target, nil
}
func (e *fakeExecutor) RestartOriginal(context.Context, string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.target.StartedAt = "after"
	return e.phase, e.err
}
func fixtureTask() model.Task {
	return model.Task{Type: "task", Action: "PREPARE", OperationID: "operation-1", AgentID: "a", NodeID: "a/ap", ServiceCode: "rtc-ap", ContainerID: "container", ContainerName: "ap", Image: "rtc/ap:1", SnapshotID: "snapshot", Deadline: time.Now().Add(time.Minute)}
}
func fixtureRule() []model.RestartRule {
	return []model.RestartRule{{AgentID: "a", ContainerName: "ap", ServiceCode: "rtc-ap", Image: "rtc/ap:1", Approved: true, ApprovalRef: "isolated test approval"}}
}
func fixtureExecutor() *fakeExecutor {
	return &fakeExecutor{target: docker.Target{ID: "container", Name: "ap", Image: "rtc/ap:1", SnapshotID: "snapshot", Runtime: "running", StartedAt: "before"}, phase: "EXECUTED"}
}
func awaitPhase(t *testing.T, r *Runner, task model.Task, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if r.Query(task).Phase == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %s, want %s", r.Query(task).Phase, want)
}
func TestExactlyOneAttemptWithDuplicateFramesAndRecovery(t *testing.T) {
	dir := t.TempDir()
	e := fixtureExecutor()
	r, err := New(dir, "a", fixtureRule(), e)
	if err != nil {
		t.Fatal(err)
	}
	task := fixtureTask()
	r.Handle(context.Background(), task)
	if e.calls != 0 || r.Query(task).Phase != "PREPARED" {
		t.Fatal("prepare executed Docker")
	}
	task.Action = "EXECUTE"
	for i := 0; i < 10; i++ {
		r.Handle(context.Background(), task)
	}
	awaitPhase(t, r, task, "EXECUTED")
	r.Ack(task.OperationID)
	r.Close()
	r, err = New(dir, "a", fixtureRule(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.Handle(context.Background(), task)
	e.mu.Lock()
	count := e.calls
	e.mu.Unlock()
	if count != 1 {
		t.Fatalf("executed %d times", count)
	}
}
func TestCrashDuringExecutingIsUncertainAndNotReplayed(t *testing.T) {
	dir := t.TempDir()
	e := fixtureExecutor()
	r, err := New(dir, "a", fixtureRule(), e)
	if err != nil {
		t.Fatal(err)
	}
	task := fixtureTask()
	r.Handle(context.Background(), task)
	result := r.Query(task)
	result.Phase = "EXECUTING"
	result.ExecutionPossible = true
	r.mu.Lock()
	err = r.persist(task, result, 0)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = New(dir, "a", fixtureRule(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	task.Action = "EXECUTE"
	r.Handle(context.Background(), task)
	if r.Query(task).Phase != "UNCERTAIN" || e.calls != 0 {
		t.Fatal("crash execution was replayed")
	}
}
func TestMissingApprovalChangedIdentityAndExpiredTaskNeverExecute(t *testing.T) {
	for _, kind := range []string{"approval", "identity", "expired"} {
		t.Run(kind, func(t *testing.T) {
			e := fixtureExecutor()
			rules := fixtureRule()
			task := fixtureTask()
			if kind == "approval" {
				rules = nil
			}
			if kind == "identity" {
				e.target.SnapshotID = "changed"
			}
			if kind == "expired" {
				task.Deadline = time.Now().Add(-time.Second)
			}
			r, err := New(t.TempDir(), "a", rules, e)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			r.Handle(context.Background(), task)
			task.Action = "EXECUTE"
			r.Handle(context.Background(), task)
			if e.calls != 0 || r.Query(task).Phase != "REJECTED" {
				t.Fatal("unapproved target executed")
			}
		})
	}
}
func TestDockerTimeoutIsNotRetried(t *testing.T) {
	e := fixtureExecutor()
	e.phase = "UNCERTAIN"
	e.err = errors.New("unknown execution")
	r, err := New(t.TempDir(), "a", fixtureRule(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	task := fixtureTask()
	r.Handle(context.Background(), task)
	task.Action = "EXECUTE"
	r.Handle(context.Background(), task)
	awaitPhase(t, r, task, "UNCERTAIN")
	r.Handle(context.Background(), task)
	if e.calls != 1 {
		t.Fatal("Docker timeout was retried")
	}
}
