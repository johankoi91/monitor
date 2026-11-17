package protocol

import (
	"errors"
	"net"
	"sort"
	"strconv"
)

type NativeEdge struct {
	Transport     string `json:"transport"`
	Endpoint      string `json:"endpoint"`
	AgentID       string `json:"agent_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	SnapshotID    string `json:"snapshot_id,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
}
type NativeCase struct {
	APClientIPs     []string     `json:"ap_client_ips,omitempty"`
	ExecutionModel  string       `json:"execution_model,omitempty"`
	Unavailable     string       `json:"unavailable,omitempty"`
	Key             string       `json:"case_key"`
	Kind            string       `json:"case_kind"`
	CoverageVersion string       `json:"coverage_version"`
	APGroup         string       `json:"ap_group"`
	APEntryPoint    string       `json:"ap_entrypoint,omitempty"`
	Sender          NativeEdge   `json:"sender"`
	Receiver        NativeEdge   `json:"receiver"`
	Candidates      []NativeEdge `json:"candidates"`
	ParameterScope  string       `json:"parameter_scope,omitempty"`
}

func (e NativeEdge) Validate() error {
	if e.Transport != "udp" && e.Transport != "aut" {
		return errors.New("native transport must be udp or aut")
	}
	if e.Endpoint != "" {
		host, port, err := net.SplitHostPort(e.Endpoint)
		n, e2 := strconv.Atoi(port)
		if err != nil || e2 != nil || net.ParseIP(host) == nil || n < 1 || n > 65535 {
			return errors.New("invalid native edge endpoint")
		}
	}
	return nil
}
func (c NativeCase) Validate() error {
	if c.Kind != "AP_JOIN" && c.Kind != "DIRECT_JOIN" && c.Kind != "CROSS_EDGE_MEDIA" {
		return errors.New("invalid native case kind")
	}
	if c.Sender.Validate() != nil || c.Receiver.Validate() != nil {
		return errors.New("invalid native role route")
	}
	if c.Kind == "AP_JOIN" {
		if c.APEntryPoint == "" || c.Sender.Endpoint != "" || c.Receiver.Endpoint != "" {
			return errors.New("AP case must use one AP entry and no fixed edge")
		}
	} else if c.Sender.Endpoint == "" || c.Receiver.Endpoint == "" {
		return errors.New("direct case requires both edge routes")
	}
	if c.ParameterScope != "" && c.ParameterScope != "connection" && c.ParameterScope != "service" {
		return errors.New("invalid native parameter scope")
	}
	return nil
}

type RoutePeer struct {
	Endpoint      string `json:"endpoint"`
	Protocol      string `json:"protocol"`
	SentBytes     int64  `json:"sent_bytes"`
	ReceivedBytes int64  `json:"received_bytes"`
}
type RouteEvidence struct {
	Guarded    bool        `json:"guarded"`
	Status     string      `json:"status"`
	Reason     string      `json:"reason,omitempty"`
	Scope      string      `json:"scope"`
	Expected   string      `json:"expected,omitempty"`
	Observed   []RoutePeer `json:"observed"`
	APObserved bool        `json:"ap_observed"`
}

type APBinding struct {
	Group       string   `json:"group"`
	EntryPoints []string `json:"entrypoints"`
}
type NativeCoverageConfig struct {
	Groups    []APBinding `json:"groups"`
	CrossEdge bool        `json:"cross_edge"`
	Excluded  []string    `json:"excluded,omitempty"`
}
type NativeInventory struct {
	Groups     map[string][]NativeEdge `json:"groups"`
	Unresolved int                     `json:"unresolved"`
}

func (c *NativeCoverageConfig) Normalize() error {
	if len(c.Groups) == 0 || len(c.Groups) > 16 {
		return errors.New("请选择 1–16 个 AP 分组")
	}
	groups := map[string]bool{}
	for i := range c.Groups {
		group := &c.Groups[i]
		if net.ParseIP(group.Group) == nil || groups[group.Group] || len(group.EntryPoints) == 0 || len(group.EntryPoints) > 16 {
			return errors.New("AP 分组及客户端入口无效")
		}
		groups[group.Group] = true
		seen := map[string]bool{}
		for _, entry := range group.EntryPoints {
			if net.ParseIP(entry) == nil || seen[entry] {
				return errors.New("每个 AP 入口须为不重复的可达 IP")
			}
			seen[entry] = true
		}
	}
	if len(c.Excluded) > 128 {
		return errors.New("移出覆盖的实例数量过多")
	}
	return nil
}
func (e NativeEdge) Key() string {
	if e.AgentID == "" && e.ContainerName == "" {
		return e.Transport + "/" + e.Endpoint
	}
	return e.AgentID + "/" + e.ContainerName
}

type NativeMember struct {
	Group string     `json:"group"`
	Edge  NativeEdge `json:"edge"`
}

const MaxWorkerCapacity = 64

type NativePlan struct {
	Topology string         `json:"topology,omitempty"`
	Phase    string         `json:"phase"`
	Version  string         `json:"coverage_version"`
	Members  []NativeMember `json:"members"`
	Cases    []NativeCase   `json:"cases"`
	Issues   []string       `json:"issues"`
}

// SDKEventCollectorPort is auxiliary reporting, not a media Edge endpoint.
const SDKEventCollectorPort = 4301

// AllowedEndpoints constrains frontend routes while allowing AP and SDK reporting.
func (c NativeCase) AllowedEndpoints() []string {
	seen := map[string]bool{}
	{
		ips := c.APClientIPs
		if len(ips) == 0 {
			ips = []string{c.APEntryPoint}
		}
		for _, ip := range ips {
			seen[net.JoinHostPort(ip, "8003")] = true
			seen[net.JoinHostPort(ip, "8443")] = true
			seen[net.JoinHostPort(ip, strconv.Itoa(SDKEventCollectorPort))] = true
		}
	}
	if c.Kind == "AP_JOIN" {
		for _, edge := range c.Candidates {
			if edge.Transport == c.Sender.Transport {
				seen[edge.Endpoint] = true
			}
		}
	} else {
		seen[c.Sender.Endpoint] = true
		seen[c.Receiver.Endpoint] = true
	}
	var endpoints []string
	for endpoint := range seen {
		if endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	sort.Strings(endpoints)
	return endpoints
}
