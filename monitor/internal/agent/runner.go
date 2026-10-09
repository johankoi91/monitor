package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/docker"
	"github.com/johankoi91/monitor/runtime/internal/journal"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"gopkg.in/yaml.v3"
)

type Executor interface {
	InspectTarget(context.Context, string) (docker.Target, error)
	RestartOriginal(context.Context, string) (string, error)
}
type taskRecord struct {
	LedgerID string            `json:"ledger_id"`
	Task     *model.Task       `json:"task,omitempty"`
	Result   *model.TaskResult `json:"result,omitempty"`
	Ack      string            `json:"ack,omitempty"`
}
type Runner struct {
	mu                sync.Mutex
	ledgerID, agentID string
	journal           *journal.Journal
	executor          Executor
	rules             []model.RestartRule
	tasks             map[string]taskRecord
	active            map[string]bool
	acked             map[string]bool
	wg                sync.WaitGroup
}

func ReadRules(path string) ([]model.RestartRule, error) {
	if path == "" {
		return []model.RestartRule{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	dec.KnownFields(true)
	var rules []model.RestartRule
	if dec.Decode(&rules) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid Agent restart rules")
	}
	for _, rule := range rules {
		if rule.Approved && (rule.AgentID == "" || rule.ContainerName == "" || rule.ServiceCode == "" || rule.Image == "" || strings.TrimSpace(rule.ApprovalRef) == "") {
			return nil, errors.New("approved restart requires an exact binding and approval reference")
		}
	}
	return rules, nil
}
func New(dir, id string, rules []model.RestartRule, executor Executor) (*Runner, error) {
	r := &Runner{agentID: id, rules: rules, executor: executor, tasks: map[string]taskRecord{}, active: map[string]bool{}, acked: map[string]bool{}}
	j, err := journal.Open(filepath.Join(dir, "tasks.jsonl"), 16<<20, func(line json.RawMessage) error {
		var record taskRecord
		if json.Unmarshal(line, &record) != nil || record.LedgerID == "" {
			return errors.New("invalid Agent task ledger")
		}
		if r.ledgerID != "" && r.ledgerID != record.LedgerID {
			return errors.New("Agent ledger identities disagree")
		}
		r.ledgerID = record.LedgerID
		if record.Ack != "" {
			r.acked[record.Ack] = true
		}
		if record.Task != nil && record.Result != nil {
			if record.Task.OperationID != record.Result.OperationID {
				return errors.New("task/result identity mismatch")
			}
			r.tasks[record.Task.OperationID] = record
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.journal = j
	if r.ledgerID == "" {
		var data [16]byte
		if _, err = rand.Read(data[:]); err != nil {
			j.Close()
			return nil, err
		}
		r.ledgerID = hex.EncodeToString(data[:])
		if err = j.Append(taskRecord{LedgerID: r.ledgerID}, 0); err != nil {
			j.Close()
			return nil, err
		}
	}
	// EXECUTING after a process crash is never converted to PREPARED or replayed.
	for id, record := range r.tasks {
		if record.Result.Phase == "EXECUTING" {
			record.Result.Phase = "UNCERTAIN"
			record.Result.ExecutionPossible = true
			record.Result.Message = "Agent restarted during execution; original task will not run again"
			if err = j.Append(record, 0); err != nil {
				j.Close()
				return nil, err
			}
			r.tasks[id] = record
		}
	}
	return r, nil
}
func (r *Runner) Close() { r.wg.Wait(); r.journal.Close() }
func (r *Runner) authorized(t model.Task) bool {
	for _, rule := range r.rules {
		if rule.Approved && rule.ApprovalRef != "" && rule.AgentID == r.agentID && rule.AgentID == t.AgentID && rule.ContainerName == t.ContainerName && rule.ServiceCode == t.ServiceCode && rule.Image == t.Image {
			return true
		}
	}
	return false
}
func sameTask(a, b model.Task) bool {
	a.Type, b.Type = "", ""
	a.Action, b.Action = "", ""
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}
func (r *Runner) persist(t model.Task, result model.TaskResult, reserve int64) error {
	record := taskRecord{LedgerID: r.ledgerID, Task: &t, Result: &result}
	if err := r.journal.Append(record, reserve); err != nil {
		return err
	}
	r.tasks[t.OperationID] = record
	return nil
}
func (r *Runner) Handle(ctx context.Context, t model.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.Type != "task" || t.AgentID != r.agentID || t.OperationID == "" || t.NodeID == "" {
		return
	}
	if old, ok := r.tasks[t.OperationID]; ok {
		if !sameTask(*old.Task, t) {
			return
		}
		delete(r.acked, t.OperationID)
		if t.Action == "EXECUTE" && old.Result.Phase == "PREPARED" && !r.active[t.ContainerID] {
			r.executeLocked(ctx, t, *old.Result)
		}
		return
	}
	if t.Action != "PREPARE" {
		return
	} // QUERY absent returns ABSENT via Query, never creates work.
	result := model.TaskResult{Type: "task_result", OperationID: t.OperationID, LedgerID: r.ledgerID, Phase: "REJECTED", ContainerID: t.ContainerID, SnapshotID: t.SnapshotID, Message: "Agent restart rule does not allow this target"}
	if r.authorized(t) && time.Now().Before(t.Deadline) && !r.active[t.ContainerID] {
		target, err := r.executor.InspectTarget(ctx, t.ContainerID)
		if err == nil && !target.Incomplete && target.ID == t.ContainerID && target.Name == t.ContainerName && target.Image == t.Image && target.SnapshotID == t.SnapshotID && (target.Runtime == "running" || target.Runtime == "exited" || target.Runtime == "dead") {
			result.Phase = "PREPARED"
			result.BeforeStartedAt = target.StartedAt
			result.Message = "original identity verified; no restart executed"
		} else {
			result.Message = "original container identity/configuration/state cannot be verified"
		}
	}
	reserve := int64(32768)
	if result.Phase == "REJECTED" {
		reserve = 0
	}
	r.persist(t, result, reserve)
}
func (r *Runner) executeLocked(ctx context.Context, t model.Task, result model.TaskResult) {
	if len(r.active) >= 2 {
		result.Phase = "REJECTED"
		result.Message = "Agent restart concurrency limit reached"
		r.persist(t, result, 0)
		return
	}
	if !r.authorized(t) || !time.Now().Before(t.Deadline) {
		result.Phase = "REJECTED"
		result.Message = "restart permission expired or revoked"
		r.persist(t, result, 0)
		return
	}
	target, err := r.executor.InspectTarget(ctx, t.ContainerID)
	if err != nil || target.Incomplete || target.ID != t.ContainerID || target.Name != t.ContainerName || target.Image != t.Image || target.SnapshotID != t.SnapshotID || (target.Runtime != "running" && target.Runtime != "exited" && target.Runtime != "dead") {
		result.Phase = "REJECTED"
		result.Message = "target changed before execution"
		r.persist(t, result, 0)
		return
	}
	now := time.Now().UTC()
	result.StartedAt = &now
	result.Phase = "EXECUTING"
	result.ExecutionPossible = true
	result.Message = "execution intent durably recorded"
	if r.persist(t, result, 0) != nil {
		return
	}
	r.active[t.ContainerID] = true
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		executionCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		phase, err := r.executor.RestartOriginal(executionCtx, t.ContainerID)
		cancel()
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.active, t.ContainerID)
		finished := time.Now().UTC()
		result.FinishedAt = &finished
		result.Phase = phase
		result.Message = "Docker restart returned; awaiting center verification"
		if err != nil {
			result.Message = err.Error()
		}
		if phase == "EXECUTED" {
			verifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			after, e := r.executor.InspectTarget(verifyCtx, t.ContainerID)
			cancel()
			if e != nil || after.ID != t.ContainerID || after.Name != t.ContainerName || after.Image != t.Image || after.SnapshotID != t.SnapshotID || after.Incomplete {
				result.Phase = "UNCERTAIN"
				result.Message = "post-restart identity cannot be verified"
			} else {
				result.AfterStartedAt = after.StartedAt
			}
		}
		r.persist(t, result, 0)
	}()
}
func (r *Runner) Query(t model.Task) model.TaskResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.tasks[t.OperationID]; ok && sameTask(*record.Task, t) {
		return *record.Result
	}
	return model.TaskResult{Type: "task_result", OperationID: t.OperationID, LedgerID: r.ledgerID, Phase: "ABSENT", Message: "task not present in this durable ledger"}
}
func (r *Runner) Results() []model.TaskResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.TaskResult{}
	for id, record := range r.tasks {
		if !r.acked[id] {
			result = append(result, *record.Result)
		}
	}
	return result
}
func (r *Runner) Ack(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.acked[id] {
		return
	}
	if _, ok := r.tasks[id]; ok && r.journal.Append(taskRecord{LedgerID: r.ledgerID, Ack: id}, 0) == nil {
		r.acked[id] = true
	}
}
