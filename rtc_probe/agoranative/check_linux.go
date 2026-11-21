//go:build linux && agora

package agoranative

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
	"github.com/johankoi91/monitor/rtc-probe/internal/telemetry"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// SelfCheck exercises the original SDK without Connect. Set the output directory
// to retain the integration project's complete API trace for review.
func SelfCheck() error {
	for _, name := range []string{"send_audio_16k_1ch.pcm", "send_video_cif_352x288.yuv"} {
		info, err := os.Stat(filepath.Join(DemoDirectory, name))
		if err != nil || info.Size() == 0 {
			return fmt.Errorf("demo media missing: %s", name)
		}
	}
	dir := os.Getenv("AVOPS_PROBE_SELF_CHECK_DIR")
	preserve := dir != ""
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "rtc-native-check-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	p := &protocol.RTCParameters{AppID: strings.Repeat("0", 32), AuthMode: "none"}
	if err := p.Normalize(); err != nil {
		return err
	}
	job := protocol.Job{RunID: "self-check", TaskID: "self-check", RuntimeID: "self-check", Revision: "local", Target: protocol.Target{ID: "local-sdk-check", Driver: "agora"}, RTC: p, WorkDir: dir}
	r := &recorder{job: job, worker: protocol.ID(), at: time.Now()}
	var err error
	r.trace, err = telemetry.New(dir, r.worker, nil, nil)
	if err != nil {
		return err
	}
	r.trace.Bind(job)
	var connections []*rtc.RtcConnection
	initialized := false
	err = func() error {
		cfg := rtc.NewAgoraServiceConfig()
		cfg.AppId = p.AppID
		cfg.EnableVideo = true
		cfg.AudioScenario = rtc.AudioScenarioDefault
		cfg.IdleMode = false
		cfg.LogPath = filepath.Join(dir, "sdk.log")
		cfg.ConfigDir = dir
		cfg.DataDir = dir
		if err := r.callArgs("sender", "INITIALIZE", "Initialize", map[string]any{"config": snapshotObject(cfg)}, false, func() int { return rtc.Initialize(cfg) }); err != nil {
			return err
		}
		initialized = true
		for i, role := range []string{"sender", "receiver"} {
			pub := rtc.NewRtcConPublishConfig()
			pub.AudioScenario = rtc.AudioScenarioDefault
			pub.IsPublishAudio = i == 0
			pub.IsPublishVideo = i == 0
			pub.VideoPublishType = rtc.VideoPublishTypeYuv
			cc := &rtc.RtcConnectionConfig{ClientRole: rtc.ClientRoleBroadcaster, ChannelProfile: rtc.ChannelProfileLiveBroadcasting}
			span := r.trace.Begin(role, "CREATE_CONNECTION", "NewRtcConnection", map[string]any{"config": snapshotObject(cc), "publish_config": snapshotObject(pub)}, false)
			con := rtc.NewRtcConnection(cc, pub)
			r.trace.End(span, "object", nil, map[string]any{"created": con != nil})
			if con == nil {
				return fmt.Errorf("native NewRtcConnection %d", i)
			}
			connections = append(connections, con)
			if err := r.call(role, "REGISTER", "RegisterObserver", func() int { return con.RegisterObserver(&rtc.RtcConnectionObserver{}) }); err != nil {
				return err
			}
		}
		video := &rtc.VideoFrameObserver{OnRenderVideoFrame: func(string, uint32, *rtc.VideoFrame) bool { return true }, GetVideoFormatPreference: func() rtc.VideoPixelFormat {
			r.callback("receiver", "REGISTER", "GetVideoFormatPreference", "RegisterVideoFrameObserver", map[string]any{"return_value": int(rtc.VideoPixelI420)}, false)
			return rtc.VideoPixelI420
		}}
		if err := r.call("receiver", "REGISTER", "RegisterVideoFrameObserver", func() int { return connections[1].RegisterVideoFrameObserver(video) }); err != nil {
			return err
		}
		encoder := rtc.NewVideoEncoderConfiguration()
		encoder.Width = 352
		encoder.Height = 288
		encoder.Framerate = 15
		encoder.Bitrate = 400
		return r.callArgs("sender", "CONFIGURE_VIDEO", "SetVideoEncoderConfiguration", map[string]any{"config": snapshotObject(encoder)}, false, func() int { return connections[0].SetVideoEncoderConfiguration(encoder) })
	}()
	for i, con := range connections {
		role := []string{"sender", "receiver"}[i]
		span := r.trace.Begin(role, "CLEANUP", "Connection.Release", r.arguments(role, "Connection.Release"), false)
		con.Release()
		r.trace.End(span, "void", nil, nil)
	}
	if initialized {
		if cleanupErr := r.call("sender", "CLEANUP", "Service.Release", rtc.Release); err == nil {
			err = cleanupErr
		}
	}
	traceErr := r.trace.Close()
	if err != nil {
		return err
	}
	if traceErr != nil {
		return traceErr
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"driver": "agora", "go_sdk": "2.6.6", "native_sdk": "4.6.31.135", "service_instances": 1, "connections": len(connections), "video_observer_registered": true, "video_encoder_configured": true, "demo_pcm": true, "demo_yuv": true, "rtc_connect_called": false, "api_trace_complete": true, "api_trace_retained": preserve})
}
