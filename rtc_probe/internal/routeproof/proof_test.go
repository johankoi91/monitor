package routeproof

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"testing"
)

func TestDirectionSeparatesMediaFromAcknowledgementsInOneProcess(t *testing.T) {
	c := &protocol.NativeCase{Kind: "CROSS_EDGE_MEDIA", APGroup: "172.16.80.25", Sender: protocol.NativeEdge{Transport: "udp", Endpoint: "111.230.192.59:4001"}, Receiver: protocol.NativeEdge{Transport: "aut", Endpoint: "111.230.192.59:4701"}}
	job := protocol.Job{Role: "pair", NativeCase: c}
	events := []Observation{{At: 2, Direction: "tx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 240000}, {At: 2, Direction: "rx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 6000}, {At: 2, Direction: "tx", Protocol: "udp", IP: "111.230.192.59", Port: 4701, Bytes: 20000}, {At: 2, Direction: "rx", Protocol: "udp", IP: "111.230.192.59", Port: 4701, Bytes: 240000}}
	for _, role := range []string{"sender", "receiver"} {
		if result := Assess(job, role, events, 1, 3); result.Status != "VERIFIED" {
			t.Fatal(role, result)
		}
	}
	wrong := *c
	wrong.Sender.Endpoint = "111.230.192.59:4002"
	job.NativeCase = &wrong
	if result := Assess(job, "sender", events, 1, 3); result.Status == "VERIFIED" {
		t.Fatal("different observed edge accepted")
	}
}
func TestAPRequiresObservedClientEntryAndCorrectNativeType(t *testing.T) {
	c := &protocol.NativeCase{Kind: "AP_JOIN", APEntryPoint: "111.230.192.59", Sender: protocol.NativeEdge{Transport: "udp"}, Receiver: protocol.NativeEdge{Transport: "udp"}, Candidates: []protocol.NativeEdge{{Transport: "udp", Endpoint: "111.230.192.59:4001"}, {Transport: "aut", Endpoint: "111.230.192.59:4701"}}}
	job := protocol.Job{Role: "pair", NativeCase: c}
	events := []Observation{{At: 2, Direction: "tx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 240000}, {At: 2, Direction: "rx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 240000}}
	if result := Assess(job, "receiver", events, 1, 3); result.Status == "VERIFIED" {
		t.Fatal("AP result accepted without AP exchange")
	}
	events = append(events, Observation{At: 0, Direction: "tx", IP: c.APEntryPoint, Port: 8003, Bytes: 100}, Observation{At: 0, Direction: "rx", IP: c.APEntryPoint, Port: 8003, Bytes: 100})
	if result := Assess(job, "receiver", events, 1, 3); result.Status != "VERIFIED" {
		t.Fatal(result)
	}
	c.Sender.Transport = "aut"
	c.Receiver.Transport = "aut"
	if result := Assess(job, "receiver", events, 1, 3); result.Status != "MISMATCH" {
		t.Fatal("UDP exchange counted as AUT", result)
	}
}
func TestBlockedAttemptsAndHandshakeCannotEstablishRoute(t *testing.T) {
	c := &protocol.NativeCase{Kind: "DIRECT_JOIN", Sender: protocol.NativeEdge{Transport: "udp", Endpoint: "111.230.192.59:4001"}, Receiver: protocol.NativeEdge{Transport: "udp", Endpoint: "111.230.192.59:4001"}}
	job := protocol.Job{Role: "pair", NativeCase: c}
	events := []Observation{{At: 2, Direction: "blocked", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 200000}, {At: 0, Direction: "tx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 200000}, {At: 2, Direction: "rx", Protocol: "udp", IP: "111.230.192.59", Port: 4001, Bytes: 100}}
	if result := Assess(job, "sender", events, 1, 3); result.Status == "VERIFIED" {
		t.Fatal("blocked attempt or early traffic counted as media")
	}
}

func TestEventCollectorCannotCountAsMediaOrUnknownEdge(t *testing.T) {
	c := &protocol.NativeCase{Kind: "AP_JOIN", APEntryPoint: "183.131.59.146", Sender: protocol.NativeEdge{Transport: "udp"}, Receiver: protocol.NativeEdge{Transport: "udp"}, Candidates: []protocol.NativeEdge{{Transport: "udp", Endpoint: "183.131.59.146:4001"}}}
	job := protocol.Job{Role: "pair", NativeCase: c}
	events := []Observation{{At: 0, Direction: "tx", IP: c.APEntryPoint, Port: 8003, Bytes: 100}, {At: 0, Direction: "rx", IP: c.APEntryPoint, Port: 8003, Bytes: 100}, {At: 2, Direction: "tx", Protocol: "udp", IP: c.APEntryPoint, Port: 4301, Bytes: 240000}, {At: 2, Direction: "rx", Protocol: "udp", IP: c.APEntryPoint, Port: 4301, Bytes: 240000}, {At: 2, Direction: "blocked", Protocol: "udp", IP: c.APEntryPoint, Port: 4301, Bytes: 1}}
	if result := Assess(job, "receiver", events, 1, 3); result.Status == "VERIFIED" {
		t.Fatal("collector counted as media", result)
	}
	events = append(events, Observation{At: 2, Direction: "tx", Protocol: "udp", IP: c.APEntryPoint, Port: 4001, Bytes: 240000}, Observation{At: 2, Direction: "rx", Protocol: "udp", IP: c.APEntryPoint, Port: 4001, Bytes: 240000})
	if result := Assess(job, "receiver", events, 1, 3); result.Status != "VERIFIED" || len(result.Observed) != 1 {
		t.Fatal("collector misclassified as Edge", result)
	}
	events = append(events, Observation{At: 2, Direction: "blocked", Protocol: "udp", IP: c.APEntryPoint, Port: 4009, Bytes: 1})
	if result := Assess(job, "receiver", events, 1, 3); result.Reason != "AP_ALLOCATED_UNDISCOVERED_ENDPOINT" {
		t.Fatal("unknown frontend ignored", result)
	}
	allowed := false
	for _, endpoint := range c.AllowedEndpoints() {
		if endpoint == "183.131.59.146:4301" {
			allowed = true
		}
	}
	if !allowed {
		t.Fatal("report endpoint blocked")
	}
}

func TestDuplexRouteRequiresRealMediaInBothDirections(t *testing.T) {
	job := protocol.Job{Role: "sender", NativeCase: &protocol.NativeCase{Duplex: true, Kind: "AP_JOIN", APEntryPoint: "192.0.2.1", Sender: protocol.NativeEdge{Transport: "udp"}, Receiver: protocol.NativeEdge{Transport: "web"}, Candidates: []protocol.NativeEdge{{Transport: "udp", Endpoint: "192.0.2.1:4001"}, {Transport: "aut", Endpoint: "192.0.2.1:4701"}}}}
	events := []Observation{{At: 0, Direction: "tx", IP: "192.0.2.1", Port: 8003, Bytes: 100}, {At: 0, Direction: "rx", IP: "192.0.2.1", Port: 8003, Bytes: 100}, {At: 2, Direction: "tx", Protocol: "udp", IP: "192.0.2.1", Port: 4001, Bytes: 200000}, {At: 2, Direction: "rx", Protocol: "udp", IP: "192.0.2.1", Port: 4001, Bytes: 190000}}
	if proof := Assess(job, "sender", events, 1, 3); proof.Status != "VERIFIED" || proof.Scope != "bidirectional_media" {
		t.Fatal("balanced duplex traffic rejected", proof)
	}
	events[3].Bytes = 100
	if Assess(job, "sender", events, 1, 3).Status == "VERIFIED" {
		t.Fatal("acknowledgements counted as receiving media")
	}
	events[3].Bytes = 190000
	events[3].Port = 4701
	if Assess(job, "sender", events, 1, 3).Status == "VERIFIED" {
		t.Fatal("receive side silently changed Native transport")
	}
}
