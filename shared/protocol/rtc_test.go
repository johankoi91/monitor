package protocol

import "testing"

func TestNativeNormalizeDerivesAPClientAddresses(t *testing.T) {
	p := RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", AccessIPs: []string{"192.0.2.99"}, AccessDomains: []string{"stale.invalid"}, NativeCoverage: &NativeCoverageConfig{Groups: []APBinding{{Group: "172.16.80.25", EntryPoints: []string{"111.230.192.59"}}}}}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	if !p.Private || p.ProxyMode != 1 || p.DisableAut || len(p.AccessIPs) != 1 || p.AccessIPs[0] != "111.230.192.59" || len(p.AccessDomains) != 0 {
		t.Fatalf("wrong Native AP configuration: %+v", p)
	}
}
func TestDisabledLegacyRangeKeepsAccessConfiguration(t *testing.T) {
	enabled := false
	p := RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", Private: true, AccessIPs: []string{"111.230.192.59"}, MediaEdges: &MediaEdgeRange{Enabled: &enabled}}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	if p.MediaEdges != nil || !p.Private || len(p.AccessIPs) != 1 || p.AccessIPs[0] != "111.230.192.59" {
		t.Fatalf("disabled range changed access config: %+v", p)
	}
}
