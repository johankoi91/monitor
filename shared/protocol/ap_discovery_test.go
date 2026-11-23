package protocol

import (
	"reflect"
	"testing"
)

func TestDiscoveredAPBindsRuntimeAddressesByStableMachine(t *testing.T) {
	config := NativeCoverageConfig{Groups: []APBinding{{AgentID: "ap-node", Group: "172.16.80.25", EntryPoints: []string{"111.230.192.59"}}}, CrossEdge: true}
	inventory := NativeInventory{AccessPoints: []DiscoveredAccessPoint{{AgentID: "ap-node", Address: "183.131.59.146", Groups: []string{"172.16.80.25"}}}}
	resolved, issues := ResolveDiscoveredAPs(config, inventory)
	if len(issues) != 0 || len(resolved.Groups) != 1 || !reflect.DeepEqual(resolved.Groups[0].EntryPoints, []string{"183.131.59.146"}) || config.Groups[0].EntryPoints[0] != "111.230.192.59" {
		t.Fatal("AP discovery mutated saved config or used group IP", resolved, issues)
	}
	inventory.AccessPoints[0].Stale = true
	_, issues = ResolveDiscoveredAPs(config, inventory)
	if len(issues) == 0 || issues[0] != "AP_DISCOVERY_STALE" {
		t.Fatal(issues)
	}
	inventory.AccessPoints = nil
	resolved, issues = ResolveDiscoveredAPs(config, inventory)
	if len(issues) == 0 || len(resolved.Groups) != 1 {
		t.Fatal("missing AP silently lost prior coverage", resolved, issues)
	}
}
