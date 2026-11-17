package hub

import (
	"encoding/json"
	"errors"
)

// Upgrade only configuration bindings. Historical execution snapshots stay intact.
func migrateTaskBindings(data []byte) ([]byte, bool, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, err
	}
	var plan map[string]json.RawMessage
	if len(state["plan"]) == 0 || string(state["plan"]) == "null" {
		return data, false, nil
	}
	if err := json.Unmarshal(state["plan"], &plan); err != nil {
		return nil, false, err
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(plan["tasks"], &tasks); err != nil {
		return nil, false, err
	}
	changed := false
	for _, task := range tasks {
		sender, hasSender := task["sender_probe_id"]
		receiver, hasReceiver := task["receiver_probe_id"]
		if !hasSender && !hasReceiver {
			continue
		}
		if len(task["probe_id"]) == 0 {
			if !hasSender {
				return nil, false, errors.New("旧任务缺少执行探针")
			}
			// Production already required one service for both connections.
			if hasReceiver && string(sender) != string(receiver) {
				return nil, false, errors.New("旧双探针任务需改为单探针后升级")
			}
			task["probe_id"] = sender
		}
		delete(task, "sender_probe_id")
		delete(task, "receiver_probe_id")
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	var err error
	plan["tasks"], err = json.Marshal(tasks)
	if err != nil {
		return nil, false, err
	}
	state["plan"], err = json.Marshal(plan)
	if err != nil {
		return nil, false, err
	}
	result, err := json.Marshal(state)
	return result, true, err
}

// Drop retired settings and queued records while upgrading the control document.
func migrateStoredState(data []byte) ([]byte, bool, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, err
	}
	changed := false
	if _, ok := state["pending_audits"]; ok {
		delete(state, "pending_audits")
		changed = true
	}
	if raw := state["settings"]; len(raw) > 0 && string(raw) != "null" {
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, false, err
		}
		if _, ok := settings["audit_days"]; ok {
			delete(settings, "audit_days")
			clean, err := json.Marshal(settings)
			if err != nil {
				return nil, false, err
			}
			state["settings"] = clean
			changed = true
		}
	}
	if changed {
		var err error
		data, err = json.Marshal(state)
		if err != nil {
			return nil, false, err
		}
	}
	result, bindingsChanged, err := migrateTaskBindings(data)
	return result, changed || bindingsChanged, err
}
