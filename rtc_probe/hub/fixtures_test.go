package hub

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"strings"
)

func newTestHub(dir string, cfg Config, docs Documents) (*Hub, error) {
	h, err := New(dir, Config{MaxConcurrentRuns: cfg.MaxConcurrentRuns}, docs)
	if err != nil {
		return nil, err
	}
	for _, id := range []string{"probe-a", "probe-b", "probe-web"} {
		client := "native"
		secret := strings.Repeat("a", 32)
		if id == "probe-b" {
			secret = strings.Repeat("b", 32)
		}
		if id == "probe-web" {
			secret = strings.Repeat("w", 32)
			client = "web"
		}
		if err = h.EnrollProbe(protocol.ProbeEnrollment{Host: protocol.ProbeHostInfo{ID: "host-" + id, Name: "Server " + id, Address: "127.0.0.1"}, HostSecret: strings.Repeat("host-"+id, 4), RuntimeID: id, RuntimeSecret: secret, ServiceType: "rtc", ClientType: client}, "127.0.0.1"); err != nil {
			return nil, err
		}
	}
	if h.state.Plan == nil {
		if err = h.UpdatePlan(planRevision(h.planLocked()), Plan{Targets: cfg.Targets, Tasks: cfg.Tasks}); err != nil {
			return nil, err
		}
	}
	return h, nil
}
func receiveTest(h *Hub, id string, sess *session, m protocol.Message) (protocol.Message, error) {
	if m.Type == "hello" || m.Type == "heartbeat" {
		r := h.state.Runtimes[id]
		if m.Capabilities == nil && protocol.RealDriver(m.Driver) {
			m.Capabilities = []string{"native_web_duplex_v1", "native_aut_web_interop_v1", protocol.ProbeEdgeAddressCapability, protocol.SharedServiceLogsCapability, "rtc_audio_v1", "rtc_video_v1", "native_routes_v1", protocol.WebMediaCapability, "native_web_interop_v1"}
		}
		m.HostID = r.HostID
		m.ServiceType = r.ServiceType
		m.ClientType = r.ClientType
	}
	return h.Receive(id, sess, m)
}
