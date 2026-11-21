//go:build linux && agora

package agoranative

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
	"github.com/johankoi91/monitor/rtc-probe/internal/routeproof"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func (r *recorder) configureRoutes(connections map[string]*rtc.RtcConnection) error {
	c := r.job.NativeCase
	if c == nil {
		return nil
	}
	for _, role := range []string{"sender", "receiver"} {
		con := connections[role]
		if con == nil {
			continue
		}
		edge := c.Sender
		if role == "receiver" {
			edge = c.Receiver
		}
		scope := c.ParameterScope
		if scope == "" {
			scope = "connection"
		}
		values := map[string]any{"rtc.enable_nasa2": edge.Transport == "aut"}
		if edge.Endpoint != "" {
			values["rtc.vos_list"] = []string{edge.Endpoint}
		}
		params, _ := json.Marshal(values)
		span := r.trace.Begin(role, "CONFIGURE_ROUTE", "GetAgoraParameter", map[string]any{"scope": scope, "connection_id": r.worker + "/" + role}, false)
		var parameter *rtc.AgoraParameter
		if scope == "service" {
			if c.Sender.Endpoint != c.Receiver.Endpoint || c.Sender.Transport != c.Receiver.Transport {
				return errors.New("service route parameters cannot isolate different roles")
			}
			parameter = rtc.GetAgoraParameter()
		} else {
			parameter = con.GetAgoraParameter()
		}
		r.trace.End(span, "object", nil, map[string]any{"present": parameter != nil, "parameter_id": r.worker + "/" + role + "/agora_parameter"})
		if parameter == nil {
			return errors.New("route parameter handle unavailable")
		}
		if err := r.callArgs(role, "CONFIGURE_ROUTE", "SetParameters", map[string]any{"scope": scope, "parameters": string(params), "route": edge}, false, func() int { return parameter.SetParameters(string(params)) }); err != nil {
			return err
		}
	}
	return nil
}
func nativeParameters(p *protocol.RTCParameters, c *protocol.NativeCase) {
	if c == nil {
		return
	}
	p.Private = true
	p.ProxyMode = 1
	p.DisableAut = c.Sender.Transport != "aut" && c.Receiver.Transport != "aut"
	ips := c.APClientIPs
	if len(ips) == 0 {
		ips = []string{c.APEntryPoint}
	}
	p.AccessIPs = nil
	p.AccessDomains = nil
	for _, ip := range ips {
		if net.ParseIP(ip) != nil {
			p.AccessIPs = append(p.AccessIPs, ip)
		} else {
			p.AccessDomains = append(p.AccessDomains, ip)
		}
	}
}

func (r *recorder) routeEvidence(role string, start, end int64) *protocol.RouteEvidence {
	file := os.Getenv("AVOPS_PROBE_ROUTE_LOG")
	if file == "" {
		file = filepath.Join(r.job.WorkDir, "route-capture.jsonl")
	}
	events, err := routeproof.Read(file)
	if err != nil {
		return &protocol.RouteEvidence{Status: "UNVERIFIED", Reason: fmt.Sprintf("ROUTE_CAPTURE_UNAVAILABLE:%T", err), Scope: "process", Observed: []protocol.RoutePeer{}}
	}
	result := routeproof.Assess(r.job, role, events, start, end)
	result.Guarded = captureStatus() == 0 && os.Getenv("AVOPS_PROBE_ROUTE_ALLOW_ENDPOINTS") == strings.Join(r.job.NativeCase.AllowedEndpoints(), ";")
	if !result.Guarded {
		result.Status = "UNVERIFIED"
		result.Reason = "ROUTE_GUARD_NOT_ACTIVE"
	}
	return result
}
