package notify

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"strings"
	"time"
)

func normalKey(id, epoch, cluster, node string) string {
	return strings.Join([]string{id, epoch, cluster, node}, "|")
}

func (n *Notifier) refreshNormalQueued() {
	n.normalQueued = map[string]bool{}
	active := map[string]string{}
	for _, e := range endpoints(n.config) {
		if e.Enabled && e.Events[3] {
			active[e.ID] = e.NormalEpoch
		}
	}
	for _, d := range n.deliveries {
		if d.NormalEpoch != "" && d.Event.CurrentStatus == "HEALTHY" && active[d.EndpointID] == d.NormalEpoch && d.LastError != "CONFIGURATION_CHANGED" && d.Status != "CANCELLED" {
			n.normalQueued[normalKey(d.EndpointID, d.NormalEpoch, d.Event.Cluster, d.Event.Node)] = true
		}
	}
}

func (n *Notifier) normalSnapshots(f Fact, known bool, queued int) []Delivery {
	result := []Delivery{}
	now := time.Now().UTC()
	if !n.config.Enabled || f.Node == "" || f.Status != "HEALTHY" || f.Stale || category(f) != Recovered || f.CollectedAt == nil || now.Sub(*f.CollectedAt) > 45*time.Second || f.CollectedAt.After(now.Add(5*time.Second)) {
		return result
	}
	previous := "UNKNOWN"
	if known {
		previous = f.Status
	}
	e := model.Notification{ID: eventID(), Type: "STATUS_SNAPSHOT", Category: Normal, OccurredAt: now, Product: "RTC", Cluster: f.Cluster, Service: f.Service, Node: f.Node, PreviousStatus: previous, CurrentStatus: f.Status, ReasonCode: f.ReasonCode, Reason: f.Reason, CollectedAt: f.CollectedAt, Stale: false}
	for _, target := range destinations(n.config, Normal) {
		if target.NormalEpoch == "" || n.normalQueued[normalKey(target.ID, target.NormalEpoch, f.Cluster, f.Node)] || queued+len(result) >= n.config.MaxPending {
			continue
		}
		result = append(result, Delivery{EndpointID: target.ID, NormalEpoch: target.NormalEpoch, Event: e, Status: "PENDING", NextAt: now, SettingsRevision: n.settingsRevision})
	}
	return result
}
