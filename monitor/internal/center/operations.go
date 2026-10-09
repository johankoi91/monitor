package center

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johankoi91/monitor/runtime/internal/journal"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/storage"
	"gopkg.in/yaml.v3"
)

func (s *Store) openOperations() error {
	s.operations = map[string]model.Operation{}
	s.requestKeys = map[string]string{}
	s.executeReady = map[string]string{}
	s.acks = map[string]map[string]bool{}
	for _, rule := range s.config.RestartRules {
		if rule.Approved && (!identifier.MatchString(rule.AgentID) || stringsInvalidName(rule.ContainerName) || !identifier.MatchString(rule.ServiceCode) || rule.Image == "" || strings.TrimSpace(rule.ApprovalRef) == "") {
			return errors.New("approved restart rule needs exact identity and approval reference")
		}
	}
	restore := func(line json.RawMessage) error {
		var record model.AuditRecord
		if json.Unmarshal(line, &record) != nil || record.At.IsZero() || record.Kind == "" || record.Operation.OperationID == "" || record.Operation.RequestKey == "" || record.Operation.NodeID == "" {
			return errors.New("invalid operation audit record")
		}
		o := record.Operation
		if previous := s.requestKeys[o.RequestKey]; previous != "" && previous != o.OperationID {
			return errors.New("conflicting stored idempotency key")
		}
		s.requestKeys[o.RequestKey] = o.OperationID
		s.operations[o.OperationID] = o
		return nil
	}
	var j storage.Log
	var err error
	if s.backend != nil {
		j, err = s.backend.OpenLog("operations", 64<<20, restore)
	} else {
		j, err = journal.Open(filepath.Join(s.dir, "operations.jsonl"), 64<<20, restore)
	}
	if err != nil {
		return err
	}
	s.opJournal = j
	s.opWrite = func(r model.AuditRecord, reserve int64) error { return j.Append(r, reserve) }
	busy := map[string]bool{}
	for _, o := range s.operations {
		if o.NodeLocked {
			if busy[o.NodeID] {
				return errors.New("conflicting stored node operations")
			}
			busy[o.NodeID] = true
		}
	}
	return nil
}
func (s *Store) busyOperationLocked(node string) string {
	for id, o := range s.operations {
		if o.NodeID == node && o.NodeLocked {
			return id
		}
	}
	return ""
}
func (s *Store) ruleLocked(node model.Node, service string, image string) bool {
	for _, r := range s.config.RestartRules {
		if r.Approved && r.ApprovalRef != "" && r.AgentID == node.AgentID && r.ContainerName == node.ContainerName && r.ServiceCode == service && r.Image == image {
			return true
		}
	}
	return false
}
func (s *Store) findNodeLocked(id string) (model.Node, string, *model.Observation) {
	if d := s.record.Baseline.Definition; d != nil {
		for _, c := range d.Clusters {
			for _, v := range c.Services {
				for _, n := range v.Nodes {
					if n.ID == id {
						var found *model.Observation
						if a := s.agents[n.AgentID]; a != nil {
							for i := range a.Report.Containers {
								if a.Report.Containers[i].Container.ContainerName == n.ContainerName {
									found = &a.Report.Containers[i]
									break
								}
							}
						}
						return n, v.Code, found
					}
				}
			}
		}
	}
	return model.Node{}, "", nil
}
func (s *Store) restartAllowedLocked(n model.Node, service string, now time.Time) (bool, string) {
	if !n.RestartEnabled {
		return false, "重启白名单未开放"
	}
	if s.storageBad || s.opJournal != nil && !s.opJournal.Healthy() {
		return false, "操作存储不可用"
	}
	if s.busyOperationLocked(n.ID) != "" {
		return false, "已有执行中或结果未知的重启"
	}
	a := s.agents[n.AgentID]
	if !fresh(a, now) {
		return false, "Agent 离线或数据过期"
	}
	_, _, o := s.findNodeLocked(n.ID)
	if o == nil {
		return false, "容器缺失"
	}
	if o.Startup == nil || o.Startup.Incomplete || o.Startup.Truncated {
		return false, "启动配置不完整"
	}
	if !s.ruleLocked(n, service, o.Container.Image) {
		return false, "服务/容器/镜像未匹配已批准的重启规则"
	}
	runtime := o.Container.RuntimeStatus
	if runtime != "running" && runtime != "exited" && runtime != "dead" {
		return false, "当前运行态不允许重启"
	}
	for _, operation := range s.operations {
		if operation.NodeID == n.ID && operation.FinishedAt != nil && now.Sub(*operation.FinishedAt) < 120*time.Second {
			return false, "重启冷却期 120 秒"
		}
	}
	return true, ""
}
func (s *Store) persistOperationLocked(o model.Operation, kind string, reserve int64) error {
	if s.storageBad || s.opWrite == nil {
		return problem(503, "OPERATION_STORE_UNAVAILABLE", "操作记录不可用，未执行重启")
	}
	if err := s.opWrite(model.AuditRecord{At: time.Now().UTC(), Kind: kind, Operation: o}, reserve); err != nil {
		if reserve == 0 || !errors.Is(err, journal.ErrCapacity) {
			s.storageBad = true
		}
		return problem(503, "OPERATION_STORE_UNAVAILABLE", "操作记录未可靠保存，禁止执行或追加重启")
	}
	s.operations[o.OperationID] = o
	s.requestKeys[o.RequestKey] = o.OperationID
	return nil
}
func (s *Store) CreateRestart(r model.RestartRequest, source, ip string) (model.Operation, error) {
	return s.createRestart(r, source, ip, "")
}

