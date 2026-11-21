package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// Plan is editable business configuration; machine credentials stay in deployment config.
type Plan struct {
	Targets []protocol.Target `json:"targets"`
	Tasks   []protocol.Task   `json:"tasks"`
}

func normalizePlan(probes []RuntimeRegistration, plan Plan) (Plan, error) {
	registrations := map[string]RuntimeRegistration{}
	for _, p := range probes {
		registrations[clientKey(p.HostID, p.ServiceType, p.ClientType)] = p
	}
	targets := map[string]bool{}
	names := map[string]bool{}
	drivers := map[string]string{}
	for i := range plan.Targets {
		v := &plan.Targets[i]
		if v.ID == "" {
			v.ID = protocol.ID()
		}
		if !protocol.ValidID(v.ID) || v.Name == "" || len(v.Name) > 128 || (v.Driver != "mock" && v.Driver != "agora") || targets[v.ID] || names[v.Name] {
			return Plan{}, errors.New("目标名称、标识或模拟模式无效")
		}
		targets[v.ID] = true
		names[v.Name] = true
		drivers[v.ID] = v.Driver
		if v.RTC != nil {
			if v.Driver != "agora" {
				return Plan{}, errors.New("仅 RTC SDK 目标可配置接入参数")
			}
			if err := v.RTC.Normalize(); err != nil {
				return Plan{}, err
			}
		}
	}
	tasks := map[string]bool{}
	for i := range plan.Tasks {
		v := &plan.Tasks[i]
		if v.Credentials != nil {
			return Plan{}, errors.New("credentials belong to the target service")
		}
		if len(v.Name) > 240 {
			return Plan{}, errors.New("任务名称过长")
		}
		if v.Scenario == "" {
			v.Scenario = "success"
		}
		if v.IntervalSeconds == 0 {
			v.IntervalSeconds = 60
		}
		if v.TimeoutSeconds == 0 {
			v.TimeoutSeconds = 30
		}
		if v.FailureThreshold == 0 {
			v.FailureThreshold = 3
		}
		if v.RecoveryThreshold == 0 {
			v.RecoveryThreshold = 2
		}
		if v.UnknownNotifySeconds == 0 {
			v.UnknownNotifySeconds = 120
		}
		if v.FailureThreshold < 1 || v.FailureThreshold > 10 || v.RecoveryThreshold < 1 || v.RecoveryThreshold > 10 || v.UnknownNotifySeconds < 30 || v.UnknownNotifySeconds > 3600 || len(v.Description) > 1000 {
			return Plan{}, errors.New("失败/恢复次数、未知通知时长或描述无效")
		}
		if !protocol.ValidID(v.ID) || tasks[v.ID] || !targets[v.TargetID] || !taskRegistered(registrations, *v) || v.IntervalSeconds < 5 || v.IntervalSeconds > 3600 || v.TimeoutSeconds < 3 || v.TimeoutSeconds >= v.IntervalSeconds {
			return Plan{}, errors.New("任务目标、服务器客户端、周期或超时配置无效")
		}
		if !protocol.ValidID(v.HostID) || !serviceName.MatchString(v.ServiceType) || !serviceName.MatchString(v.ClientType) {
			return Plan{}, errors.New("server, service type and client type are required")
		}
		if drivers[v.TargetID] == "agora" && (v.ServiceType != "rtc" || (v.ClientType != "native" && v.ClientType != "web" && v.ClientType != protocol.InteropClient)) {
			return Plan{}, errors.New("RTC tasks require an advertised native or web client")
		}
		if protocol.IsInteropClient(v.ClientType) {
			var target *protocol.Target
			for i := range plan.Targets {
				if plan.Targets[i].ID == v.TargetID {
					target = &plan.Targets[i]
				}
			}
			if target == nil || target.RTC == nil || target.RTC.NativeCoverage == nil || target.RTC.WebAccess == nil || target.RTC.WebAccess.Coverage == nil || v.TimeoutSeconds < 15 {
				return Plan{}, errors.New("互通任务需要 Native/Web 能力和至少15秒超时")
			}
		}
		if v.ClientType == "web" && drivers[v.TargetID] != "agora" {
			return Plan{}, errors.New("Web 探测须关联真实 RTC 目标")
		}
		if drivers[v.TargetID] == "agora" {
			if v.IntervalSeconds < 30 || v.TimeoutSeconds < 5 || v.TimeoutSeconds > 120 {
				return Plan{}, errors.New("真实 RTC 周期为 30–3600 秒，业务超时为 5–120 秒且须短于周期")
			}
			var target *protocol.Target
			for j := range plan.Targets {
				if plan.Targets[j].ID == v.TargetID {
					target = &plan.Targets[j]
					break
				}
			}
			if target == nil || target.RTC == nil {
				return Plan{}, errors.New("请先在目标服务配置 RTC 接入与鉴权")
			}
			if !protocol.IsInteropClient(v.ClientType) && (target.RTC.NativeCoverage != nil || v.ClientType == "web" && target.RTC.WebAccess != nil && target.RTC.WebAccess.Coverage != nil) {
				if v.SweepIntervalSeconds == 0 {
					v.SweepIntervalSeconds = 900
				}
				if v.CrossIntervalSeconds == 0 {
					v.CrossIntervalSeconds = 1800
				}
				if v.SweepIntervalSeconds < 60 || v.SweepIntervalSeconds > 86400 || v.CrossIntervalSeconds < 60 || v.CrossIntervalSeconds > 86400 {
					return Plan{}, errors.New("Native 轮巡周期为 60–86400 秒")
				}
			}
			v.RTC = taskRTC(*v, target.RTC)
			if v.ClientType == "web" {
				if err := v.RTC.WebAccess.Normalize(); err != nil {
					return Plan{}, err
				}
				if v.TimeoutSeconds < 15 {
					return Plan{}, errors.New("Web 探测超时至少为 15 秒")
				}
			}
			v.CredentialVersion = target.CredentialVersion
			v.Credentials = nil
			v.HasCertificate = false
			if v.RTC.MediaDurationMS+2000 > v.TimeoutSeconds*1000 {
				return Plan{}, errors.New("业务超时应至少比媒体发送时长多 2 秒")
			}
		} else if !scenarios[v.Scenario] {
			return Plan{}, errors.New("模拟任务场景无效")
		}
		tasks[v.ID] = true
	}
	if len(plan.Targets) > 50 || len(plan.Tasks) > 100 {
		return Plan{}, errors.New("目标或任务数量达到框架上限")
	}
	return plan, nil
}
func (h *Hub) planLocked() Plan {
	p := Plan{}
	for _, target := range h.config.Targets {
		p.Targets = append(p.Targets, h.publicTarget(target))
	}
	for _, t := range h.config.Tasks {
		t.Enabled = h.enabled(t)
		t = h.publicTask(t)
		p.Tasks = append(p.Tasks, t)
	}
	return p
}
func planRevision(p Plan) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:24]
}
func (h *Hub) UpdatePlan(expected string, plan Plan) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if expected == "" || expected != planRevision(h.planLocked()) {
		return errors.New("配置已变化，请刷新后重试")
	}
	h.readNativeInventory(time.Now().UTC())
	for i := range plan.Targets {
		rtc := cloneRTC(plan.Targets[i].RTC)
		plan.Targets[i].RTC = rtc
		if rtc == nil {
			continue
		}
		coverages := []*protocol.NativeCoverageConfig{rtc.NativeCoverage}
		if rtc.WebAccess != nil {
			coverages = append(coverages, rtc.WebAccess.Coverage)
		}
		for _, coverage := range coverages {
			if coverage == nil {
				continue
			}
			resolved, issues := protocol.ResolveDiscoveredAPs(*coverage, h.nativeSnapshot)
			changed := fingerprint(cloneRTC(h.target(plan.Targets[i].ID).RTC)) != fingerprint(rtc)
			if len(issues) > 0 {
				if changed {
					return errors.New("AP 自动发现不可用：" + issues[0])
				}
				continue
			}
			*coverage = resolved
		}
	}
	p, err := normalizePlan(h.runtimesLocked(), plan)
	if err != nil {
		return err
	}
	if err := h.validateNativeBudget(p.Tasks, p.Targets); err != nil {
		return err
	}
	secrets := map[string]protocol.AppCredentials{}
	for i := range p.Targets {
		target := &p.Targets[i]
		previous := h.target(target.ID)
		old := h.state.TargetCredentials[target.ID]
		if credentialScopeChanged(previous.RTC, target.RTC) {
			old = protocol.AppCredentials{}
		}
		next := overlayCredentials(old, target.Credentials)
		next, err = validateCredentials(target.RTC, next)
		if err != nil {
			return err
		}
		if next != h.state.TargetCredentials[target.ID] || credentialScopeChanged(previous.RTC, target.RTC) {
			target.CredentialVersion = protocol.ID()
		} else {
			target.CredentialVersion = previous.CredentialVersion
		}
		target.Credentials = nil
		target.HasCertificate = false
		secrets[target.ID] = next
	}
	for i := range p.Tasks {
		for _, target := range p.Targets {
			if target.ID == p.Tasks[i].TargetID {
				p.Tasks[i].RTC = taskRTC(p.Tasks[i], target.RTC)
				p.Tasks[i].CredentialVersion = target.CredentialVersion
				break
			}
		}
	}
	affected := map[string]bool{}
	for _, old := range h.config.Tasks {
		found := false
		for _, next := range p.Tasks {
			if next.ID == old.ID {
				found = true
				var target protocol.Target
				for _, v := range p.Targets {
					if v.ID == next.TargetID {
						target = v
						break
					}
				}
				if revision(old, h.target(old.TargetID)) != revision(next, target) || h.enabled(old) != next.Enabled {
					affected[old.ID] = true
				}
				break
			}
		}
		if !found {
			affected[old.ID] = true
		}
	}
	now := time.Now().UTC()
	if err = h.change(func(s *state) error {
		stored := storagePlan(p)
		s.Plan = &stored
		s.TargetCredentials = secrets
		s.Enabled = map[string]bool{}
		for id, current := range s.Native {
			if !affected[id] {
				continue
			}
			var next *protocol.Task
			for i := range p.Tasks {
				if p.Tasks[i].ID == id {
					next = &p.Tasks[i]
					break
				}
			}
			if next == nil || !coverageEnabled(*next) {
				delete(s.Native, id)
				continue
			}
			if old, ok := h.task(id); ok && old.TargetID != next.TargetID {
				current.Expected = nil
			}
			current.Version = ""
			for _, phase := range current.Phases {
				phase.Health = TaskHealth{State: "UNKNOWN", Reason: "CONFIG_CHANGED"}
				phase.NextAt = now
			}
		}
		for id := range s.Health {
			if affected[id] {
				delete(s.Health, id)
			}
		}
		for _, run := range s.Runs {
			if affected[run.TaskID] && !terminal(run.Status) {
				run.Status = "CANCELLED"
				run.Reason = "CONFIG_CHANGED"
				run.FinishedAt = &now
				if run.Kind == nativeCoverageKind {
					h.stopNative(s, run, now, "CONFIG_CHANGED", "CANCELLED")
				}
				if run.Kind == edgeSweepKind {
					h.stopSweep(s, run, now, "CONFIG_CHANGED", "CANCELLED")
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	h.config.Targets, h.config.Tasks = p.Targets, p.Tasks
	for _, t := range p.Tasks {
		if affected[t.ID] || h.next[t.ID].IsZero() {
			h.next[t.ID] = now.Add(time.Duration(t.IntervalSeconds) * time.Second)
		}
	}
	return nil
}

func taskRegistered(registrations map[string]RuntimeRegistration, task protocol.Task) bool {
	if protocol.IsInteropClient(task.ClientType) {
		return registrations[clientKey(task.HostID, task.ServiceType, "native")].ID != "" && registrations[clientKey(task.HostID, task.ServiceType, "web")].ID != ""
	}
	return registrations[clientKey(task.HostID, task.ServiceType, task.ClientType)].ID != ""
}
