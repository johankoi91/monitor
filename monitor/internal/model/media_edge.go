package model

import "time"

// MediaEdgeEndpoint is one container's advertised RTC media ingress.
type MediaEdgeEndpoint struct {
	EdgeType      string `json:"edge_type"`
	ContainerID   string `json:"container_id"`
	SnapshotID    string `json:"snapshot_id"`
	IPForClient   string `json:"ip_for_client"`
	UserPort      int    `json:"user_port"`
	AgentID       string `json:"agent_id"`
	HostIP        string `json:"host_ip"`
	ContainerName string `json:"container_name"`
	Stale         bool   `json:"stale"`
}
type MediaEdgeSkipped struct {
	AgentID       string `json:"agent_id"`
	HostIP        string `json:"host_ip"`
	ContainerName string `json:"container_name"`
	Reason        string `json:"reason"`
}
type MediaEdgeInventory struct {
	ByAP              map[string][]MediaEdgeEndpoint `json:"by_ap"`
	Hosts             []string                       `json:"hosts"`
	MatchedContainers int                            `json:"matched_containers"`
	Skipped           []MediaEdgeSkipped             `json:"skipped"`
	GeneratedAt       time.Time                      `json:"generated_at"`
}
