package center

import (
	"net"
	"strconv"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// NativeInventory projects the existing startup cache without exposing other arguments.
func (s *Store) NativeInventory() (protocol.NativeInventory, error) {
	discovered := s.MediaEdges()
	result := protocol.NativeInventory{Groups: map[string][]protocol.NativeEdge{}}
	for group, edges := range discovered.ByAP {
		for _, edge := range edges {
			if edge.EdgeType == "web" {
				continue
			}
			result.Groups[group] = append(result.Groups[group], protocol.NativeEdge{Transport: edge.EdgeType, Endpoint: net.JoinHostPort(edge.IPForClient, strconv.Itoa(edge.UserPort)), AgentID: edge.AgentID, ContainerName: edge.ContainerName, ContainerID: edge.ContainerID, SnapshotID: edge.SnapshotID, Stale: edge.Stale})
		}
	}
	for _, skipped := range discovered.Skipped {
		if mediaEdgeType(skipped.ContainerName) != "web" {
			result.Unresolved++
		}
	}
	return result, nil
}
