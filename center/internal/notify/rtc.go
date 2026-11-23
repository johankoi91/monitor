package notify

import (
	"github.com/johankoi91/monitor/runtime/shared/model"
	"strings"
	"time"
)

// ObserveRTC shares the durable HMAC/retry transport, but keeps business facts,
// subscriptions and identities distinct from container observations.
func (n *Notifier) ObserveRTC(facts []Fact) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failed {
		return
	}
	current := map[string]bool{}
	batch := record{}
	flush := func() bool {
		if len(batch.Changes)+len(batch.Removed)+len(batch.Deliveries) == 0 {
			return true
		}
		ok := n.persist(batch)
		batch = record{}
		return ok
	}
	for _, f := range facts {
		if !f.Business || !strings.HasPrefix(f.Key, "rtc/") {
			continue
		}
		current[f.Key] = true
		old, known := n.facts[f.Key]
		changed := !known || old.Status != f.Status || old.ConfigRevision != f.ConfigRevision || old.Notify != f.Notify
		if !changed {
			continue
		}
		batch.Changes = append(batch.Changes, f)
		category := RTCUnknown
		if f.Status == "UNHEALTHY" {
			category = RTCAbnormal
		} else if f.Status == "HEALTHY" {
			category = RTCRecovered
		}
		initialNormal := !known && f.Status == "HEALTHY"
		configurationChanged := known && old.ConfigRevision != f.ConfigRevision
		if configurationChanged {
			for _, delivery := range n.deliveries {
				if delivery.Event.TaskID == f.TaskID && (delivery.Status == "PENDING" || delivery.Status == "INFLIGHT") {
					delivery.Status = "CANCELLED"
					delivery.LastError = "TASK_CONFIGURATION_CHANGED"
					batch.Deliveries = append(batch.Deliveries, delivery)
				}
			}
		}
		eligible := f.Notify && !initialNormal && !configurationChanged && (!known || old.Status != f.Status || !old.Notify)
		if f.Status == "HEALTHY" && known && (!old.Notify || old.Status == "HEALTHY") {
			eligible = false
		}
		if n.config.Enabled && eligible {
			now := time.Now().UTC()
			e := model.Notification{ID: eventID(), Type: "RTC_BUSINESS_STATUS_CHANGED", Category: category, Layer: "BLACKBOX", OccurredAt: now, Product: "RTC", Cluster: f.Cluster, Service: f.TargetID, TaskID: f.TaskID, TargetID: f.TargetID, RunID: f.RunID, ConfigRevision: f.ConfigRevision, Path: f.Path, PreviousStatus: old.Status, CurrentStatus: f.Status, ReasonCode: f.ReasonCode, Reason: f.Reason, CollectedAt: f.CollectedAt, Stale: f.Stale}
			if e.PreviousStatus == "" {
				e.PreviousStatus = "UNKNOWN"
			}
			for _, target := range destinations(n.config, category) {
				if n.pending()+len(batch.Deliveries) >= n.config.MaxPending {
					batch.Dropped++
				} else {
					batch.Deliveries = append(batch.Deliveries, Delivery{EndpointID: target.ID, Event: e, Status: "PENDING", NextAt: now, SettingsRevision: n.settingsRevision})
				}
			}
		}
		if len(batch.Changes)+len(batch.Deliveries) >= 32 && !flush() {
			return
		}
	}
	for key := range n.facts {
		if strings.HasPrefix(key, "rtc/") && !current[key] {
			batch.Removed = append(batch.Removed, key)
			for _, delivery := range n.deliveries {
				if delivery.Event.TaskID == n.facts[key].TaskID && (delivery.Status == "PENDING" || delivery.Status == "INFLIGHT") {
					delivery.Status = "CANCELLED"
					delivery.LastError = "TASK_REMOVED_OR_DISABLED"
					batch.Deliveries = append(batch.Deliveries, delivery)
				}
			}
		}
	}
	flush()
}
