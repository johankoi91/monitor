package hub

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

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
func hasCredentials(c protocol.Credentials) bool {
	return c.AppCertificate != "" || c.SenderToken != "" || c.ReceiverToken != ""
}
func overlayCredentials(old protocol.Credentials, input *protocol.Credentials) protocol.Credentials {
	if input == nil {
		return old
	}
	if input.AppCertificate != "" {
		old.AppCertificate = input.AppCertificate
	}
	if input.SenderToken != "" {
		old.SenderToken = input.SenderToken
	}
	if input.ReceiverToken != "" {
		old.ReceiverToken = input.ReceiverToken
	}
	return old
}
func credentialScopeChanged(a, b *protocol.RTCParameters) bool {
	if a == nil || b == nil {
		return a != b
	}
	return a.AppID != b.AppID || a.AuthMode != b.AuthMode || a.ChannelName != b.ChannelName || b.AuthMode == "token" && (a.SenderUID != b.SenderUID || a.ReceiverUID != b.ReceiverUID)
}
func validateCredentials(p *protocol.RTCParameters, c protocol.Credentials) (protocol.Credentials, error) {
	if p == nil {
		return protocol.Credentials{}, nil
	}
	switch p.AuthMode {
	case "none":
		return protocol.Credentials{}, nil
	case "certificate":
		if len(c.AppCertificate) != 32 {
			return c, errors.New("请在目标服务填写 32 位 App Certificate")
		}
		c.SenderToken = ""
		c.ReceiverToken = ""
	case "token":
		if c.SenderToken == "" || c.ReceiverToken == "" {
			return c, errors.New("请在目标服务填写两端 Token")
		}
		c.AppCertificate = ""
	}
	return c, nil
}

// Promote legacy task-owned settings. Conflicting legacy configurations receive
// separate target identities so no existing task silently changes its RTC path.
func promoteTaskSettings(plan Plan, vault map[string]protocol.Credentials) (Plan, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, err
	}
	var copied Plan
	if err = json.Unmarshal(data, &copied); err != nil {
		return Plan{}, err
	}
	plan = copied
	indexes := map[string]int{}
	legacy := map[string]bool{}
	for i, target := range plan.Targets {
		indexes[target.ID] = i
		if target.Driver == "agora" && target.RTC == nil {
			legacy[target.ID] = true
		}
	}
	groups := map[string]map[[32]byte]string{}
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		baseID := task.TargetID
		if !legacy[baseID] {
			if task.Credentials != nil && hasCredentials(*task.Credentials) {
				return Plan{}, errors.New("RTC 凭证由目标服务管理，请修改目标服务")
			}
			continue
		}
		if task.RTC == nil {
			continue
		}
		config := cloneRTC(task.RTC)
		if err = config.Normalize(); err != nil {
			return Plan{}, err
		}
		c := overlayCredentials(vault[task.ID], task.Credentials)
		keyData, _ := json.Marshal(struct {
			RTC         *protocol.RTCParameters
			Credentials protocol.Credentials
		}{config, c})
		key := sha256.Sum256(keyData)
		if groups[baseID] == nil {
			groups[baseID] = map[[32]byte]string{}
		}
		if id, ok := groups[baseID][key]; ok {
			task.TargetID = id
			task.Credentials = nil
			continue
		}
		index, exists := indexes[baseID]
		if !exists {
			return Plan{}, errors.New("任务目标不存在")
		}
		target := plan.Targets[index]
		if len(groups[baseID]) > 0 {
			suffix := "-migrated-" + strconv.Itoa(len(groups[baseID])+1)
			id := baseID
			if len(id)+len(suffix) > 96 {
				id = id[:96-len(suffix)]
			}
			id += suffix
			for indexes[id] != 0 || (len(plan.Targets) > 0 && plan.Targets[0].ID == id) {
				id = baseID[:min(len(baseID), 70)] + "-" + protocol.ID()[:8]
			}
			target.ID = id
			name := target.Name
			for len(name) > 85 {
				_, size := utf8.DecodeLastRuneInString(name)
				name = name[:len(name)-size]
			}
			target.Name = name + "（迁移配置 " + strconv.Itoa(len(groups[baseID])+1) + "）"
			index = len(plan.Targets)
			plan.Targets = append(plan.Targets, target)
			indexes[id] = index
		}
		target.RTC = config
		if hasCredentials(c) {
			copy := c
			target.Credentials = &copy
		}
		target.CredentialVersion = task.CredentialVersion
		plan.Targets[index] = target
		groups[baseID][key] = target.ID
		task.TargetID = target.ID
		task.Credentials = nil
	}
	return plan, nil
}

func storagePlan(p Plan) Plan {
	copy := Plan{Targets: append([]protocol.Target(nil), p.Targets...), Tasks: append([]protocol.Task(nil), p.Tasks...)}
	for i := range copy.Targets {
		copy.Targets[i].Credentials = nil
		copy.Targets[i].HasCertificate = false
		copy.Targets[i].HasSenderToken = false
		copy.Targets[i].HasReceiverToken = false
	}
	for i := range copy.Tasks {
		copy.Tasks[i].RTC = nil
		copy.Tasks[i].Credentials = nil
		copy.Tasks[i].CredentialVersion = ""
		copy.Tasks[i].HasCertificate = false
		copy.Tasks[i].HasSenderToken = false
		copy.Tasks[i].HasReceiverToken = false
	}
	return copy
}
func (h *Hub) publicTarget(target protocol.Target) protocol.Target {
	c := h.state.TargetCredentials[target.ID]
	target.RTC = cloneRTC(target.RTC)
	target.Credentials = nil
	target.HasCertificate = c.AppCertificate != ""
	target.HasSenderToken = c.SenderToken != ""
	target.HasReceiverToken = c.ReceiverToken != ""
	return target
}

func (h *Hub) loadTargetConfiguration() (bool, error) {
	hadPlan := h.state.Plan != nil
	source := Plan{Targets: h.config.Targets, Tasks: h.config.Tasks}
	if hadPlan {
		source = *h.state.Plan
	}
	dirty := len(h.state.Credentials) > 0 || h.state.TargetCredentials == nil
	for _, task := range source.Tasks {
		if task.RTC != nil {
			dirty = true
		}
	}
	p, err := promoteTaskSettings(source, h.state.Credentials)
	if err != nil {
		return false, err
	}
	p, err = normalizePlan(h.probesLocked(), p)
	if err != nil {
		return false, err
	}
	vault := map[string]protocol.Credentials{}
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
		target.HasSenderToken = false
		target.HasReceiverToken = false
	}
	for i := range p.Tasks {
		for _, target := range p.Targets {
			if p.Tasks[i].TargetID == target.ID {
				p.Tasks[i].RTC = cloneRTC(target.RTC)
				p.Tasks[i].CredentialVersion = target.CredentialVersion
				break
			}
		}
	}
	h.config.Targets, h.config.Tasks = p.Targets, p.Tasks
	h.state.TargetCredentials = vault
	h.state.Credentials = nil
	if hadPlan {
		stored := storagePlan(p)
		h.state.Plan = &stored
	}
	return dirty, nil
}
