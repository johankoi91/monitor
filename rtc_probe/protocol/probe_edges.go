package protocol

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
)

const ProbeEdgeAddressCapability = "edge_address_v1"
const ProbeEdgeAddressFormat = "address_v1"

// ProbeEdgeAddress is the complete Edge object allowed on the probe wire.
// Empty IP and port 0 represent AP assignment, without pinning a media Edge.
type ProbeEdgeAddress struct {
	EdgeType    string `json:"edge_type"`
	IPForClient string `json:"ip_for_client"`
	UserPort    int    `json:"user_port"`
}

func (e NativeEdge) ProbeAddress() (ProbeEdgeAddress, error) {
	out := ProbeEdgeAddress{EdgeType: e.Transport}
	if err := e.Validate(); err != nil {
		return out, err
	}
	if e.Endpoint == "" {
		return out, nil
	}
	host, port, err := net.SplitHostPort(e.Endpoint)
	if err != nil {
		return out, err
	}
	out.IPForClient = host
	out.UserPort, err = strconv.Atoi(port)
	return out, err
}

// Center inventory and coverage state use the rich representation; execution
// jobs use address-only Edges and require ProbeEdgeAddressFormat at admission.
func (e *NativeEdge) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, compact := fields["edge_type"]; !compact {
		type inventoryEdge NativeEdge
		var value inventoryEdge
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*e = NativeEdge(value)
		return nil
	}
	if len(fields) != 3 || fields["ip_for_client"] == nil || fields["user_port"] == nil {
		return errors.New("probe Edge requires only edge_type, ip_for_client and user_port")
	}
	for _, key := range []string{"edge_type", "ip_for_client", "user_port"} {
		if string(fields[key]) == "null" {
			return errors.New("null probe Edge field")
		}
	}
	var value ProbeEdgeAddress
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	edge := NativeEdge{Transport: value.EdgeType}
	if value.IPForClient != "" || value.UserPort != 0 {
		if net.ParseIP(value.IPForClient) == nil || value.UserPort < 1 || value.UserPort > 65535 {
			return errors.New("invalid probe Edge address")
		}
		edge.Endpoint = net.JoinHostPort(value.IPForClient, strconv.Itoa(value.UserPort))
	}
	if err := edge.Validate(); err != nil {
		return err
	}
	*e = edge
	return nil
}

// ForProbe leaves inventory identity and freshness at the center, including
// candidate edges used to verify AP routing. Case correlation remains intact.
func (c NativeCase) ForProbe() *NativeCase {
	out := c
	addressOnly := func(e NativeEdge) NativeEdge { return NativeEdge{Transport: e.Transport, Endpoint: e.Endpoint} }
	out.Sender = addressOnly(c.Sender)
	out.Receiver = addressOnly(c.Receiver)
	out.APClientIPs = append([]string(nil), c.APClientIPs...)
	if c.Candidates != nil {
		out.Candidates = make([]NativeEdge, len(c.Candidates))
		for i, e := range c.Candidates {
			out.Candidates[i] = addressOnly(e)
		}
	}
	return &out
}

func (j Job) MarshalJSON() ([]byte, error) {
	type jobAlias Job
	if j.EdgeFormat != ProbeEdgeAddressFormat || j.NativeCase == nil {
		return json.Marshal(jobAlias(j))
	}
	type caseAlias NativeCase
	c := j.NativeCase
	sender, err := c.Sender.ProbeAddress()
	if err != nil {
		return nil, err
	}
	receiver, err := c.Receiver.ProbeAddress()
	if err != nil {
		return nil, err
	}
	var candidates []ProbeEdgeAddress
	if c.Candidates != nil {
		candidates = make([]ProbeEdgeAddress, len(c.Candidates))
		for i, e := range c.Candidates {
			candidates[i], err = e.ProbeAddress()
			if err != nil {
				return nil, err
			}
		}
	}
	projection := struct {
		*caseAlias
		Sender     ProbeEdgeAddress   `json:"sender"`
		Receiver   ProbeEdgeAddress   `json:"receiver"`
		Candidates []ProbeEdgeAddress `json:"candidates"`
	}{(*caseAlias)(c), sender, receiver, candidates}
	return json.Marshal(struct {
		*jobAlias
		NativeCase any `json:"native_case"`
	}{(*jobAlias)(&j), projection})
}
