package hub

import (
	"time"

	token "github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src/rtctokenbuilder2"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func (h *Hub) Summary() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	mode, status, reason := h.healthLocked()
	configured, enabled := 0, 0
	var lastSuccess *time.Time
	for _, task := range h.config.Tasks {
		if h.target(task.TargetID).Driver != "agora" {
			continue
		}
		configured++
		if h.enabled(task) {
			enabled++
		}
		value := h.effectiveHealth(task, time.Now().UTC())
		if value.LastSuccessAt != nil && (lastSuccess == nil || value.LastSuccessAt.After(*lastSuccess)) {
			lastSuccess = value.LastSuccessAt
		}
	}
	return map[string]any{"mode": mode, "status": status, "reason": reason, "configured_tasks": configured, "enabled_tasks": enabled, "last_success_at": lastSuccess, "storage_available": h.storageOK}
}

func (h *Hub) publicTask(t protocol.Task) protocol.Task {
	t.RTC = nil
	t.Credentials = nil
	t.HasCertificate = false
	t.HasSenderToken = false
	t.HasReceiverToken = false
	t.CredentialVersion = ""
	return t
}

func (h *Hub) pairCommand(r *Run) (protocol.Message, bool) {
	a, b := r.Endpoints["sender"], r.Endpoints["receiver"]
	kind := ""
	if !terminal(r.Status) {
		if (!a.Ready || !b.Ready) && a.Result == nil && b.Result == nil {
			kind = "prepare_run"
		} else if r.Status == "RUNNING" && (a.Result == nil || b.Result == nil) {
			kind = "start_media"
		}
	} else if a.Result == nil || b.Result == nil {
		kind = "cancel_run"
	}
	if kind == "" {
		return protocol.Message{}, false
	}
	if !terminal(r.Status) && time.Now().After(r.Deadline) {
		kind = "cancel_run"
	}
	m := protocol.Msg(kind)
	m.RunID = r.ID
	m.Revision = r.Revision
	m.Role = "pair"
	if kind == "prepare_run" {
		job := a.Job
		job.Role = "pair"
		credentials := h.tokens[r.ID]
		if credentials == nil {
			c := h.state.TargetCredentials[r.TargetID]
			credentials = &protocol.Credentials{}
			if job.RTC.AuthMode == "token" {
				credentials.SenderToken = c.SenderToken
				credentials.ReceiverToken = c.ReceiverToken
			}
			if job.RTC.AuthMode == "certificate" {
				ttl := uint32(time.Until(r.Deadline).Seconds()) + 120
				var err error
				credentials.SenderToken, err = token.BuildTokenWithUid(job.RTC.AppID, c.AppCertificate, job.Channel, job.RTC.SenderUID, token.RolePublisher, ttl, ttl)
				if err == nil {
					credentials.ReceiverToken, err = token.BuildTokenWithUid(job.RTC.AppID, c.AppCertificate, job.Channel, job.RTC.ReceiverUID, token.RolePublisher, ttl, ttl)
				}
				if err != nil {
					now := time.Now().UTC()
					_ = h.change(func(s *state) error {
						v := s.Runs[r.ID]
						v.Status = "ERROR"
						v.Reason = "TOKEN_SIGNING_FAILED"
						v.FinishedAt = &now
						return nil
					})
					return protocol.Message{}, false
				}
			}
			h.tokens[r.ID] = credentials
		}
		job.Credentials = credentials
		m.Job = &job
	}
	return m, true
}
func verifiedEvidence(r *Run, a, b *protocol.Result) bool {
	if !a.Evidence.Connected || !b.Evidence.Connected {
		return false
	}
	p := r.Endpoints["sender"].Job.RTC
	if p == nil {
		return a.Evidence.Published && a.Evidence.SentFrames > 0 && b.Evidence.Subscribed && b.Evidence.ValidSamples >= 16000
	}
	if p.Audio() && (!a.Evidence.Published || a.Evidence.SentFrames == 0 || !b.Evidence.Subscribed || b.Evidence.MediaDurationMS < p.MinAudioMS) {
		return false
	}
	if p.Video() && (!a.Evidence.VideoPublished || a.Evidence.SentVideoFrames == 0 || !b.Evidence.VideoSubscribed || b.Evidence.ReceivedVideoFrames < p.MinVideoFrames) {
		return false
	}
	return true
}
func (h *Hub) healthLocked() (string, string, string) {
	mode := "mock"
	for _, p := range h.sessions {
		if p.Driver == "agora" {
			mode = "agora"
		}
	}
	for _, v := range h.config.Targets {
		if v.Driver == "agora" {
			mode = "agora"
		}
	}
	if mode == "mock" {
		return mode, "UNKNOWN", "SIMULATION_ONLY"
	}
	configured := false
	unknown := false
	failed := false
	for _, t := range h.config.Tasks {
		if !h.enabled(t) || h.target(t.TargetID).Driver != "agora" {
			continue
		}
		configured = true
		current := h.effectiveHealth(t, time.Now().UTC())
		if current.State == "UNHEALTHY" {
			failed = true
		} else if current.State != "HEALTHY" {
			unknown = true
		}
	}
	if failed {
		return mode, "UNHEALTHY", "MEDIA_PROBE_FAILED"
	}
	if !configured {
		return mode, "UNKNOWN", "NO_NATIVE_TASK_CONFIGURED"
	}
	if unknown {
		return mode, "UNKNOWN", "WAITING_FOR_FRESH_MEDIA_EVIDENCE"
	}
	return mode, "HEALTHY", "CONFIGURED_PATHS_VERIFIED"
}

func runOccupiesSlot(r *Run, now time.Time) bool {
	if r.Kind == edgeSweepKind {
		return false
	}
	if !terminal(r.Status) {
		return true
	}
	if r.FinishedAt == nil || now.After(r.FinishedAt.Add(protocol.CleanupGrace+protocol.ResultReportGrace)) {
		return false
	}
	for _, e := range r.Endpoints {
		if e.Result == nil {
			return true
		}
	}
	return false
}

func supportsNativeMedia(s *session) bool {
	if s == nil || s.Driver != "agora" {
		return false
	}
	// Version 0.2 predates explicit capability negotiation and supports both media.
	if len(s.Capabilities) == 0 {
		return true
	}
	audio, video := false, false
	for _, capability := range s.Capabilities {
		if capability == "rtc_audio_v1" {
			audio = true
		}
		if capability == "rtc_video_v1" {
			video = true
		}
	}
	return audio && video
}
