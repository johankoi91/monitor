package center

import (
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"net/http"
)

func (s *Server) enrollProbe(w http.ResponseWriter, r *http.Request, observed string) {
	if r.Method != "POST" {
		method(w)
		return
	}
	if r.Header.Get("Origin") != "" {
		failure(w, 403, "ORIGIN_DENIED", "机器上报不接受浏览器 Origin")
		return
	}
	id, token, ok := r.BasicAuth()
	if !ok || id != "probe-enroll" || len(s.ProbeEnrollmentToken) < 24 || !equal(token, s.ProbeEnrollmentToken) {
		failure(w, 401, "PROBE_ENROLL_DENIED", "探测服务接入凭证无效")
		return
	}
	if s.Probes == nil {
		failure(w, 503, "PROBE_CENTER_UNAVAILABLE", "探测服务中心不可用")
		return
	}
	var input protocol.ProbeEnrollment
	if !decode(w, r, &input) {
		return
	}
	reused := input.RuntimeID == s.AdminID || input.Host.ID == s.AdminID
	for _, secret := range []string{input.RuntimeSecret, input.HostSecret} {
		reused = reused || equal(secret, s.AdminSecret) || equal(secret, s.AgentEnrollmentToken) || equal(secret, s.ProbeEnrollmentToken) || equal(secret, s.Store.config.Notifications.Secret) || s.Store.AgentCredentialMatches(secret)
	}
	if _, exists := s.Store.Agent(input.RuntimeID); exists {
		reused = true
	}
	if _, exists := s.Store.Agent(input.Host.ID); exists {
		reused = true
	}
	if reused {
		failure(w, 400, "PROBE_ENROLL_INVALID", "服务器与客户端身份、密钥须独立")
		return
	}
	if err := s.Probes.EnrollProbe(input, observed); err != nil {
		failure(w, 409, "PROBE_ENROLL_FAILED", err.Error())
		return
	}
	send(w, 200, protocol.ProbeEnrollmentReply{HostID: input.Host.ID, RuntimeID: input.RuntimeID, ObservedAddress: observed})
}
