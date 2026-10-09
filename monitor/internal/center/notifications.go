package center

import (
	"time"

	"github.com/johankoi91/monitor/runtime/internal/notify"
)

func (s *Store) NotificationFacts() (string, []notify.Fact) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	facts := []notify.Fact{}
	view := s.statusesLocked("", "", time.Now().UTC())
	for _, service := range view["services"].([]map[string]any) {
		cluster, code := service["cluster_code"].(string), service["service_code"].(string)
		for _, node := range service["nodes"].([]map[string]any) {
			at, _ := node["collected_at"].(*time.Time)
			nodeStale := node["stale"].(bool)
			// Keep internal task/baseline IDs stable; expose the host IP and
			// exact container name as the notification's external identity.
			notificationNode := node["host_address"].(string) + "-" + node["container_name"].(string)
			facts = append(facts, notify.Fact{Key: "node/" + node["node_id"].(string), Cluster: cluster, Service: code, Node: notificationNode, Status: node["status"].(string), ReasonCode: node["reason_code"].(string), Reason: node["reason"].(string), CollectedAt: at, Stale: nodeStale})
		}
	}
	return s.record.Baseline.Revision, facts
}