func (s *Store) CreateCustomerRestart(r model.RestartRequest, source, ip, ownerPrefix string) (model.Operation, error) {
	return s.createRestart(r, source, ip, ownerPrefix)
}

func (s *Store) createRestart(r model.RestartRequest, source, ip, ownerPrefix string) (model.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.NodeID = strings.TrimSpace(r.NodeID)
	r.Operator = strings.TrimSpace(r.Operator)
	r.Reason = strings.TrimSpace(r.Reason)
	r.RequestKey = strings.TrimSpace(r.RequestKey)
	if r.NodeID == "" || len(r.NodeID) > 512 || r.Operator == "" || utf8.RuneCountInString(r.Operator) > 128 || utf8.RuneCountInString(r.Reason) < 2 || utf8.RuneCountInString(r.Reason) > 500 || r.RequestKey == "" || len(r.RequestKey) > 128 {
		return model.Operation{}, problem(400, "INVALID_RESTART_REQUEST", "请填写节点、操作说明人、2–500 字原因和幂等键")
	}
	if id := s.requestKeys[r.RequestKey]; id != "" {
		o := s.operations[id]
		if ownerPrefix != "" && !strings.HasPrefix(o.AuthenticatedSource, ownerPrefix) {
			return model.Operation{}, problem(403, "OPERATION_OWNER_DENIED", "幂等键已属于其他调用来源")
		}
		if o.NodeID != r.NodeID || o.Operator != r.Operator || o.Reason != r.Reason {
			return model.Operation{}, problem(409, "IDEMPOTENCY_CONFLICT", "同一幂等键的目标或参数不同")
		}
		return clone(o), nil
	}
	n, service, o := s.findNodeLocked(r.NodeID)
	if n.ID == "" {
		return model.Operation{}, problem(404, "NODE_NOT_FOUND", "节点不在已保存的基准中")
	}
	allowed, reason := s.restartAllowedLocked(n, service, time.Now())
	if !allowed {
		return model.Operation{}, problem(409, "NODE_NOT_RESTARTABLE", reason)
	}
	active := 0
	for _, operation := range s.operations {
		if operation.NodeLocked {
			active++
		}
	}
	if active >= 16 {
		return model.Operation{}, problem(409, "OPERATION_CAPACITY", "执行中或未知操作已达 16 个上限")
	}
	before := "UNKNOWN"
	for _, v := range s.statusesLocked("", "", time.Now())["services"].([]map[string]any) {
		for _, node := range v["nodes"].([]map[string]any) {
			if node["node_id"] == n.ID {
				before = node["status"].(string)
			}
		}
	}
	now := time.Now().UTC()
	id := ID()
	task := model.Task{Type: "task", Action: "PREPARE", OperationID: id, AgentID: n.AgentID, NodeID: n.ID, ServiceCode: service, ContainerID: o.Container.ContainerID, ContainerName: n.ContainerName, Image: o.Container.Image, SnapshotID: o.Container.SnapshotID, Deadline: now.Add(40 * time.Second)}
	operation := model.Operation{OperationID: id, RequestKey: r.RequestKey, Type: "RESTART_CONTAINER", Status: "PENDING", Phase: "ACCEPTED", NodeID: n.ID, Operator: r.Operator, Reason: r.Reason, AuthenticatedSource: source, SourceIP: ip, RequestedAt: now, BeforeStatus: before, Message: "任务已可靠受理，等待 Agent 执行前核验", NodeLocked: true, Task: task}
	if err := s.persistOperationLocked(operation, "ACCEPTED", int64(active+1)*32768); err != nil {
		return model.Operation{}, err
	}
	return clone(operation), nil
}
func (s *Store) Operation(id string) (model.Operation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.operations[id]
	if !ok {
		return o, problem(404, "OPERATION_NOT_FOUND", "操作不存在")
	}
	return clone(o), nil
}
func (s *Store) OperationList(limit int) []model.Operation {
	return s.operationList(limit, "")
}

