package hub

import (
	"encoding/json"
	"errors"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func cloneRTC(p *protocol.RTCParameters) *protocol.RTCParameters {
	if p == nil {
		return nil
	}
	b, _ := json.Marshal(p)
	var copy protocol.RTCParameters
	_ = json.Unmarshal(b, &copy)
	return &copy
}
func taskRTC(task protocol.Task, source *protocol.RTCParameters) *protocol.RTCParameters {
	p := cloneRTC(source)
	if p == nil {
		return nil
	}
	if protocol.IsInteropClient(task.ClientType) {
		return p
	}
	if task.ClientType != "web" {
		p.WebAccess = nil
		return p
	}
	if p.WebAccess == nil {
		p.WebAccess = &protocol.WebAccessConfiguration{}
	}
	if p.WebAccess.Coverage == nil && p.NativeCoverage != nil {
		coverage := *p.NativeCoverage
		coverage.Excluded = nil
		p.WebAccess.Coverage = &coverage
	}
	p.NativeCoverage = nil
	p.MediaEdges = nil
	_ = p.WebAccess.Normalize()
	p.Codec = 1
	p.Width = 320
	p.Height = 180
	p.FPS = 12
	p.SampleRate = 48000
	return p
}
func hasCredentials(c protocol.AppCredentials) bool {
	return c.AppCertificate != ""
}
func overlayCredentials(old protocol.AppCredentials, input *protocol.AppCredentials) protocol.AppCredentials {
	if input == nil {
		return old
	}
	if input.AppCertificate != "" {
		old.AppCertificate = input.AppCertificate
	}
	return old
}
func credentialScopeChanged(a, b *protocol.RTCParameters) bool {
	if a == nil || b == nil {
		return a != b
	}
	return a.AppID != b.AppID || a.AuthMode != b.AuthMode
}
func validateCredentials(p *protocol.RTCParameters, c protocol.AppCredentials) (protocol.AppCredentials, error) {
	if p == nil {
		return protocol.AppCredentials{}, nil
	}
	switch p.AuthMode {
	case "none":
		return protocol.AppCredentials{}, nil
	case "certificate":
		if len(c.AppCertificate) != 32 {
			return c, errors.New("请在目标服务填写 32 位 App Certificate")
		}
	}
	return c, nil
}

func storagePlan(p Plan) Plan {
	copy := Plan{Targets: append([]protocol.Target(nil), p.Targets...), Tasks: append([]protocol.Task(nil), p.Tasks...)}
	for i := range copy.Targets {
		copy.Targets[i].Credentials = nil
		copy.Targets[i].HasCertificate = false
	}
	for i := range copy.Tasks {
		copy.Tasks[i].RTC = nil
		copy.Tasks[i].Credentials = nil
		copy.Tasks[i].CredentialVersion = ""
		copy.Tasks[i].HasCertificate = false
	}
	return copy
}
func (h *Hub) publicTarget(target protocol.Target) protocol.Target {
	c := h.state.TargetCredentials[target.ID]
	target.RTC = cloneRTC(target.RTC)
	target.Credentials = nil
	target.HasCertificate = c.AppCertificate != ""
	return target
}

func (h *Hub) loadTargetConfiguration() (bool, error) {
	hadPlan := h.state.Plan != nil
	source := Plan{Targets: h.config.Targets, Tasks: h.config.Tasks}
	if hadPlan {
		source = *h.state.Plan
	}
	dirty := h.state.TargetCredentials == nil
	p, err := normalizePlan(h.runtimesLocked(), source)
	if err != nil {
		return false, err
	}
	vault := map[string]protocol.AppCredentials{}
	for i := range p.Targets {
		target := &p.Targets[i]
		c := overlayCredentials(h.state.TargetCredentials[target.ID], target.Credentials)
		c, err = validateCredentials(target.RTC, c)
		if err != nil {
			return false, err
		}
		if hasCredentials(c) && target.CredentialVersion == "" {
			target.CredentialVersion = protocol.ID()
			dirty = true
		}
		vault[target.ID] = c
		target.Credentials = nil
		target.HasCertificate = false
	}
	for i := range p.Tasks {
		for _, target := range p.Targets {
			if p.Tasks[i].TargetID == target.ID {
				p.Tasks[i].RTC = taskRTC(p.Tasks[i], target.RTC)
				p.Tasks[i].CredentialVersion = target.CredentialVersion
				break
			}
		}
	}
	h.config.Targets, h.config.Tasks = p.Targets, p.Tasks
	h.state.TargetCredentials = vault
	if hadPlan {
		stored := storagePlan(p)
		h.state.Plan = &stored
	}
	return dirty, nil
}
