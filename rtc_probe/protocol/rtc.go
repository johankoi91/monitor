package protocol

import (
	"errors"
	"regexp"
)

// RTCParameters contains public target settings and execution snapshots. Credentials are write-only.
type RTCParameters struct {
	MediaEdges      *MediaEdgeRange `json:"media_edges,omitempty"`
	AppID           string          `json:"app_id"`
	Template        string          `json:"template"`
	Source          string          `json:"source"`
	AuthMode        string          `json:"auth_mode"`
	ChannelName     string          `json:"channel_name,omitempty"`
	ChannelPrefix   string          `json:"channel_prefix"`
	SenderUID       uint32          `json:"sender_uid"`
	ReceiverUID     uint32          `json:"receiver_uid"`
	SampleRate      int             `json:"sample_rate"`
	AudioChannels   int             `json:"audio_channels"`
	MediaDurationMS int             `json:"media_duration_ms"`
	MinAudioMS      int             `json:"min_audio_ms"`
	MinVideoFrames  int             `json:"min_video_frames"`
	Width           int             `json:"video_width"`
	Height          int             `json:"video_height"`
	FPS             int             `json:"video_fps"`
	Bitrate         int             `json:"video_bitrate_kbps"`
	Codec           int             `json:"video_codec"`
	Private         bool            `json:"private_deployment"`
	AccessIPs       []string        `json:"access_ips,omitempty"`
	AccessDomains   []string        `json:"access_domains,omitempty"`
	VerifyDomain    string          `json:"verify_domain,omitempty"`
	ProxyMode       int             `json:"proxy_mode"`
	DisableAut      bool            `json:"disable_aut"`
	AreaCode        uint32          `json:"area_code"`
	DomainLimit     int             `json:"domain_limit"`
	AudioScenario   int             `json:"audio_scenario"`
	AudioProfile    int             `json:"audio_profile"`
	ChannelProfile  int             `json:"channel_profile"`
	ChannelType     int             `json:"channel_type"`
	LogSizeKB       int             `json:"log_size_kb"`
	LogLevel        int             `json:"log_level"`
}
type Credentials struct {
	AppCertificate string `json:"app_certificate,omitempty"`
	SenderToken    string `json:"sender_token,omitempty"`
	ReceiverToken  string `json:"receiver_token,omitempty"`
}

func (p RTCParameters) Audio() bool { return p.Template == "audio" || p.Template == "audio_video" }
func (p RTCParameters) Video() bool { return p.Template == "video" || p.Template == "audio_video" }
func (p *RTCParameters) Normalize() error {
	if !regexp.MustCompile(`^[a-fA-F0-9]{32}$`).MatchString(p.AppID) {
		return errors.New("AppID 须为 32 位十六进制项目标识")
	}
	if p.MediaEdges != nil {
		if p.MediaEdges.Enabled != nil && !*p.MediaEdges.Enabled {
			p.MediaEdges = nil
		} else {
			if err := p.MediaEdges.Normalize(); err != nil {
				return err
			}
			enabled := true
			p.MediaEdges.Enabled = &enabled
		}
	}

	p.Template = "audio_video"
	p.Source = "demo"
	if p.AuthMode == "" {
		p.AuthMode = "certificate"
	}
	if p.AuthMode != "none" && p.AuthMode != "token" && p.AuthMode != "certificate" {
		return errors.New("鉴权方式无效")
	}
	if p.ChannelPrefix == "" {
		p.ChannelPrefix = "probe"
	}
	if !ValidID(p.ChannelPrefix) || len(p.ChannelPrefix) > 24 {
		return errors.New("频道前缀仅允许 24 位字母数字及下划线/连字符")
	}
	if p.AuthMode == "token" && p.ChannelName == "" {
		return errors.New("使用预签发 Token 时须填写其绑定的固定测试频道")
	}
	if len(p.ChannelName) > 64 {
		return errors.New("固定频道过长")
	}
	if p.AuthMode != "token" {
		p.ChannelName = ""
	}
	p.ChannelProfile = 1
	p.ChannelType = 0
	p.AudioScenario = 0
	p.AudioProfile = 0
	if p.SenderUID == 0 {
		p.SenderUID = 10001
	}
	if p.ReceiverUID == 0 {
		p.ReceiverUID = 10002
	}
	if p.SenderUID == p.ReceiverUID {
		return errors.New("发送与接收 UID 必须不同")
	}
	p.SampleRate = 16000
	p.AudioChannels = 1
	p.MediaDurationMS = 5000
	p.MinAudioMS = 1000
	p.MinVideoFrames = 5
	p.Width = 352
	p.Height = 288
	p.FPS = 15
	p.Bitrate = 400
	p.Codec = 2
	if p.Private && len(p.AccessIPs)+len(p.AccessDomains) == 0 {
		return errors.New("私有化模式须填写接入 IP 或域名")
	}
	if len(p.AccessIPs)+len(p.AccessDomains) > 16 || p.ProxyMode < 0 || p.ProxyMode > 1 {
		return errors.New("私有化接入配置无效")
	}
	if p.AreaCode == 0 {
		p.AreaCode = 0xffffffff
	}
	if p.LogSizeKB == 0 {
		p.LogSizeKB = 5120
	}
	if p.LogLevel == 0 {
		p.LogLevel = 1
	}
	if p.LogSizeKB < 1024 || p.LogSizeKB > 20480 {
		return errors.New("SDK 日志大小须为 1024–20480 KB")
	}
	return nil
}
