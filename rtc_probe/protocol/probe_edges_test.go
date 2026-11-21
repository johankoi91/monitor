package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestProbeEdgeWireIsExactlyThreeFields(t *testing.T) {
	for _, kind := range []string{"udp", "aut", "web"} {
		e := NativeEdge{Transport: kind, Endpoint: "183.131.59.146:4001", AgentID: "private-agent", ContainerName: "private-container", ContainerID: "private-container-id", SnapshotID: "private-snapshot", Stale: true}
		c := NativeCase{Sender: e, Receiver: e, Candidates: []NativeEdge{e}}
		j := Job{EdgeFormat: ProbeEdgeAddressFormat, NativeCase: c.ForProbe()}
		data, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"private-agent", "private-container", "private-snapshot", "stale", "transport", "endpoint"} {
			if bytes.Contains(data, []byte(`"`+s+`"`)) || bytes.Contains(data, []byte("private-")) {
				t.Fatal("inventory data leaked", string(data))
			}
		}
		var wire struct {
			NativeCase struct {
				Sender     map[string]any   `json:"sender"`
				Receiver   map[string]any   `json:"receiver"`
				Candidates []map[string]any `json:"candidates"`
			} `json:"native_case"`
		}
		if err = json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		for _, edge := range []map[string]any{wire.NativeCase.Sender, wire.NativeCase.Receiver, wire.NativeCase.Candidates[0]} {
			if len(edge) != 3 || edge["edge_type"] != kind || edge["ip_for_client"] != "183.131.59.146" || edge["user_port"] != float64(4001) {
				t.Fatal(edge)
			}
		}
		var decoded Job
		if err = json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.NativeCase, j.NativeCase) {
			t.Fatal("worker/manifest round trip changed routes", decoded.NativeCase, j.NativeCase)
		}
		rich, _ := json.Marshal(c)
		if !bytes.Contains(rich, []byte("private-snapshot")) {
			t.Fatal("center inventory lost its metadata")
		}
	}
}
func TestProbeEdgeAddressValidationAndCenterInventory(t *testing.T) {
	for _, data := range []string{`{"edge_type":"udp","ip_for_client":"183.131.59.146","user_port":"4001"}`, `{"edge_type":"udp","ip_for_client":"183.131.59.146","user_port":0}`, `{"edge_type":"udp","ip_for_client":"bad","user_port":4001}`, `{"edge_type":"udp","ip_for_client":"183.131.59.146","user_port":4001,"stale":false}`, `{"edge_type":"udp","ip_for_client":"","user_port":null}`} {
		var e NativeEdge
		if json.Unmarshal([]byte(data), &e) == nil {
			t.Fatal("invalid Edge accepted", data)
		}
	}
	for _, data := range []string{`{"edge_type":"aut","ip_for_client":"","user_port":0}`, `{"edge_type":"web","ip_for_client":"2001:db8::1","user_port":4501}`} {
		var e NativeEdge
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ProbeAddress(); err != nil {
			t.Fatal(err)
		}
	}
	var inventory NativeEdge
	if err := json.Unmarshal([]byte(`{"transport":"udp","endpoint":"183.131.59.146:4001","agent_id":"inventory-agent"}`), &inventory); err != nil || inventory.AgentID != "inventory-agent" {
		t.Fatal("center inventory not readable", err)
	}
	data, _ := json.Marshal(inventory)
	if !bytes.Contains(data, []byte("inventory-agent")) {
		t.Fatal("center inventory lost identity")
	}
}

func TestInteropGuardAllowsNativeReceiverAPAllocation(t *testing.T) {
	c := NativeCase{ClientType: InteropClient, Kind: "AP_JOIN", Sender: NativeEdge{Transport: "web"}, Receiver: NativeEdge{Transport: "udp"}, APClientIPs: []string{"192.0.2.1"}, Candidates: []NativeEdge{{Transport: "udp", Endpoint: "192.0.2.1:4001"}, {Transport: "web", Endpoint: "192.0.2.1:4501"}}}
	found := false
	for _, endpoint := range c.AllowedEndpoints() {
		if endpoint == "192.0.2.1:4001" {
			found = true
		}
	}
	if !found {
		t.Fatal("native receiver's AP-assigned edge was blocked")
	}
}

func TestAUTInteropValidatesAndGuardsBothDirections(t *testing.T) {
	for _, sender := range []string{"native", "web"} {
		c := NativeCase{ClientType: InteropAUTClient, Kind: "AP_JOIN", APEntryPoint: "192.0.2.1", SenderClient: sender, ReceiverClient: "web", Sender: NativeEdge{Transport: "aut"}, Receiver: NativeEdge{Transport: "web"}, Candidates: []NativeEdge{{Transport: "aut", Endpoint: "192.0.2.1:4701"}, {Transport: "udp", Endpoint: "192.0.2.1:4001"}, {Transport: "web", Endpoint: "192.0.2.1:4501"}}}
		if sender == "web" {
			c.ReceiverClient = "native"
			c.Sender, c.Receiver = c.Receiver, c.Sender
		}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		aut := false
		for _, endpoint := range c.AllowedEndpoints() {
			if endpoint == "192.0.2.1:4701" {
				aut = true
			}
			if endpoint == "192.0.2.1:4001" {
				t.Fatal("AUT guard allowed UDP fallback")
			}
		}
		if !aut {
			t.Fatal("AUT media endpoint blocked")
		}
		if sender == "native" {
			c.Sender.Transport = "udp"
		} else {
			c.Receiver.Transport = "udp"
		}
		if c.Validate() == nil {
			t.Fatal("AUT case accepted UDP native endpoint")
		}
	}
}

func TestDuplexCaseRequiresCanonicalPeerIdentities(t *testing.T) {
	c := NativeCase{Duplex: true, ClientType: InteropClient, SenderClient: "native", ReceiverClient: "web", Kind: "AP_JOIN", APEntryPoint: "192.0.2.1", Sender: NativeEdge{Transport: "udp"}, Receiver: NativeEdge{Transport: "web"}}
	if c.Validate() != nil {
		t.Fatal("valid duplex case rejected")
	}
	c.SenderClient, c.ReceiverClient = "web", "native"
	c.Sender, c.Receiver = c.Receiver, c.Sender
	if c.Validate() == nil {
		t.Fatal("duplex endpoint identities reversed")
	}
}
