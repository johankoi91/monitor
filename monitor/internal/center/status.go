package center

import (
	"encoding/json"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

type debounce struct {
	Failures, Successes int
	Known, Healthy      bool
	Binding             string
}

type restartBurst struct {
	ContainerID string
	Count       int
	Changes     []time.Time
}

func (s *Store) updateBursts(id string, a *agentState, now time.Time) {
	for _, o := range a.Report.Containers {
		key := id + "/" + o.Container.ContainerName
		b := s.bursts[key]
		if b == nil || b.ContainerID != o.Container.ContainerID || o.RestartCount < b.Count {
			s.bursts[key] = &restartBurst{ContainerID: o.Container.ContainerID, Count: o.RestartCount}
			continue
		}
		delta := o.RestartCount - b.Count
		if delta > 3 {
			delta = 3
		}
		for i := 0; i < delta; i++ {
			b.Changes = append(b.Changes, now)
		}
		b.Count = o.RestartCount
		kept := []time.Time{}
		for _, at := range b.Changes {
			if now.Sub(at) <= 5*time.Minute {
				kept = append(kept, at)
			}
		}
		if len(kept) > 3 {
			kept = kept[len(kept)-3:]
		}
		b.Changes = kept
	}
}
func (s *Store) updateChecks(agentID string, a *agentState) {
	if s.record.Baseline.Definition == nil || a.Report.BaselineRevision != s.record.Baseline.Revision {
		return
	}
	for _, c := range s.record.Baseline.Definition.Clusters {
		for _, v := range c.Services {
			for _, n := range v.Nodes {
				if n.AgentID != agentID || len(n.Checks) == 0 {
					continue
				}
				for _, o := range a.Report.Containers {
					if o.Container.ContainerName != n.ContainerName {
						continue
					}
					bindingBytes, _ := json.Marshal(n.Checks)
					binding := o.Container.ContainerID + string(bindingBytes)
					state := s.checks[n.ID]
					if state == nil || state.Binding != binding {
						state = &debounce{Binding: binding}
						s.checks[n.ID] = state
					}
					if o.Container.RuntimeStatus != "running" {
						state.Known = false
						state.Healthy = false
						state.Successes = 0
						state.Failures = 0
						continue
					}
					if len(o.Checks) != len(n.Checks) {
						state.Known = false
						continue
					}
					ok := true
					valid := true
					for i, check := range n.Checks {
						r := o.Checks[i]
						if r.Check != check || r.CollectedAt.Before(a.Report.CollectedAt) || r.CollectedAt.After(a.ReceivedAt.Add(5*time.Second)) {
							valid = false
						}
						ok = ok && r.OK
					}
					if !valid {
						state.Known = false
						continue
					}
					if ok {
						state.Successes++
						state.Failures = 0
						if state.Successes >= 2 {
							state.Known = true
							state.Healthy = true
						}
					} else {
						state.Failures++
						state.Successes = 0
						if state.Failures >= 3 {
							state.Known = true
							state.Healthy = false
						}
					}
				}
			}
		}
	}
}

func (s *Store) Statuses(serviceFilter, statusFilter string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusesLocked(serviceFilter, statusFilter, time.Now().UTC())
}
func (s *Store) statusesLocked(serviceFilter, statusFilter string, now time.Time) map[string]any {
	services := []map[string]any{}
	if d := s.record.Baseline.Definition; d != nil {
		for _, cluster := range d.Clusters {
			for _, service := range cluster.Services {
				if serviceFilter != "" && service.Code != serviceFilter {
					continue
				}
				nodes := []map[string]any{}
				healthy, unhealthy, unknown, discovered := 0, 0, 0, 0
				for _, node := range service.Nodes {
					status, runtime, level, reason, message := "UNKNOWN", "unknown", "AGENT", "AGENT_OFFLINE", "等待 Agent 有效快照"
					var collected *time.Time
					image := ""
					restarts := 0
					stale := true
					a := s.agents[node.AgentID]
					var o *model.Observation
					if a != nil {
						for i := range a.Report.Containers {
							if a.Report.Containers[i].Container.ContainerName == node.ContainerName {
								o = &a.Report.Containers[i]
								break
							}
						}
					}
					if a != nil && !a.Report.CollectedAt.IsZero() {
						t := a.Report.CollectedAt
						collected = &t
						if o != nil {
							t = o.Container.CollectedAt
							collected = &t
							image = o.Container.Image
							restarts = o.RestartCount
						}
					}
					if a != nil && a.Connected {
						reason = "STATUS_STALE"
						message = "采集数据过期或尚未收到新快照"
					}
					if fresh(a, now) {
						stale = false
						level = "CONTAINER"
						status = "UNHEALTHY"
						if o == nil {
							runtime = "missing"
							reason = "CONTAINER_MISSING"
							message = "应有容器不在最新完整快照中"
						} else {
							discovered++
							runtime = o.Container.RuntimeStatus
							switch runtime {
							case "running":
								status = "HEALTHY"
								reason = "OK"
								message = "容器运行，未配置业务探测"
								if b := s.bursts[node.AgentID+"/"+node.ContainerName]; b != nil && len(b.Changes) >= 3 && now.Sub(b.Changes[0]) <= 5*time.Minute {
									status = "UNHEALTHY"
									reason = "CONTAINER_RESTARTING"
									message = "五分钟内观测到至少三次 Docker 自动重启"
								}
							case "restarting":
								reason = "CONTAINER_RESTARTING"
								message = "Docker 报告容器正在重启"
							case "exited", "dead", "paused":
								reason = "CONTAINER_EXITED"
								message = "容器未处于运行状态"
							default:
								status = "UNKNOWN"
								reason = "STATUS_STALE"
								message = "Docker 运行态尚不能确认"
							}
							if runtime == "running" && status != "UNHEALTHY" && o.DockerHealth != "" {
								level = "READINESS"
								switch o.DockerHealth {
								case "healthy":
									message = "Docker Healthcheck 通过"
								case "unhealthy":
									status = "UNHEALTHY"
									reason = "DOCKER_HEALTH_UNHEALTHY"
									message = "Docker Healthcheck 失败"
								default:
									status = "UNKNOWN"
									reason = "STATUS_STALE"
									message = "Docker Healthcheck 尚未就绪"
								}
							}
							if runtime == "running" && len(node.Checks) > 0 && status != "UNHEALTHY" {
								level = "READINESS"
								state := s.checks[node.ID]
								if a.Report.BaselineRevision != s.record.Baseline.Revision || state == nil || !state.Known {
									status = "UNKNOWN"
									reason = "STATUS_STALE"
									message = "等待本版 TCP 探测与连续成功确认"
								} else if !state.Healthy {
									status = "UNHEALTHY"
									reason = "TCP_CHECK_FAILED"
									message = "TCP 连续三次失败"
								} else if status == "HEALTHY" {
									message = "容器运行且 TCP 检查通过"
								}
							}
						}
					}
					switch status {
					case "HEALTHY":
						healthy++
					case "UNHEALTHY":
						unhealthy++
					default:
						unknown++
					}
					allowed, block := s.restartAllowedLocked(node, service.Code, now)
					var resources *model.ResourceMetrics
					var lifecycle *model.ContainerLifecycle
					if o != nil {
						resources = resourceView(o.Container.Resources, a, now)
						lifecycle = lifecycleView(o.Container.Lifecycle, a, now)
					}
					nodes = append(nodes, map[string]any{"node_id": node.ID, "host_name": node.HostName, "host_address": node.HostAddress, "container_name": node.ContainerName, "image": image, "status": status, "runtime_status": runtime, "check_level": level, "reason_code": reason, "reason": message, "restart_count": restarts, "restart_enabled": node.RestartEnabled, "restart_allowed": allowed, "restart_block_reason": block, "active_operation_id": s.busyOperationLocked(node.ID), "collected_at": collected, "stale": stale, "resources": resources, "lifecycle": lifecycle})
				}
				status := "UNKNOWN"
				if len(nodes) > 0 {
					if healthy == len(nodes) {
						status = "HEALTHY"
					} else if healthy > 0 {
						status = "DEGRADED"
					} else if unhealthy > 0 {
						status = "UNHEALTHY"
					}
				}
				if statusFilter != "" && statusFilter != status {
					continue
				}
				services = append(services, map[string]any{"cluster_code": cluster.Code, "service_code": service.Code, "service_name": service.Name, "status": status, "expected_node_count": len(nodes), "discovered_node_count": discovered, "healthy_node_count": healthy, "unhealthy_node_count": unhealthy, "unknown_node_count": unknown, "nodes": nodes})
			}
		}
	}
	return map[string]any{"code": 0, "message": "OK", "request_id": ID(), "generated_at": now, "services": services}
}
