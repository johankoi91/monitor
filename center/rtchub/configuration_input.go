package hub

import "github.com/johankoi91/monitor/runtime/shared/protocol"

// Configuration input excludes execution state and retired dual-probe settings.
type targetInput struct {
	ID          string                   `json:"target_id,omitempty"`
	Name        string                   `json:"name"`
	ClusterCode string                   `json:"cluster_code,omitempty"`
	Driver      string                   `json:"driver"`
	RTC         *protocol.RTCParameters  `json:"rtc,omitempty"`
	Credentials *protocol.AppCredentials `json:"credentials,omitempty"`
}
type taskInput struct {
	Name                 string `json:"name,omitempty" yaml:"name,omitempty"`
	ClientType           string `json:"client_type,omitempty"`
	SweepIntervalSeconds int    `json:"sweep_interval_seconds,omitempty"`
	CrossIntervalSeconds int    `json:"cross_interval_seconds,omitempty"`
	ID                   string `json:"task_id"`
	TargetID             string `json:"target_id"`
	HostID               string `json:"host_id"`
	ServiceType          string `json:"service_type"`
	IntervalSeconds      int    `json:"interval_seconds"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
	FailureThreshold     int    `json:"failure_threshold"`
	RecoveryThreshold    int    `json:"recovery_threshold"`
	UnknownNotifySeconds int    `json:"unknown_notify_seconds"`
	Description          string `json:"description,omitempty"`
}
type configurationInput struct {
	Expected string        `json:"expected_revision"`
	Targets  []targetInput `json:"targets"`
	Tasks    []taskInput   `json:"tasks"`
}

func (h *Hub) configurationPlan(input configurationInput) Plan {
	h.mu.Lock()
	defer h.mu.Unlock()
	plan := Plan{}
	for _, v := range input.Targets {
		plan.Targets = append(plan.Targets, protocol.Target{ID: v.ID, Name: v.Name, ClusterCode: v.ClusterCode, Driver: v.Driver, RTC: v.RTC, Credentials: v.Credentials})
	}
	for _, v := range input.Tasks {
		// Creation starts disabled; configuration changes preserve the single enable state.
		enabled := false
		if current, ok := h.task(v.ID); ok {
			enabled = h.enabled(current)
			if v.Name == "" {
				v.Name = current.Name
			}
		}
		plan.Tasks = append(plan.Tasks, protocol.Task{Name: v.Name, ClientType: v.ClientType, SweepIntervalSeconds: v.SweepIntervalSeconds, CrossIntervalSeconds: v.CrossIntervalSeconds, ID: v.ID, TargetID: v.TargetID, HostID: v.HostID, ServiceType: v.ServiceType, Enabled: enabled, IntervalSeconds: v.IntervalSeconds, TimeoutSeconds: v.TimeoutSeconds, FailureThreshold: v.FailureThreshold, RecoveryThreshold: v.RecoveryThreshold, UnknownNotifySeconds: v.UnknownNotifySeconds, Description: v.Description})
	}
	return plan
}
