package model

import "encoding/json"

// The durable/API representation keeps one state and one target. Transport
// envelopes are reconstructed when sending a command, not stored repeatedly.
func (o Operation) MarshalJSON() ([]byte, error) {
	type alias Operation
	b, err := json.Marshal(alias(o))
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	v["record_version"] = 2
	state := o.Phase
	if !o.NodeLocked {
		state = o.Status
		if o.Phase == "MANUALLY_CLOSED" {
			state = "MANUALLY_CLOSED"
		}
	}
	v["status"] = state
	for _, key := range []string{"phase", "result_known", "duration_ms", "operator", "type"} {
		delete(v, key)
	}
	if target, ok := v["task"].(map[string]any); ok {
		for _, key := range []string{"type", "operation_id", "node_id", "action"} {
			delete(target, key)
		}
		if !o.NodeLocked {
			delete(target, "deadline")
		}
		v["target"] = target
		delete(v, "task")
	}
	if !o.NodeLocked {
		delete(v, "verify_deadline")
	}
	if evidence, ok := v["evidence"].(map[string]any); ok {
		for _, key := range []string{"type", "operation_id", "container_id", "snapshot_id", "ledger_id"} {
			delete(evidence, key)
		}
		if !o.NodeLocked {
			for _, key := range []string{"phase", "message", "execution_possible", "started_at", "finished_at"} {
				delete(evidence, key)
			}
		}
	}
	return json.Marshal(v)
}

func (o *Operation) UnmarshalJSON(b []byte) error {
	type alias Operation
	var v map[string]json.RawMessage
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var version int
	_ = json.Unmarshal(v["record_version"], &version)
	if version == 2 {
		v["task"] = v["target"]
		var state string
		_ = json.Unmarshal(v["status"], &state)
		phase, status := state, "RUNNING"
		switch state {
		case "ACCEPTED":
			status = "PENDING"
		case "SUCCESS", "FAILED", "REJECTED", "TIMEOUT":
			status = state
			phase = "DONE"
		case "MANUALLY_CLOSED":
			status = "FAILED"
			phase = state
		case "UNCERTAIN":
			status = "TIMEOUT"
		}
		v["status"], _ = json.Marshal(status)
		v["phase"], _ = json.Marshal(phase)
		b, _ = json.Marshal(v)
	}
	if err := json.Unmarshal(b, (*alias)(o)); err != nil {
		return err
	}
	o.Type = "RESTART_CONTAINER"
	o.Task.Type = "task"
	o.Task.OperationID = o.OperationID
	o.Task.NodeID = o.NodeID
	o.Task.Action = "PREPARE"
	if o.Evidence != nil {
		o.Evidence.Type = "task_result"
		o.Evidence.OperationID = o.OperationID
		o.Evidence.ContainerID = o.Task.ContainerID
		o.Evidence.SnapshotID = o.Task.SnapshotID
		o.Evidence.LedgerID = o.LedgerID
	}
	return nil
}
