package hub

import (
	"errors"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"sort"
	"strings"
	"time"
)

func taskPhases(task protocol.Task) []string {
	if protocol.IsInteropClient(task.ClientType) {
		return []string{"AP"}
	}
	return nativePhases
}

// Each transport uses one channel where Native and Web both publish and receive.
func BuildInteropPlan(task protocol.Task, inventory protocol.NativeInventory) protocol.NativePlan {
	plan := protocol.NativePlan{Phase: "AP", Topology: "native_udp_aut_web_duplex_v1", Members: []protocol.NativeMember{}, Cases: []protocol.NativeCase{}, Issues: []string{}}
	native, ni := protocol.ResolveDiscoveredAPs(*task.RTC.NativeCoverage, inventory)
	web, wi := protocol.ResolveDiscoveredAPs(*task.RTC.WebAccess.Coverage, inventory)
	plan.Issues = append(ni, wi...)
	sort.Slice(native.Groups, func(i, j int) bool { return native.Groups[i].Group < native.Groups[j].Group })
	var group, entry string
	for _, n := range native.Groups {
		for _, w := range web.Groups {
			if n.Group != w.Group {
				continue
			}
			entries := append([]string(nil), n.EntryPoints...)
			sort.Strings(entries)
			for _, ip := range entries {
				for _, other := range w.EntryPoints {
					if ip == other && entry == "" {
						group, entry = n.Group, ip
					}
				}
			}
		}
	}
	if entry == "" {
		plan.Issues = append(plan.Issues, "NO_SHARED_AP")
	}
	for _, edge := range inventory.Groups[group] {
		if edge.Transport == "udp" || edge.Transport == "aut" || edge.Transport == "web" {
			plan.Members = append(plan.Members, protocol.NativeMember{Group: group, Edge: edge})
		}
	}
	sort.Slice(plan.Members, func(i, j int) bool { return plan.Members[i].Edge.Key() < plan.Members[j].Edge.Key() })
	found := map[string]bool{}
	for _, member := range plan.Members {
		found[member.Edge.Transport] = true
		if member.Edge.Stale {
			plan.Issues = append(plan.Issues, "INVENTORY_STALE")
		}
	}
	commonIssue := ""
	if len(plan.Issues) > 0 {
		commonIssue = plan.Issues[0]
	}
	stable := append([]protocol.NativeMember(nil), plan.Members...)
	for i := range stable {
		stable[i].Edge.Stale = false
	}
	plan.Version = fingerprint(struct {
		Topology, Group, Entry string
		Members                []protocol.NativeMember
	}{plan.Topology, group, entry, stable})
	for _, transport := range []string{"udp", "aut"} {
		unavailable := commonIssue
		if !found[transport] || !found["web"] {
			issue := "INTEROP_" + strings.ToUpper(transport) + "_EDGE_CAPABILITY_UNAVAILABLE"
			plan.Issues = append(plan.Issues, issue)
			if unavailable == "" {
				unavailable = issue
			}
		}
		caseType := protocol.InteropClient
		if transport == "aut" {
			caseType = protocol.InteropAUTClient
		}
		plan.Cases = append(plan.Cases, protocol.NativeCase{Duplex: true, ClientType: caseType, SenderClient: "native", ReceiverClient: "web", Kind: "AP_JOIN", Key: "interop-" + transport + "-duplex", APGroup: group, APEntryPoint: entry, APClientIPs: []string{entry}, Sender: protocol.NativeEdge{Transport: transport}, Receiver: protocol.NativeEdge{Transport: "web"}, CoverageVersion: plan.Version, ExecutionModel: "split_clients_duplex", Unavailable: unavailable})
	}
	return plan
}
func (h *Hub) admitInterop(task protocol.Task, now time.Time, parent string) error {
	for _, client := range []string{"native", "web"} {
		copy := task
		copy.ClientType = client
		copy.RTC = taskRTC(copy, task.RTC)
		if err := h.admitProbe(copy, now, parent); err != nil {
			return err
		}
		runtime := h.sessions[h.runtimeForTask(copy)]
		if runtime == nil {
			return errors.New("PROBE_OFFLINE")
		}
		for _, required := range []string{"native_web_interop_v1", "native_aut_web_interop_v1", "native_web_duplex_v1"} {
			supported := false
			for _, cap := range runtime.Capabilities {
				if cap == required {
					supported = true
				}
			}
			if !supported {
				return errors.New("PROBE_INTEROP_CAPABILITY_UNAVAILABLE")
			}
		}
	}
	return nil
}

func (h *Hub) taskUsesRuntime(task protocol.Task, id string) bool {
	if !protocol.IsInteropClient(task.ClientType) {
		return h.runtimeForTask(task) == id
	}
	for _, client := range []string{"native", "web"} {
		copy := task
		copy.ClientType = client
		if h.runtimeForTask(copy) == id {
			return true
		}
	}
	return false
}
