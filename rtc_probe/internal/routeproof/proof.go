// Package routeproof verifies SDK socket metadata against the frozen route plan.
package routeproof

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type Observation struct {
	At        int64  `json:"ts_ns"`
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Bytes     int64  `json:"bytes"`
}

func Read(path string) ([]Observation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 16<<20 {
		return nil, fmt.Errorf("route metadata limit exceeded")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	var out []Observation
	for {
		var value Observation
		if err = decoder.Decode(&value); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
}
func Assess(job protocol.Job, role string, events []Observation, mediaStart, mediaEnd int64) *protocol.RouteEvidence {
	c := job.NativeCase
	out := &protocol.RouteEvidence{Status: "UNVERIFIED", Reason: "MEDIA_NOT_STARTED", Scope: "process", Observed: []protocol.RoutePeer{}}
	if c == nil {
		return out
	}
	expected := c.Sender
	if role == "receiver" {
		expected = c.Receiver
	}
	out.Expected = expected.Endpoint
	if job.Role == "pair" {
		out.Scope = "shared_process"
	}
	peers := map[string]*protocol.RoutePeer{}
	candidates := map[string]string{}
	for _, edge := range c.Candidates {
		candidates[edge.Endpoint] = edge.Transport
	}
	candidates[c.Sender.Endpoint] = c.Sender.Transport
	candidates[c.Receiver.Endpoint] = c.Receiver.Transport
	apPeers := map[string]*protocol.RoutePeer{}
	apIPs := map[string]bool{}
	for _, ip := range c.APClientIPs {
		apIPs[ip] = true
	}
	if len(apIPs) == 0 {
		apIPs[c.APEntryPoint] = true
	}
	undiscovered := false
	for _, event := range events {
		endpoint := net.JoinHostPort(event.IP, strconv.Itoa(event.Port))
		_, mediaCandidate := candidates[endpoint]
		// The SDK reports to the target deployment's event_collector on 4301.
		// Its traffic cannot establish a media route or an AP exchange.
		if apIPs[event.IP] && event.Port == protocol.SDKEventCollectorPort && !mediaCandidate {
			continue
		}
		if event.Direction == "blocked" {
			endpoint := net.JoinHostPort(event.IP, strconv.Itoa(event.Port))
			if c.Kind == "AP_JOIN" && apIPs[event.IP] && event.Port != 53 && event.Port != 443 && event.Port != 8003 && event.Port != 8443 {
				if _, known := candidates[endpoint]; !known {
					undiscovered = true
				}
			}
			continue
		}
		if apIPs[event.IP] && (event.Port == 8003 || event.Port == 8443) {
			peer := apPeers[event.IP]
			if peer == nil {
				peer = &protocol.RoutePeer{}
				apPeers[event.IP] = peer
			}
			if event.Direction == "tx" {
				peer.SentBytes += event.Bytes
			} else if event.Direction == "rx" {
				peer.ReceivedBytes += event.Bytes
			}
		}
		if mediaStart == 0 || event.At < mediaStart || event.At > mediaEnd || event.Port == 53 || (event.Port == 8003 || event.Port == 8443) {
			continue
		}
		// Known media peers, or UDP traffic to any other target, are relevant to fallback detection.
		if _, known := candidates[endpoint]; !known && (event.Protocol != "udp" || event.IP == c.APGroup) {
			continue
		}
		peer := peers[endpoint+"/"+event.Protocol]
		if peer == nil {
			peer = &protocol.RoutePeer{Endpoint: endpoint, Protocol: event.Protocol}
			peers[endpoint+"/"+event.Protocol] = peer
		}
		if event.Direction == "tx" {
			peer.SentBytes += event.Bytes
		} else if event.Direction == "rx" {
			peer.ReceivedBytes += event.Bytes
		}
	}
	for _, peer := range apPeers {
		if peer.SentBytes > 0 && peer.ReceivedBytes > 0 {
			out.APObserved = true
		}
	}
	verified := false
	var duplexSent, duplexReceived int64
	for _, peer := range peers {
		out.Observed = append(out.Observed, *peer)
		// Sustained media-window exchange plus valid decoded media is required by the caller.
		same := c.Sender.Endpoint == c.Receiver.Endpoint && c.Sender.Transport == c.Receiver.Transport
		carrying := peer.SentBytes >= 16<<10 && peer.ReceivedBytes > 0 && peer.SentBytes >= peer.ReceivedBytes*2
		if role == "receiver" {
			carrying = peer.ReceivedBytes >= 16<<10 && peer.SentBytes > 0 && peer.ReceivedBytes >= peer.SentBytes*2
		}
		if job.Role == "pair" && same {
			carrying = (peer.SentBytes >= 16<<10 || peer.ReceivedBytes >= 16<<10) && peer.SentBytes > 0 && peer.ReceivedBytes > 0
		}
		if job.Duplex() {
			carrying = peer.SentBytes >= 16<<10 || peer.ReceivedBytes >= 16<<10
		}
		if !carrying {
			continue
		}
		kind, known := candidates[peer.Endpoint]
		if !known || kind != expected.Transport || expected.Endpoint != "" && peer.Endpoint != expected.Endpoint {
			out.Status = "MISMATCH"
			out.Reason = "UNEXPECTED_MEDIA_EDGE"
			continue
		}
		verified = true
		if job.Duplex() {
			duplexSent += peer.SentBytes
			duplexReceived += peer.ReceivedBytes
		}
	}
	if job.Duplex() {
		verified = duplexSent >= 16<<10 && duplexReceived >= 16<<10
		out.Scope = "bidirectional_media"
	}
	sort.Slice(out.Observed, func(i, j int) bool {
		return out.Observed[i].Endpoint+out.Observed[i].Protocol < out.Observed[j].Endpoint+out.Observed[j].Protocol
	})
	if out.Status == "MISMATCH" {
		return out
	}
	if undiscovered {
		out.Status = "UNVERIFIED"
		out.Reason = "AP_ALLOCATED_UNDISCOVERED_ENDPOINT"
		return out
	}
	if mediaStart == 0 {
		return out
	}
	if job.Role == "pair" {
		out.Scope = "media_direction"
	}
	if !verified {
		out.Reason = "MEDIA_ROUTE_NOT_OBSERVED"
		return out
	}
	if c.Kind == "AP_JOIN" && !out.APObserved {
		out.Reason = "AP_REQUEST_NOT_OBSERVED"
		return out
	}
	out.Status = "VERIFIED"
	out.Reason = ""
	return out
}
