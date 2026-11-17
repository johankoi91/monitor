package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"sort"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func fingerprint(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:24]
}

// BuildNativePlan freezes the selected Native inventory. Missing expected members
// remain marked stale until a target configuration explicitly retires them.
func BuildNativePlan(config protocol.NativeCoverageConfig, inventory protocol.NativeInventory, prior []protocol.NativeMember, phase string, rotation uint64) protocol.NativePlan {
	plan := protocol.NativePlan{Topology: "ring", Phase: phase, Members: []protocol.NativeMember{}, Cases: []protocol.NativeCase{}, Issues: []string{}}
	groups := map[string]bool{}
	excluded := map[string]bool{}
	for _, binding := range config.Groups {
		groups[binding.Group] = true
	}
	for _, key := range config.Excluded {
		excluded[key] = true
	}
	members := map[string]protocol.NativeMember{}
	for _, member := range prior {
		if groups[member.Group] && !excluded[member.Edge.Key()] {
			member.Edge.Stale = true
			members[member.Edge.Key()] = member
		}
	}
	for group, edges := range inventory.Groups {
		if !groups[group] {
			continue
		}
		for _, edge := range edges {
			if (edge.Transport != "udp" && edge.Transport != "aut") || excluded[edge.Key()] {
				continue
			}
			if edge.Validate() != nil || edge.Endpoint == "" {
				plan.Issues = append(plan.Issues, "INVALID_NATIVE_ENDPOINT")
				continue
			}
			members[edge.Key()] = protocol.NativeMember{Group: group, Edge: edge}
		}
	}
	for _, member := range members {
		plan.Members = append(plan.Members, member)
		if member.Edge.Stale {
			plan.Issues = append(plan.Issues, "INVENTORY_STALE")
		}
	}
	sort.Slice(plan.Members, func(i, j int) bool {
		return plan.Members[i].Group+plan.Members[i].Edge.Key() < plan.Members[j].Group+plan.Members[j].Edge.Key()
	})
	if inventory.Unresolved > 0 {
		plan.Issues = append(plan.Issues, "INVENTORY_INCOMPLETE")
	}
	if len(plan.Members) > 64 {
		plan.Issues = append(plan.Issues, "NATIVE_INVENTORY_LIMIT")
	}
	stable := append([]protocol.NativeMember(nil), plan.Members...)
	for i := range stable {
		stable[i].Edge.Stale = false
	}
	plan.Version = fingerprint(struct {
		Topology string
		Config   protocol.NativeCoverageConfig
		Members  []protocol.NativeMember
	}{"ring-v1", config, stable})
	if len(plan.Members) > 64 {
		binding := config.Groups[0]
		plan.Cases = []protocol.NativeCase{{Key: "inventory-limit", Kind: "AP_JOIN", CoverageVersion: plan.Version, APGroup: binding.Group, APEntryPoint: binding.EntryPoints[0], Sender: protocol.NativeEdge{Transport: "udp"}, Receiver: protocol.NativeEdge{Transport: "udp"}, Unavailable: "NATIVE_INVENTORY_LIMIT"}}
		return plan
	}
	add := func(group, entry, kind string, sender, receiver protocol.NativeEdge, unavailable string) {
		c := protocol.NativeCase{Kind: kind, APGroup: group, APEntryPoint: entry, Sender: sender, Receiver: receiver, CoverageVersion: plan.Version, Unavailable: unavailable, ParameterScope: "connection", ExecutionModel: "shared"}

		c.APClientIPs = []string{entry}
		if kind != "AP_JOIN" {
			for _, binding := range config.Groups {
				if binding.Group == group {
					c.APClientIPs = append([]string(nil), binding.EntryPoints...)
				}
			}
		}
		c.Key = fingerprint(struct{ Kind, Group, Entry, Sender, Receiver, ST, RT string }{kind, group, entry, sender.Endpoint, receiver.Endpoint, sender.Transport, receiver.Transport})
		plan.Cases = append(plan.Cases, c)
	}
	for _, binding := range config.Groups {
		kinds := map[string][]protocol.NativeEdge{"udp": {}, "aut": {}}
		for _, member := range plan.Members {
			if member.Group == binding.Group {
				kinds[member.Edge.Transport] = append(kinds[member.Edge.Transport], member.Edge)
			}
		}
		for _, kind := range []string{"udp", "aut"} {
			sort.Slice(kinds[kind], func(i, j int) bool { return kinds[kind][i].Endpoint < kinds[kind][j].Endpoint })
		}
		switch phase {
		case "AP":
			for _, entry := range binding.EntryPoints {
				for _, kind := range []string{"udp", "aut"} {
					reason := ""
					if len(kinds[kind]) == 0 {
						reason = "NO_" + kind + "_EDGE_DISCOVERED"
					}
					add(binding.Group, entry, "AP_JOIN", protocol.NativeEdge{Transport: kind}, protocol.NativeEdge{Transport: kind}, reason)
				}
			}
		case "DIRECT":
			for _, kind := range []string{"udp", "aut"} {
				if len(kinds[kind]) == 0 {
					add(binding.Group, binding.EntryPoints[0], "DIRECT_JOIN", protocol.NativeEdge{Transport: kind}, protocol.NativeEdge{Transport: kind}, "NO_"+kind+"_EDGE_DISCOVERED")
				}
				for _, edge := range kinds[kind] {
					add(binding.Group, binding.EntryPoints[0], "DIRECT_JOIN", edge, edge, "")
				}
			}
		case "CROSS":
			if !config.CrossEdge {
				continue
			}
			// Every member sends once and receives once in this directed ring.
			// Adjacent rounds reverse the same ring; subsequent rounds shuffle it.
			ring := []protocol.NativeEdge{}
			for _, kind := range []string{"udp", "aut"} {
				edges := append([]protocol.NativeEdge(nil), kinds[kind]...)
				if rotation/2 > 0 {
					random := rand.New(rand.NewSource(int64(rotation / 2)))
					random.Shuffle(len(edges), func(i, j int) { edges[i], edges[j] = edges[j], edges[i] })
				}
				ring = append(ring, edges...)
			}
			if len(ring) < 2 {
				add(binding.Group, binding.EntryPoints[0], "CROSS_EDGE_MEDIA", protocol.NativeEdge{Transport: "udp"}, protocol.NativeEdge{Transport: "aut"}, "CROSS_EDGE_REQUIRES_TWO_INSTANCES")
				continue
			}
			for i, sender := range ring {
				next := (i + 1) % len(ring)
				if rotation%2 == 1 {
					next = (i + len(ring) - 1) % len(ring)
				}
				add(binding.Group, binding.EntryPoints[0], "CROSS_EDGE_MEDIA", sender, ring[next], "")
			}
		}
	}
	sort.Strings(plan.Issues)
	unique := plan.Issues[:0]
	for _, issue := range plan.Issues {
		if len(unique) == 0 || unique[len(unique)-1] != issue {
			unique = append(unique, issue)
		}
	}
	plan.Issues = unique
	return plan
}