func (s *Store) OwnOperations(ownerPrefix string) []model.Operation {
	return s.operationList(50, ownerPrefix)
}

func (s *Store) operationList(limit int, ownerPrefix string) []model.Operation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := []model.Operation{}
	for _, o := range s.operations {
		if ownerPrefix != "" && !strings.HasPrefix(o.AuthenticatedSource, ownerPrefix) {
			continue
		}
		list = append(list, clone(o))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].RequestedAt.After(list[j].RequestedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}
func (s *Store) ActiveOperations() []model.Operation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []model.Operation{}
	for _, o := range s.operations {
		if o.NodeLocked {
			result = append(result, clone(o))
		}
	}
	return result
}

func (s *Store) Audit(id string) ([]model.AuditRecord, error) {
	if _, err := s.Operation(id); err != nil {
		return nil, err
	}
	records := []model.AuditRecord{}
	if s.backend != nil {
		rows, err := s.backend.ReadLog("operations", id, 200)
		if err != nil {
			return nil, problem(503, "AUDIT_UNAVAILABLE", "审计读取失败")
		}
		for _, raw := range rows {
			var r model.AuditRecord
			if json.Unmarshal(raw, &r) != nil {
				return nil, problem(503, "AUDIT_UNAVAILABLE", "审计数据无效")
			}
			records = append(records, r)
		}
		return records, nil
	}
	err := s.opJournal.Scan(func(line json.RawMessage) error {
		var record model.AuditRecord
		if json.Unmarshal(line, &record) != nil {
			return errors.New("audit corrupt")
		}
		if record.Operation.OperationID == id {
			records = append(records, record)
			if len(records) > 200 {
				records = records[1:]
			}
		}
		return nil
	})
	if err != nil {
		return nil, problem(503, "AUDIT_UNAVAILABLE", "审计读取失败，记录保留供核查")
	}
	return records, nil
}
func (s *Store) Commands(id, session string) ([]model.Task, []model.ResultAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := []model.Task{}
	acks := []model.ResultAck{}
	a := s.agents[id]
	if a == nil || a.Session != session {
		return tasks, acks
	}
	if !s.storageBad && s.opJournal.Healthy() {
		for opID, o := range s.operations {
			if o.Task.AgentID != id || !o.NodeLocked {
				continue
			}
			t := o.Task
			t.Action = "QUERY"
			if o.Phase == "ACCEPTED" {
				t.Action = "PREPARE"
			} else if o.Phase == "EXECUTE_INTENT" && s.executeReady[opID] == session {
				t.Action = "EXECUTE"
				delete(s.executeReady, opID)
			}
			tasks = append(tasks, t)
		}
	}
	for opID := range s.acks[id] {
		acks = append(acks, model.ResultAck{Type: "result_ack", OperationID: opID})
		delete(s.acks[id], opID)
	}
	return tasks, acks
}
func (s *Store) ackLocked(agent, id string) {
	if s.acks[agent] == nil {
		s.acks[agent] = map[string]bool{}
	}
	s.acks[agent][id] = true
}
func finishOperation(o *model.Operation, status, phase, message string, known bool, at time.Time) {
	o.Status = status
	o.Phase = phase
	o.Message = message
	o.ResultKnown = known
	o.NodeLocked = !known
	o.FinishedAt = &at
	o.DurationMS = at.Sub(o.RequestedAt).Milliseconds()
}

