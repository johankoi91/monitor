package hub

import (
	"fmt"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"testing"
)

func nativeInventoryFixture() (protocol.NativeCoverageConfig, protocol.NativeInventory) {
	cfg := protocol.NativeCoverageConfig{Groups: []protocol.APBinding{{Group: "172.16.80.25", EntryPoints: []string{"111.230.192.59"}}}, CrossEdge: true}
	inv := protocol.NativeInventory{AccessPoints: []protocol.DiscoveredAccessPoint{{AgentID: "agent", Address: "111.230.192.59", Groups: []string{"172.16.80.25"}}}, Groups: map[string][]protocol.NativeEdge{}}
	for _, kind := range []string{"udp", "aut", "web"} {
		for i := 1; i <= 3; i++ {
			port := 4000 + i
			if kind == "aut" {
				port = 4700 + i
			}
			if kind == "web" {
				port = 4500 + i
			}
			inv.Groups["172.16.80.25"] = append(inv.Groups["172.16.80.25"], protocol.NativeEdge{Transport: kind, Endpoint: fmt.Sprintf("111.230.192.59:%d", port), AgentID: "agent", ContainerName: fmt.Sprintf("%s_%d", kind, i), ContainerID: fmt.Sprintf("%s_id_%d", kind, i)})
		}
	}
	return cfg, inv
}
func TestNativePlanCountsDirectionAndExcludesWeb(t *testing.T) {
	cfg, inv := nativeInventoryFixture()
	for phase, want := range map[string]int{"AP": 2, "DIRECT": 6, "CROSS": 6} {
		plan := BuildNativePlan(cfg, inv, nil, phase, 0)
		if len(plan.Cases) != want || len(plan.Members) != 6 {
			t.Fatal(phase, len(plan.Cases), len(plan.Members))
		}
		seen := map[string]bool{}
		for _, c := range plan.Cases {
			if c.Validate() != nil || c.Sender.Transport == "web" || c.Receiver.Transport == "web" || seen[c.Key] {
				t.Fatal("invalid or duplicate case", c)
			}
			seen[c.Key] = true
			if c.Kind == "AP_JOIN" && (c.APEntryPoint != "111.230.192.59" || c.Sender.Endpoint != "") {
				t.Fatal("AP must use advertised client entry")
			}
		}
	}
}
func TestNativePlanRetainsMissingAndVersionsReplacement(t *testing.T) {
	cfg, inv := nativeInventoryFixture()
	first := BuildNativePlan(cfg, inv, nil, "DIRECT", 0)
	absent := protocol.NativeInventory{AccessPoints: inv.AccessPoints, Groups: map[string][]protocol.NativeEdge{}}
	missing := BuildNativePlan(cfg, absent, first.Members, "DIRECT", 0)
	if len(missing.Members) != 6 || len(missing.Issues) == 0 {
		t.Fatal("missing expected edges silently retired")
	}
	if missing.Version != first.Version {
		t.Fatal("temporary freshness changed inventory identity")
	}
	inv.Groups[cfg.Groups[0].Group][0].ContainerID = "replacement"
	replacement := BuildNativePlan(cfg, inv, first.Members, "DIRECT", 0)
	if replacement.Version == first.Version {
		t.Fatal("new container reused old coverage version")
	}
	cfg.Excluded = []string{first.Members[0].Edge.Key()}
	retired := BuildNativePlan(cfg, absent, first.Members, "DIRECT", 0)
	if len(retired.Members) != 5 {
		t.Fatal("explicit retirement ignored")
	}
}

func TestNativeRingCoversEveryRoleAndBothDirectionsAcrossRounds(t *testing.T) {
	for _, count := range []int{1, 2, 4, 7} {
		cfg, inv := nativeInventoryFixture()
		inv.Groups[cfg.Groups[0].Group] = nil
		for _, kind := range []string{"udp", "aut"} {
			for i := 0; i < count; i++ {
				port := 4001 + i
				if kind == "aut" {
					port = 4701 + i
				}
				inv.Groups[cfg.Groups[0].Group] = append(inv.Groups[cfg.Groups[0].Group], protocol.NativeEdge{Transport: kind, Endpoint: fmt.Sprintf("111.230.192.59:%d", port)})
			}
		}
		first := BuildNativePlan(cfg, inv, nil, "CROSS", 0)
		reverse := BuildNativePlan(cfg, inv, nil, "CROSS", 1)
		if len(first.Cases) != 2*count {
			t.Fatal("ring size is not dynamic", count, len(first.Cases))
		}
		send, recv := map[string]int{}, map[string]int{}
		directions := map[string]bool{}
		for _, c := range first.Cases {
			send[c.Sender.Endpoint]++
			recv[c.Receiver.Endpoint]++
			directions[c.Sender.Endpoint+"->"+c.Receiver.Endpoint] = true
		}
		for _, member := range first.Members {
			if send[member.Edge.Endpoint] != 1 || recv[member.Edge.Endpoint] != 1 {
				t.Fatal("member missing role", member)
			}
		}
		for _, c := range reverse.Cases {
			if !directions[c.Receiver.Endpoint+"->"+c.Sender.Endpoint] {
				t.Fatal("adjacent round did not reverse ring")
			}
		}
	}
}
