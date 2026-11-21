package protocol

import "net"

// ResolveDiscoveredAPs binds a target to discovered AP machines. Group strings
// join Edge --ap arguments; only the machine's advertised address is an ingress.
func ResolveDiscoveredAPs(config NativeCoverageConfig, inventory NativeInventory) (NativeCoverageConfig, []string) {
	out := config
	out.Groups = nil
	issues := []string{}
	seen := map[string]bool{}
	for _, binding := range config.Groups {
		candidates := []DiscoveredAccessPoint{}
		for _, ap := range inventory.AccessPoints {
			match := binding.AgentID != "" && binding.AgentID == ap.AgentID
			if binding.AgentID == "" {
				for _, group := range ap.Groups {
					if group == binding.Group {
						match = true
					}
				}
			}
			if match {
				candidates = append(candidates, ap)
			}
		}
		if len(candidates) != 1 {
			reason := "AP_NOT_DISCOVERED"
			if len(candidates) > 1 {
				reason = "AP_DISCOVERY_AMBIGUOUS"
			}
			issues = append(issues, reason)
			if len(binding.EntryPoints) > 0 {
				out.Groups = append(out.Groups, binding)
			}
			continue
		}
		ap := candidates[0]
		if ap.Stale {
			issues = append(issues, "AP_DISCOVERY_STALE")
		}
		if net.ParseIP(ap.Address) == nil {
			issues = append(issues, "AP_DISCOVERY_INVALID_ADDRESS")
			continue
		}
		for _, group := range ap.Groups {
			if seen[ap.AgentID+"/"+group] {
				continue
			}
			seen[ap.AgentID+"/"+group] = true
			owners := 0
			for _, other := range inventory.AccessPoints {
				for _, value := range other.Groups {
					if value == group {
						owners++
					}
				}
			}
			if owners > 1 {
				issues = append(issues, "AP_DISCOVERY_AMBIGUOUS")
			}
			out.Groups = append(out.Groups, APBinding{AgentID: ap.AgentID, Group: group, EntryPoints: []string{ap.Address}})
		}
	}
	if len(out.Groups) == 0 {
		issues = append(issues, "AP_NOT_DISCOVERED")
	}
	return out, issues
}