func (s *Store) ReceiveResult(agent, session string, r model.TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.agents[agent]
	if a == nil || a.Session != session {
		return errors.New("old task result session")
	}
	o, ok := s.operations[r.OperationID]
	if !ok || o.Task.AgentID != agent {
		return errors.New("unknown or foreign operation")
	}
	if !o.NodeLocked {
		s.ackLocked(agent, r.OperationID)
		return nil
	}
	if r.LedgerID == "" {
		return errors.New("missing Agent ledger identity")
	}
	if o.LedgerID != "" && o.LedgerID != r.LedgerID {
		if o.Phase == "UNCERTAIN" {
			return nil
		}
		finishOperation(&o, "TIMEOUT", "UNCERTAIN", "Agent 任务账本身份变化，需现场核实", false, time.Now().UTC())
		return s.persistOperationLocked(o, "LEDGER_CHANGED", 0)
	}
	if r.Phase == "ABSENT" {
		if o.Phase == "ACCEPTED" {
			return nil
		}
		if o.Phase != "UNCERTAIN" {
			finishOperation(&o, "TIMEOUT", "UNCERTAIN", "Agent 无法提供既有执行证据，需现场核实", false, time.Now().UTC())
			return s.persistOperationLocked(o, "EVIDENCE_ABSENT", 0)
		}
		return nil
	}
	if r.ContainerID != o.Task.ContainerID || r.SnapshotID != o.Task.SnapshotID {
		return errors.New("task evidence identity mismatch")
	}
	now := time.Now().UTC()
	switch r.Phase {
	case "PREPARED":
		if r.ExecutionPossible {
			return errors.New("prepared task already executable")
		}
		if o.Phase == "ACCEPTED" {
			n, service, observed := s.findNodeLocked(o.NodeID)
			a := s.agents[n.AgentID]
			if !n.RestartEnabled || !fresh(a, now) || !s.ruleLocked(n, service, o.Task.Image) || observed == nil || observed.Container.ContainerID != o.Task.ContainerID || observed.Container.SnapshotID != o.Task.SnapshotID || now.After(o.Task.Deadline) {
				finishOperation(&o, "REJECTED", "DONE", "执行前规则或数据已变化，未下发执行", true, now)
			} else {
				o.Status = "RUNNING"
				o.Phase = "EXECUTE_INTENT"
				o.StartedAt = &now
				o.LedgerID = r.LedgerID
				o.Message = "已落盘执行意图，等待 Agent 原容器重启"
			}
			if err := s.persistOperationLocked(o, "PREPARED", 0); err != nil {
				return err
			}
		}
		if o.Phase == "EXECUTE_INTENT" {
			n, service, observed := s.findNodeLocked(o.NodeID)
			if !n.RestartEnabled || !s.ruleLocked(n, service, o.Task.Image) || observed != nil && (observed.Container.ContainerID != o.Task.ContainerID || observed.Container.SnapshotID != o.Task.SnapshotID) {
				finishOperation(&o, "REJECTED", "DONE", "核对未执行任务时规则或目标已变化", true, now)
				return s.persistOperationLocked(o, "RECONCILE_REJECTED", 0)
			}
			if !fresh(s.agents[n.AgentID], now) {
				return nil
			}
			if now.After(o.Task.Deadline) {
				finishOperation(&o, "FAILED", "DONE", "Agent 确认仍未执行，任务已过期", true, now)
				return s.persistOperationLocked(o, "EXPIRED_UNEXECUTED", 0)
			}
			s.executeReady[o.OperationID] = session
		} else if !o.NodeLocked {
			s.ackLocked(agent, r.OperationID)
		}
	case "EXECUTING":
		if o.Phase == "EXECUTE_INTENT" {
			o.Phase = "WAIT_RESULT"
			o.Message = "Agent 已持久记录执行阶段，等待证据"
			o.Evidence = &r
			return s.persistOperationLocked(o, "EXECUTING", 0)
		}
	case "EXECUTED":
		if o.Phase == "ACCEPTED" || r.StartedAt == nil || r.FinishedAt == nil || r.AfterStartedAt == "" || r.AfterStartedAt == r.BeforeStartedAt {
			return errors.New("invalid execution evidence")
		}
		if o.Phase != "VERIFY" {
			o.Status = "RUNNING"
			o.Phase = "VERIFY"
			o.FinishedAt = nil
			o.Evidence = &r
			deadline := r.FinishedAt.Add(60 * time.Second)
			o.VerifyDeadline = &deadline
			o.Message = "原容器已执行重启，复查当前检查层级"
			if err := s.persistOperationLocked(o, "EXECUTED", 0); err != nil {
				return err
			}
		}
		s.ackLocked(agent, r.OperationID)
	case "REJECTED", "FAILED":
		if o.Phase == "VERIFY" {
			return nil
		}
		status := "FAILED"
		if r.Phase == "REJECTED" {
			status = "REJECTED"
		}
		finishOperation(&o, status, "DONE", r.Message, true, now)
		o.Evidence = &r
		if err := s.persistOperationLocked(o, "AGENT_REJECTED_OR_FAILED", 0); err != nil {
			return err
		}
		s.ackLocked(agent, r.OperationID)
	case "UNCERTAIN":
		if o.Phase != "UNCERTAIN" {
			finishOperation(&o, "TIMEOUT", "UNCERTAIN", "执行结果未知，未重试；需现场核实后关闭", false, now)
			o.Evidence = &r
			if err := s.persistOperationLocked(o, "EXECUTION_UNCERTAIN", 0); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid Agent task phase")
	}
	return nil
}

// ApplyRestartRules is a deployment action, never a selection-form privilege.
// New selections remain disabled until approved rules are applied at startup.
func (s *Store) ApplyRestartRules() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Baseline.Definition == nil {
		return nil
	}
	record := clone(s.record)
	changed := false
	for ci := range record.Baseline.Definition.Clusters {
		for si := range record.Baseline.Definition.Clusters[ci].Services {
			service := &record.Baseline.Definition.Clusters[ci].Services[si]
			for ni := range service.Nodes {
				n := &service.Nodes[ni]
				approved := false
				for _, rule := range s.config.RestartRules {
					if rule.Approved && rule.ApprovalRef != "" && rule.AgentID == n.AgentID && rule.ContainerName == n.ContainerName && rule.ServiceCode == service.Code {
						approved = true
					}
				}
				if n.RestartEnabled != approved {
					n.RestartEnabled = approved
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	at := time.Now().UTC()
	revision := ID()
	url := "/api/v1/baseline/yaml?revision=" + revision
	record.Baseline.Revision = revision
	record.Baseline.SavedAt = &at
	record.Baseline.YAMLURL = &url
	record.Source = "deployment:restart_rules"
	record.Change = model.Selection{ExpectedRevision: s.record.Baseline.Revision, Additions: []model.Addition{}, Removals: []string{}}
	data, err := yaml.Marshal(record.Baseline.Definition)
	if err != nil {
		return err
	}
	record.YAML = string(data)
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = s.write(encoded); err != nil {
		return err
	}
	s.record = record
	return nil
}
func (s *Store) TickOperations(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := map[string]map[string]any{}
	for _, v := range s.statusesLocked("", "", now)["services"].([]map[string]any) {
		for _, n := range v["nodes"].([]map[string]any) {
			states[n["node_id"].(string)] = n
		}
	}
	for _, o := range s.operations {
		if !o.NodeLocked {
			continue
		}
		switch o.Phase {
		case "ACCEPTED":
			if now.After(o.Task.Deadline) {
				finishOperation(&o, "REJECTED", "DONE", "执行前准备超时，未下发重启", true, now)
				s.persistOperationLocked(o, "PREPARE_TIMEOUT", 0)
			}
		case "EXECUTE_INTENT", "WAIT_RESULT":
			if now.After(o.Task.Deadline) {
				finishOperation(&o, "TIMEOUT", "UNCERTAIN", "执行结果超时，节点保持锁定，禁止重复下发", false, now)
				s.persistOperationLocked(o, "RESULT_TIMEOUT", 0)
			}
		case "VERIFY":
			_, _, observed := s.findNodeLocked(o.NodeID)
			state := states[o.NodeID]
			status := "UNKNOWN"
			if state != nil {
				status = state["status"].(string)
			}
			o.AfterStatus = &status
			if observed != nil && (observed.Container.ContainerID != o.Task.ContainerID || observed.Container.SnapshotID != o.Task.SnapshotID) {
				finishOperation(&o, "FAILED", "DONE", "复查发现容器身份或配置变化", true, now)
				s.persistOperationLocked(o, "VERIFY_IDENTITY_FAILED", 0)
			} else if o.VerifyDeadline == nil || now.After(*o.VerifyDeadline) {
				finishOperation(&o, "TIMEOUT", "DONE", "原容器已执行重启，但 60 秒复查窗口未通过", true, now)
				s.persistOperationLocked(o, "VERIFY_TIMEOUT", 0)
			} else if status == "HEALTHY" && observed != nil && o.Evidence != nil && o.Evidence.FinishedAt != nil && !observed.Container.CollectedAt.Before(*o.Evidence.FinishedAt) && observed.StartedAt == o.Evidence.AfterStartedAt {
				finishOperation(&o, "SUCCESS", "DONE", "原容器运行且当前配置的检查通过；请复查业务", true, now)
				s.persistOperationLocked(o, "VERIFY_SUCCESS", 0)
			}
		}
	}
}
func (s *Store) Resolve(id string, r model.ResolveRequest, source, ip string) (model.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.operations[id]
	if !ok {
		return o, problem(404, "OPERATION_NOT_FOUND", "操作不存在")
	}
	if o.Phase != "UNCERTAIN" || !o.NodeLocked {
		return o, problem(409, "NOT_UNCERTAIN", "仅可关闭待现场核实的未知结果")
	}
	if strings.TrimSpace(r.Operator) == "" || len(r.Operator) > 128 || len(strings.TrimSpace(r.Reason)) < 2 || len(r.Reason) > 500 || len(strings.TrimSpace(r.Evidence)) < 10 || len(r.Evidence) > 1000 || (r.Outcome != "EXECUTED" && r.Outcome != "NOT_EXECUTED") {
		return o, problem(400, "INVALID_RESOLUTION", "需填写核实人、原因、现场证据及 EXECUTED/NOT_EXECUTED 结论")
	}
	finishOperation(&o, "FAILED", "MANUALLY_CLOSED", "人工核实并关闭未知结果（"+r.Outcome+"），不据此修改健康状态", true, time.Now().UTC())
	o.Message += "；" + r.Operator + "：" + r.Reason + "；证据：" + r.Evidence + "；来源：" + source + "/" + ip
	if err := s.persistOperationLocked(o, "MANUAL_RESOLUTION", 0); err != nil {
		return model.Operation{}, err
	}
	return clone(o), nil
}
