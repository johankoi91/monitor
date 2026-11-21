package hub

import (
	"fmt"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"time"
)

// Completion-based intervals keep queues bounded. Validate their combined worst-case load.
func (h *Hub) validateNativeBudget(tasks []protocol.Task, targets []protocol.Target) error {
	nativeProbe := map[string]bool{}
	for _, task := range tasks {
		for _, target := range targets {
			if task.TargetID == target.ID && target.RTC != nil && (target.RTC.NativeCoverage != nil || target.RTC.WebAccess != nil && target.RTC.WebAccess.Coverage != nil) && task.Enabled {
				nativeProbe[h.runtimeForTask(task)] = true
			}
		}
	}
	if len(nativeProbe) == 0 {
		return nil
	}
	h.readNativeInventory(time.Now().UTC())
	loads := map[string]float64{}
	for _, task := range tasks {
		if !task.Enabled || !nativeProbe[h.runtimeForTask(task)] {
			continue
		}
		var target protocol.Target
		for _, value := range targets {
			if value.ID == task.TargetID {
				target = value
				break
			}
		}
		demand := float64(0)
		var prior []protocol.NativeMember
		if state := h.state.Native[task.ID]; state != nil {
			prior = state.Expected
		}
		effective := task
		if effective.RTC == nil {
			effective.RTC = target.RTC
		}
		if coverageEnabled(effective) {
			for _, phase := range taskPhases(task) {
				if phase == "CROSS" && !coverageConfig(effective).CrossEdge {
					continue
				}
				plan := buildCoveragePlan(effective, *coverageConfig(effective), h.nativeSnapshot, prior, phase, 0)
				work := nativeBudget(task, len(plan.Cases)).Seconds()
				workers := h.nativeWorkerLimit(task, len(plan.Members))
				if workers > len(plan.Cases) {
					workers = len(plan.Cases)
				}
				if workers < 1 {
					workers = 1
				}
				// Resource demand is worker-seconds / the shortest possible cycle.
				// A parallel round can complete much sooner than the serial work.
				round := nativeBudget(task, (len(plan.Cases)+workers-1)/workers).Seconds()
				demand += work / (round + phaseInterval(task, phase).Seconds())
			}
			limit := h.nativeWorkerLimit(task, len(buildCoveragePlan(effective, *coverageConfig(effective), h.nativeSnapshot, prior, "AP", 0).Members))
			if demand > float64(limit) {
				return fmt.Errorf("Native 执行周期超出单任务预算（%.0f%%），请增大 AP 或轮巡间隔", demand*100)
			}
		} else {
			work := (time.Duration(task.TimeoutSeconds)*time.Second + protocol.CleanupGrace + protocol.ResultReportGrace).Seconds()
			demand = work / float64(task.IntervalSeconds)
		}
		loads[h.runtimeForTask(task)] += demand
		if protocol.IsInteropClient(task.ClientType) {
			web := task
			web.ClientType = "web"
			loads[h.runtimeForTask(web)] += demand
		}
	}
	for probe, load := range loads {
		capacity := 1
		if session := h.sessions[probe]; session != nil && session.Capacity > 0 {
			capacity = session.Capacity
		}
		if load > float64(capacity) {
			return fmt.Errorf("探针 %s 的任务周期合计超出并发预算（%.0f%% / %d 个工作槽位）", probe, load*100, capacity)
		}
	}
	return nil
}

// Native concurrency follows the target's expected inventory, with independent
// probe and center ceilings. No fixed Edge count is used for dispatch.
func (h *Hub) nativeWorkerLimit(task protocol.Task, members int) int {
	if members < 1 {
		members = 1
	}
	limit := 1
	if session := h.sessions[h.runtimeForTask(task)]; session != nil && session.Capacity > 0 {
		limit = session.Capacity
	}
	if protocol.IsInteropClient(task.ClientType) {
		web := task
		web.ClientType = "web"
		if session := h.sessions[h.runtimeForTask(web)]; session == nil {
			limit = 1
		} else if session.Capacity < limit {
			limit = session.Capacity
		}
	}
	if members < limit {
		limit = members
	}
	if h.config.MaxConcurrentRuns < limit {
		limit = h.config.MaxConcurrentRuns
	}
	return limit
}
