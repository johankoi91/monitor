package center

import (
	"net/http"
	"strings"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

func (s *Server) enrollAgent(w http.ResponseWriter, r *http.Request, observed string) {
	if r.Method != "POST" {
		method(w)
		return
	}
	if r.Header.Get("Origin") != "" {
		failure(w, 403, "ORIGIN_DENIED", "机器登记不接受浏览器 Origin")
		return
	}
	id, token, ok := r.BasicAuth()
	if !ok || id != "agent-enroll" || len(s.AgentEnrollmentToken) < 24 || !equal(token, s.AgentEnrollmentToken) {
		failure(w, 401, "AGENT_ENROLL_DENIED", "Agent 接入凭证无效")
		return
	}
	var input struct {
		ID     string         `json:"agent_id"`
		Secret string         `json:"agent_secret"`
		Host   model.HostInfo `json:"host_info"`
	}
	if !decode(w, r, &input) {
		return
	}

	reused := input.ID == s.AdminID || equal(input.Secret, s.AdminSecret) || equal(input.Secret, s.AgentEnrollmentToken) || equal(input.Secret, s.Store.config.Notifications.Secret)
	if s.Probes != nil {
		reused = reused || s.Probes.ProbeCredentialMatches(input.Secret)
	}
	reused = reused || equal(input.Secret, s.ProbeEnrollmentToken)
	if reused || strings.TrimSpace(input.ID) == "" {
		failure(w, 400, "AGENT_ENROLL_INVALID", "Agent 身份与密钥须独立")
		return
	}
	if err := s.Store.EnrollAgent(input.ID, input.Secret, input.Host); err != nil {
		failure(w, 409, "AGENT_ENROLL_FAILED", err.Error())
		return
	}
	send(w, 200, map[string]any{"agent_id": input.ID, "observed_address": observed})
}
