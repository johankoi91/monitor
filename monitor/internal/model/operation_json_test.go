package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCompactOperationPreservesRecoveryEvidence(t *testing.T) {
	now := time.Now().UTC()
	for _, phase := range []string{"ACCEPTED", "EXECUTE_INTENT", "WAIT_RESULT", "VERIFY", "UNCERTAIN", "DONE", "MANUALLY_CLOSED"} {
		o := Operation{OperationID: "op", NodeID: "agent/container", RequestKey: "once", Status: "RUNNING", Phase: phase, NodeLocked: true, RequestedAt: now, LedgerID: "ledger", Task: Task{AgentID: "agent", ContainerID: "original", ContainerName: "container", SnapshotID: "config", Image: "image", Deadline: now.Add(time.Minute)}}
		if phase == "UNCERTAIN" {
			o.Status = "TIMEOUT"
			o.FinishedAt = &now
		}
		if phase == "DONE" || phase == "MANUALLY_CLOSED" {
			o.Status = "SUCCESS"
			if phase == "MANUALLY_CLOSED" {
				o.Status = "FAILED"
			}
			o.NodeLocked = false
			o.FinishedAt = &now
		}
		if phase == "ACCEPTED" {
			o.Status = "PENDING"
		}
		body, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		json.Unmarshal(body, &fields)
		for _, key := range []string{"operator", "duration_ms", "result_known", "phase", "task", "type"} {
			if _, ok := fields[key]; ok {
				t.Fatalf("redundant field %s retained", key)
			}
		}
		var restored Operation
		if err = json.Unmarshal(body, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.Phase != phase || restored.Status != o.Status || restored.NodeLocked != o.NodeLocked || restored.Task.ContainerID != "original" || restored.Task.SnapshotID != "config" || restored.Task.OperationID != "op" {
			t.Fatalf("phase %s recovery changed: %+v", phase, restored)
		}
		if o.NodeLocked && !restored.Task.Deadline.Equal(o.Task.Deadline) {
			t.Fatal("active deadline lost")
		}
	}
}
