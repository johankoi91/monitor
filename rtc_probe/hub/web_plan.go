package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"math/rand"
	"sort"
)

func buildCoveragePlan(task protocol.Task, config protocol.NativeCoverageConfig, inventory protocol.NativeInventory, prior []protocol.NativeMember, phase string, round uint64) protocol.NativePlan {
	if protocol.IsInteropClient(task.ClientType) {
		return BuildInteropPlan(task, inventory)
	}
	if task.ClientType == "web" {
		return BuildWebPlan(config, inventory, prior, phase, round)
	}
	return BuildNativePlan(config, inventory, prior, phase, round)
}
func BuildWebPlan(config protocol.NativeCoverageConfig, inventory protocol.NativeInventory, prior []protocol.NativeMember, phase string, round uint64) protocol.NativePlan {
	plan := protocol.NativePlan{Topology: "ring", Phase: phase, Members: []protocol.NativeMember{}, Cases: []protocol.NativeCase{}, Issues: []string{}}
	config, apIssues := protocol.ResolveDiscoveredAPs(config, inventory)
	plan.Issues = append(plan.Issues, apIssues...)
	if len(config.Groups) == 0 {
		plan.Version = fingerprint(config)
		plan.Cases = []protocol.NativeCase{{Key: "ap-unavailable", ClientType: "web", Kind: "AP_JOIN", Sender: protocol.NativeEdge{Transport: "web"}, Receiver: protocol.NativeEdge{Transport: "web"}, Unavailable: "AP_NOT_DISCOVERED"}}
		return plan
	}
	groups, excluded := map[string]bool{}, map[string]bool{}
	for _, g := range config.Groups {
		groups[g.Group] = true
	}
	for _, key := range config.Excluded {
		excluded[key] = true
	}
	members := map[string]protocol.NativeMember{}
	for _, m := range prior {
		if groups[m.Group] && m.Edge.Transport == "web" && !excluded[m.Edge.Key()] {
			m.Edge.Stale = true
			members[m.Edge.Key()] = m
		}
	}
	for group, edges := range inventory.Groups {
		if !groups[group] {
			continue
		}
		for _, edge := range edges {
			if edge.Transport != "web" || excluded[edge.Key()] {
				continue
			}
			if edge.Validate() != nil || edge.Endpoint == "" {
				plan.Issues = append(plan.Issues, "INVALID_WEB_ENDPOINT")
				continue
			}
			members[edge.Key()] = protocol.NativeMember{Group: group, Edge: edge}
		}
	}
	for _, m := range members {
		plan.Members = append(plan.Members, m)
		if m.Edge.Stale {
			plan.Issues = append(plan.Issues, "INVENTORY_STALE")
		}
	}
	sort.Slice(plan.Members, func(i, j int) bool {
		return plan.Members[i].Group+plan.Members[i].Edge.Key() < plan.Members[j].Group+plan.Members[j].Edge.Key()
	})
	if inventory.WebUnresolved > 0 {
		plan.Issues = append(plan.Issues, "WEB_INVENTORY_INCOMPLETE")
	}
	stable := append([]protocol.NativeMember(nil), plan.Members...)
	for i := range stable {
		stable[i].Edge.Stale = false
	}
	plan.Version = fingerprint(struct {
		Type    string
		Config  protocol.NativeCoverageConfig
		Members []protocol.NativeMember
	}{"web-ring-v1", config, stable})
	add := func(binding protocol.APBinding, entry, kind string, sender, receiver protocol.NativeEdge, unavailable string) {
		if unavailable == "" && len(apIssues) > 0 {
			unavailable = apIssues[0]
		}
		c := protocol.NativeCase{ClientType: "web", Kind: kind, APGroup: binding.Group, APEntryPoint: entry, APClientIPs: binding.EntryPoints, Sender: sender, Receiver: receiver, CoverageVersion: plan.Version, ExecutionModel: "browser_contexts", ParameterScope: "browser_context", Unavailable: unavailable}
		if kind == "AP_JOIN" {
			c.APClientIPs = []string{entry}
		}
		c.Key = fingerprint(struct{ Kind, Group, Entry, Sender, Receiver string }{kind, binding.Group, entry, sender.Endpoint, receiver.Endpoint})
		plan.Cases = append(plan.Cases, c)
	}
	for _, binding := range config.Groups {
		var edges []protocol.NativeEdge
		for _, m := range plan.Members {
			if m.Group == binding.Group {
				edges = append(edges, m.Edge)
			}
		}
		if len(plan.Members) > 64 {
			plan.Issues = append(plan.Issues, "WEB_INVENTORY_LIMIT")
			add(binding, binding.EntryPoints[0], "AP_JOIN", protocol.NativeEdge{Transport: "web"}, protocol.NativeEdge{Transport: "web"}, "WEB_INVENTORY_LIMIT")
			break
		}
		sort.Slice(edges, func(i, j int) bool { return edges[i].Endpoint < edges[j].Endpoint })
		switch phase {
		case "AP":
			for _, entry := range binding.EntryPoints {
				reason := ""
				if len(edges) == 0 {
					reason = "NO_WEB_EDGE_DISCOVERED"
				}
				add(binding, entry, "AP_JOIN", protocol.NativeEdge{Transport: "web"}, protocol.NativeEdge{Transport: "web"}, reason)
			}
		case "DIRECT":
			if len(edges) == 0 {
				add(binding, binding.EntryPoints[0], "AP_JOIN", protocol.NativeEdge{Transport: "web"}, protocol.NativeEdge{Transport: "web"}, "NO_WEB_EDGE_DISCOVERED")
			}
			for _, edge := range edges {
				add(binding, binding.EntryPoints[0], "DIRECT_JOIN", edge, edge, "")
			}
		case "CROSS":
			if !config.CrossEdge {
				continue
			}
			if len(edges) < 2 {
				add(binding, binding.EntryPoints[0], "AP_JOIN", protocol.NativeEdge{Transport: "web"}, protocol.NativeEdge{Transport: "web"}, "CROSS_EDGE_REQUIRES_TWO_INSTANCES")
				continue
			}
			if round/2 > 0 {
				random := rand.New(rand.NewSource(int64(round / 2)))
				random.Shuffle(len(edges), func(i, j int) { edges[i], edges[j] = edges[j], edges[i] })
			}
			for i, edge := range edges {
				next := (i + 1) % len(edges)
				if round%2 == 1 {
					next = (i + len(edges) - 1) % len(edges)
				}
				add(binding, binding.EntryPoints[0], "CROSS_EDGE_MEDIA", edge, edges[next], "")
			}
		}
	}
	sort.Strings(plan.Issues)
	return plan
}
