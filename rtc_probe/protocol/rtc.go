package protocol

import (
	"errors"
	"regexp"
)

// RTCParameters contains public target settings and execution snapshots. Credentials are write-only.
type RTCParameters struct {
	NativeCoverage  *NativeCoverageConfig `json:"native_coverage,omitempty"`
	MediaEdges      *MediaEdgeRange       `json:"media_edges,omitempty"`
	AppID           string                `json:"app_id"`
	Template        string                `json:"template"`
	Source          string                `json:"source"`
	AuthMode        string                `json:"auth_mode"`
	SampleRate      int                   `json:"sample_rate"`
	AudioChannels   int                   `json:"audio_channels"`
	MediaDurationMS int                   `json:"media_duration_ms"`
	MinAudioMS      int                   `json:"min_audio_ms"`
	MinVideoFrames  int                   `json:"min_video_frames"`
	Width           int                   `json:"video_width"`
	Height          int                   `json:"video_height"`
	FPS             int                   `json:"video_fps"`
	Bitrate         int                   `json:"video_bitrate_kbps"`
	Codec           int                   `json:"video_codec"`
	Private         bool                  `json:"private_deployment"`
	AccessIPs       []string              `json:"access_ips,omitempty"`
	AccessDomains   []string              `json:"access_domains,omitempty"`
	VerifyDomain    string                `json:"verify_domain,omitempty"`
	ProxyMode       int                   `json:"proxy_mode"`
	DisableAut      bool                  `json:"disable_aut"`
	AreaCode        uint32                `json:"area_code"`
	DomainLimit     int                   `json:"domain_limit"`
	AudioScenario   int                   `json:"audio_scenario"`
	AudioProfile    int                   `json:"audio_profile"`
	ChannelProfile  int                   `json:"channel_profile"`
	ChannelType     int                   `json:"channel_type"`
	LogSizeKB       int                   `json:"log_size_kb"`
	LogLevel        int                   `json:"log_level"`
}

const SenderUID uint32 = 10001
const ReceiverUID uint32 = 10002

// AppCredentials is the only credential accepted in target configuration.
type AppCredentials struct {
	AppCertificate string `json:"app_certificate,omitempty"`
}

// Credentials carries generated session tokens over the private probe channel.
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

	if p.NativeCoverage != nil {
		if err := p.NativeCoverage.Normalize(); err != nil {
			return err
		}
		p.MediaEdges = nil
		p.Private = true
		p.ProxyMode = 1
		p.DisableAut = false
		p.AccessIPs = nil
		p.AccessDomains = nil
		seen := map[string]bool{}
		for _, binding := range p.NativeCoverage.Groups {
			for _, ip := range binding.EntryPoints {
				if !seen[ip] {
					p.AccessIPs = append(p.AccessIPs, ip)
					seen[ip] = true
				}
			}
		}
	}
	p.Template = "audio_video"
	p.Source = "demo"
	if p.AuthMode == "" {
		p.AuthMode = "certificate"
	}
	if p.AuthMode != "none" && p.AuthMode != "certificate" {
		return errors.New("鉴权方式无效")
	}
	p.ChannelProfile = 1
	p.ChannelType = 0
	p.AudioScenario = 0
	p.AudioProfile = 0
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
