//go:build linux && agora

// Package agoranative runs one service with two real RTC connections per job.
package agoranative

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/telemetry"
)

const DemoDirectory = "/opt/rtc-probe/test_data"

var errLocalMedia = errors.New("LOCAL_MEDIA_UNAVAILABLE")

type recorder struct {
	trace   *telemetry.Recorder
	mu      sync.Mutex
	job     protocol.Job
	worker  string
	at      time.Time
	out     chan protocol.Message
	closed  bool
	dropped atomic.Uint64
}

func (r *recorder) message(kind, role string) protocol.Message {
	m := protocol.Msg(kind)
	m.RunID = r.job.RunID
	m.Revision = r.job.Revision
	m.Role = role
	return m
}
func (r *recorder) event(role, kind, stage, name, call string, code *int, fields map[string]any, duration int64) {
	r.trace.Event(role, kind, stage, name, call, code, fields, time.Duration(duration)*time.Millisecond)
}
func (r *recorder) forward(e protocol.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	role, _ := e.Fields["endpoint_role"].(string)
	m := r.message("telemetry_batch", role)
	m.Events = []protocol.Event{e}
	select {
	case r.out <- m:
	default:
		r.dropped.Add(1)
	}
}
func (r *recorder) call(role, stage, name string, fn func() int) error {
	return r.callArgs(role, stage, name, r.arguments(role, name), false, fn)
}
func (r *recorder) callArgs(role, stage, name string, args map[string]any, stream bool, fn func() int) error {
	span := r.trace.Begin(role, stage, name, args, stream)
	code := fn()
	r.trace.End(span, "int", &code, nil)
	if code != 0 {
		return fmt.Errorf("%s:%d", name, code)
	}
	return nil
}
func (r *recorder) callback(role, stage, name, api string, args map[string]any, stream bool) {
	r.trace.Callback(role, stage, name, api, args, stream)
}

type stats struct {
	mu    sync.Mutex
	value protocol.Evidence
}

func (s *stats) update(fn func(*protocol.Evidence)) { s.mu.Lock(); defer s.mu.Unlock(); fn(&s.value) }
func (s *stats) snapshot() protocol.Evidence        { s.mu.Lock(); defer s.mu.Unlock(); return s.value }
func sanitize(s string, c *protocol.Credentials) string {
	if c != nil {
		for _, secret := range []string{c.SenderToken, c.ReceiverToken, c.AppCertificate} {
			if secret != "" {
				s = strings.ReplaceAll(s, secret, "[REDACTED]")
			}
		}
	}
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

// mediaPolicy is used by isolated acceptance tests; production always sends both.
type mediaPolicy struct{ omitAudio, omitVideo bool }

func Run(parent context.Context, input io.Reader, output io.Writer) error {
	return run(parent, input, output, mediaPolicy{})
}
func run(parent context.Context, input io.Reader, output io.Writer, policy mediaPolicy) error {
	d := json.NewDecoder(io.LimitReader(input, 1<<20))
	var initial protocol.Message
	if err := d.Decode(&initial); err != nil {
		return err
	}
	if initial.Job == nil || initial.Job.Role != "pair" || initial.Job.RTC == nil {
		return errors.New("native pair assignment required")
	}
	job := *initial.Job
	p := *job.RTC
	if err := p.Normalize(); err != nil {
		return err
	}
	if p.MediaEdges != nil && (job.EdgeEndpoint == "" || !p.MediaEdges.Contains(job.EdgeEndpoint)) {
		return errors.New("selected UDP edge outside task range")
	}
	if p.MediaEdges == nil && job.EdgeEndpoint != "" {
		return errors.New("UDP edge not configured by task")
	}
	if job.Target.Driver != "agora" || !protocol.ValidID(job.RunID) || job.WorkDir == "" {
		return errors.New("invalid native assignment")
	}
	ctx, cancel := context.WithDeadline(parent, job.Deadline)
	defer cancel()
	r := &recorder{job: job, worker: protocol.ID(), at: time.Now(), out: make(chan protocol.Message, 256)}
	secretValues := []string{}
	if job.Credentials != nil {
		secretValues = []string{job.Credentials.SenderToken, job.Credentials.ReceiverToken, job.Credentials.AppCertificate}
	}
	var traceErr error
	r.trace, traceErr = telemetry.New(job.WorkDir, r.worker, secretValues, r.forward)
	if traceErr != nil {
		return fmt.Errorf("API trace storage unavailable: %w", traceErr)
	}
	r.trace.Bind(job)
	written := make(chan error, 1)
	go func() {
		enc := json.NewEncoder(output)
		var first error
		for m := range r.out {
			if first == nil {
				if err := enc.Encode(m); err != nil {
					first = err
					cancel()
				}
			}
		}
		written <- first
	}()
	start := make(chan struct{}, 1)
	go func() {
		for {
			var m protocol.Message
			if d.Decode(&m) != nil {
				cancel()
				return
			}
			if m.RunID != job.RunID || m.Revision != job.Revision {
				continue
			}
			if m.Type == "cancel_run" {
				cancel()
			}
			if m.Type == "start_media" {
				select {
				case start <- struct{}{}:
				default:
				}
			}
		}
	}()
	values := map[string]*stats{"sender": {}, "receiver": {}}
	connections := map[string]*rtc.RtcConnection{}
	joined := map[string]chan struct{}{"sender": make(chan struct{}, 1), "receiver": make(chan struct{}, 1)}
	disconnected := map[string]chan struct{}{"sender": make(chan struct{}, 1), "receiver": make(chan struct{}, 1)}
	live := map[string]*atomic.Bool{"sender": {}, "receiver": {}}
	failures := make(chan string, 8)
	cleaning := atomic.Bool{}
	streaming := atomic.Bool{}
	for _, role := range []string{"sender", "receiver"} {
		r.event(role, "worker_event", "INITIALIZE", "worker_started", "", nil, map[string]any{"driver": "agora", "shared_service": true, "connections": 2}, 0)
	}
	phase := "INITIALIZE"
	phaseAt := time.Now()
	var stages []protocol.StageTiming
	advance := func(next string) {
		if phase == next {
			return
		}
		now := time.Now()
		stages = append(stages, protocol.StageTiming{Name: phase, StartedMS: phaseAt.Sub(r.at).Milliseconds(), DurationMS: now.Sub(phaseAt).Milliseconds()})
		phase = next
		phaseAt = now
	}
	serviceInitialized := false
	err := func() error {
		sdkDir := filepath.Join(job.WorkDir, "sdk")
		if err := os.MkdirAll(sdkDir, 0700); err != nil {
			return err
		}
		cfg := rtc.NewAgoraServiceConfig()
		cfg.AppId = p.AppID
		cfg.EnableAudioDevice = false
		cfg.EnableAudioProcessor = true
		cfg.EnableVideo = true
		cfg.UseStringUid = false
		cfg.AudioScenario = rtc.AudioScenario(p.AudioScenario)
		cfg.ChannelProfile = rtc.ChannelProfile(p.ChannelProfile)
		cfg.AreaCode = rtc.AreaCode(p.AreaCode)
		cfg.DomainLimit = p.DomainLimit
		cfg.IdleMode = false
		cfg.APMModel = 0
		cfg.LogPath = filepath.Join(sdkDir, "agorasdk.log")
		cfg.ConfigDir = sdkDir
		cfg.DataDir = sdkDir
		cfg.LogSize = p.LogSizeKB
		cfg.LogLevel = p.LogLevel
		if err := r.callArgs("sender", phase, "Initialize", map[string]any{"config": snapshotObject(cfg), "log_size_native_unit": "KB"}, false, func() int { return rtc.Initialize(cfg) }); err != nil {
			return err
		}
		serviceInitialized = true
		if job.EdgeEndpoint != "" {
			advance("CONFIGURE_MEDIA_EDGE")
			parameters, err := protocol.VOSParameters(job.EdgeEndpoint)
			if err != nil {
				return err
			}
			span := r.trace.Begin("sender", phase, "GetAgoraParameter", map[string]any{"scope": "service"}, false)
			parameter := rtc.GetAgoraParameter()
			r.trace.End(span, "object", nil, map[string]any{"present": parameter != nil, "parameter_id": r.worker + "/service/agora_parameter"})
			if parameter == nil {
				return errors.New("AGORA_PARAMETER_UNAVAILABLE")
			}
			if err := r.callArgs("sender", phase, "SetParameters", map[string]any{"scope": "service", "parameters": parameters, "rtc.vos_list": []string{job.EdgeEndpoint}, "applies_to": []string{"sender", "receiver"}}, false, func() int { return parameter.SetParameters(parameters) }); err != nil {
				return err
			}
		}
		for _, role := range []string{"sender", "receiver"} {
			advance("CREATE_CONNECTION")
			pub := rtc.NewRtcConPublishConfig()
			pub.AudioScenario = rtc.AudioScenario(p.AudioScenario)
			pub.AudioProfile = rtc.AudioProfile(p.AudioProfile)
			pub.IsPublishAudio = role == "sender"
			pub.IsPublishVideo = role == "sender"
			pub.AudioPublishType = rtc.AudioPublishTypePcm
			pub.VideoPublishType = rtc.VideoPublishTypeYuv
			cc := &rtc.RtcConnectionConfig{AutoSubscribeAudio: false, AutoSubscribeVideo: false, EnableAudioRecordingOrPlayout: false, ClientRole: rtc.ClientRoleBroadcaster, ChannelProfile: rtc.ChannelProfile(p.ChannelProfile), ChannelType: rtc.ChannelType(p.ChannelType)}
			span := r.trace.Begin(role, phase, "NewRtcConnection", map[string]any{"config": snapshotObject(cc), "publish_config": snapshotObject(pub)}, false)
			con := rtc.NewRtcConnection(cc, pub)
			r.trace.End(span, "object", nil, map[string]any{"created": con != nil, "connection_id": r.worker + "/" + role})
			if con == nil {
				return errors.New("CREATE_CONNECTION_FAILED")
			}
			connections[role] = con
		}
		advance("CONFIGURE_ACCESS")
		if p.Private {
			config := &rtc.LocalAccessPointConfiguration{IPList: p.AccessIPs, DomainList: p.AccessDomains, VerifyDomainName: p.VerifyDomain, Mode: rtc.LocalProxyMode(p.ProxyMode), DisableAut: p.DisableAut}
			if err := r.call("sender", phase, "SetLocalAccessPoint", func() int { return rtc.SetLocalAccessPoint(config) }); err != nil {
				return err
			}
		}
		for _, endpointRole := range []string{"sender", "receiver"} {
			role := endpointRole
			con := connections[role]
			uid := strconv.FormatUint(uint64(p.SenderUID), 10)
			if role == "receiver" {
				uid = strconv.FormatUint(uint64(p.ReceiverUID), 10)
			}
			cb := func(name string, fields map[string]any) {
				if fields == nil {
					fields = map[string]any{}
				}
				fields["local_user_id"] = r.worker + "/" + role + "/local_user"
				r.callback(role, callbackStage(name), name, relatedAPI(name), fields, false)
			}
			observer := &rtc.RtcConnectionObserver{
				OnConnecting: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, reason int) {
					cb("OnConnecting", connectionParameters(info, &reason))
				},
				OnConnected: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, reason int) {
					cb("OnConnected", connectionParameters(info, &reason))
					if info == nil || info.ChannelId != job.Channel || info.LocalUserId != uid {
						return
					}
					live[role].Store(true)
					values[role].update(func(e *protocol.Evidence) { e.Connected = true })
					select {
					case joined[role] <- struct{}{}:
					default:
					}
				},
				OnDisconnected: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, reason int) {
					parameters := connectionParameters(info, &reason)
					parameters["expected"] = cleaning.Load()
					cb("OnDisconnected", parameters)
					if info == nil || info.ChannelId != job.Channel || info.LocalUserId != uid {
						return
					}
					live[role].Store(false)
					if cleaning.Load() {
						select {
						case disconnected[role] <- struct{}{}:
						default:
						}
					} else {
						select {
						case failures <- fmt.Sprintf("DISCONNECTED:%s:%d", role, reason):
						default:
						}
					}
				},
				OnConnectionFailure: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, code int) {
					parameters := connectionParameters(info, nil)
					parameters["err_code"] = code
					parameters["reason"] = code
					parameters["reason_type"] = "CONNECTION_CHANGED_REASON_TYPE"
					cb("OnConnectionFailure", parameters)
					select {
					case failures <- fmt.Sprintf("JOIN_FAILED:%s:%d", role, code):
					default:
					}
				},
				OnConnectionLost: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo) {
					cb("OnConnectionLost", connectionParameters(info, nil))
				},
				OnReconnecting: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, reason int) {
					cb("OnReconnecting", connectionParameters(info, &reason))
				},
				OnReconnected: func(_ *rtc.RtcConnection, info *rtc.RtcConnectionInfo, reason int) {
					cb("OnReconnected", connectionParameters(info, &reason))
					live[role].Store(true)
				},
				OnUserJoined: func(_ *rtc.RtcConnection, user string) { cb("OnUserJoined", map[string]any{"uid": user}) },
				OnUserLeft: func(_ *rtc.RtcConnection, user string, reason int) {
					cb("OnUserLeft", map[string]any{"uid": user, "reason": reason})
				},
				OnError: func(_ *rtc.RtcConnection, code int, message string) {
					cb("OnError", map[string]any{"code": code, "message": sanitize(message, job.Credentials)})
				},
				OnEncryptionError: func(_ *rtc.RtcConnection, code int) { cb("OnEncryptionError", map[string]any{"code": code}) },
				OnTokenPrivilegeWillExpire: func(_ *rtc.RtcConnection, token string) {
					cb("OnTokenPrivilegeWillExpire", map[string]any{"token": token, "token_present": token != ""})
				},
				OnTokenPrivilegeDidExpire: func(_ *rtc.RtcConnection) {
					cb("OnTokenPrivilegeDidExpire", nil)
					select {
					case failures <- "TOKEN_EXPIRED":
					default:
					}
				},
			}
			advance("REGISTER")
			if err := r.call(role, phase, "RegisterObserver", func() int { return con.RegisterObserver(observer) }); err != nil {
				return err
			}
			local := &rtc.LocalUserObserver{
				OnAudioTrackPublishSuccess: func(_ *rtc.LocalUser, _ *rtc.LocalAudioTrack) {
					cb("OnAudioTrackPublishSuccess", map[string]any{"audio_track_id": r.worker + "/" + role + "/audio_track"})
				},
				OnVideoTrackPublishSuccess: func(_ *rtc.LocalUser, _ *rtc.LocalVideoTrack) {
					cb("OnVideoTrackPublishSuccess", map[string]any{"video_track_id": r.worker + "/" + role + "/video_track"})
				},
				OnAudioTrackUnpublished: func(_ *rtc.LocalUser, _ *rtc.LocalAudioTrack) {
					cb("OnAudioTrackUnpublished", map[string]any{"audio_track_id": r.worker + "/" + role + "/audio_track"})
				},
				OnVideoTrackUnpublished: func(_ *rtc.LocalUser, _ *rtc.LocalVideoTrack) {
					cb("OnVideoTrackUnpublished", map[string]any{"video_track_id": r.worker + "/" + role + "/video_track"})
				},
				OnAudioPublishStateChanged: func(_ *rtc.LocalUser, channel string, oldState, newState, elapsed int) {
					cb("OnAudioPublishStateChanged", map[string]any{"channel_id": channel, "old_state": oldState, "new_state": newState, "elapsed": elapsed})
				},
				OnUserAudioTrackSubscribed: func(_ *rtc.LocalUser, user string, _ *rtc.RemoteAudioTrack) {
					cb("OnUserAudioTrackSubscribed", map[string]any{"uid": user, "remote_audio_track_id": r.worker + "/" + role + "/remote_audio/" + user})
				},
				OnUserVideoTrackSubscribed: func(_ *rtc.LocalUser, user string, info *rtc.VideoTrackInfo, _ *rtc.RemoteVideoTrack) {
					cb("OnUserVideoTrackSubscribed", map[string]any{"uid": user, "track_info": videoTrackParameters(info), "remote_video_track_id": r.worker + "/" + role + "/remote_video/" + user})
				},
				OnUserAudioTrackStateChanged: func(_ *rtc.LocalUser, user string, _ *rtc.RemoteAudioTrack, state, reason, elapsed int) {
					cb("OnUserAudioTrackStateChanged", map[string]any{"uid": user, "remote_audio_track_id": r.worker + "/" + role + "/remote_audio/" + user, "state": state, "reason": reason, "elapsed": elapsed})
				},
				OnUserVideoTrackStateChanged: func(_ *rtc.LocalUser, user string, _ *rtc.RemoteVideoTrack, state, reason, elapsed int) {
					cb("OnUserVideoTrackStateChanged", map[string]any{"uid": user, "remote_video_track_id": r.worker + "/" + role + "/remote_video/" + user, "state": state, "reason": reason, "elapsed": elapsed})
				},
				OnUserInfoUpdated: func(_ *rtc.LocalUser, user string, mediaInfo, val int) {
					cb("OnUserInfoUpdated", map[string]any{"uid": user, "user_media_info": mediaInfo, "value": val})
				},
			}
			if err := r.call(role, phase, "RegisterLocalUserObserver", func() int { return con.RegisterLocalUserObserver(local) }); err != nil {
				return err
			}
		}
		b := connections["receiver"]
		if err := r.call("receiver", "REGISTER", "SetPlaybackAudioFrameBeforeMixingParameters", func() int { return r.user("receiver", b).SetPlaybackAudioFrameBeforeMixingParameters(1, 16000) }); err != nil {
			return err
		}
		audio := &rtc.AudioFrameObserver{OnGetAudioFramePosition: func(*rtc.LocalUser) int {
			r.callback("receiver", "REGISTER", "OnGetAudioFramePosition", "RegisterAudioFrameObserver", map[string]any{"return_value": 0x0008}, true)
			return 0x0008
		}, OnPlaybackAudioFrameBeforeMixing: func(_ *rtc.LocalUser, channel, user string, f *rtc.AudioFrame, vad rtc.VadState, vadFrame *rtc.AudioFrame) bool {
			parameters := audioParameters(channel, user, f, vad, vadFrame)
			// Native 4.6.31 may omit channelId here. This observer belongs to
			// B's connection, whose channel and UID were checked in OnConnected.
			parameters["connection_channel_id"] = job.Channel
			parameters["channel_from_connection"] = channel == ""
			accepted := false
			defer func() {
				parameters["accepted"] = accepted
				r.callback("receiver", "RECEIVE_AUDIO", "OnPlaybackAudioFrameBeforeMixing", "SubscribeAudio", parameters, true)
			}()
			if cleaning.Load() || ctx.Err() != nil || !streaming.Load() || !sameAudioSource(job.Channel, job.PublisherUID, channel, user, values["receiver"].snapshot().Connected) || f == nil || f.Type != rtc.AudioFrameTypePCM16 || f.BytesPerSample != 2 || f.Channels != 1 || f.SamplesPerSec != 16000 || f.SamplesPerChannel <= 0 || len(f.Buffer) != f.SamplesPerChannel*2 {
				return true
			}
			var energy int64
			for i := 0; i < len(f.Buffer); i += 2 {
				v := int64(int16(binary.LittleEndian.Uint16(f.Buffer[i:])))
				energy += v * v
			}
			if energy/int64(f.SamplesPerChannel) < 900 {
				return true
			}
			accepted = true
			first := false
			values["receiver"].update(func(e *protocol.Evidence) {
				first = e.ReceivedFrames == 0
				e.ReceivedFrames++
				e.ValidSamples += f.SamplesPerChannel
				e.MediaDurationMS = e.ValidSamples / 16
				if first {
					e.FirstFrameMS = time.Since(r.at).Milliseconds()
				}
			})
			if first {
				parameters["first_valid_frame"] = true
			}
			return true
		}}
		if err := r.call("receiver", "REGISTER", "RegisterAudioFrameObserver", func() int { return b.RegisterAudioFrameObserver(audio, 0, nil) }); err != nil {
			return err
		}
		video := &rtc.VideoFrameObserver{OnRenderVideoFrame: func(channel string, user uint32, f *rtc.VideoFrame) bool {
			parameters := videoParameters(channel, user, f)
			accepted := false
			defer func() {
				parameters["accepted"] = accepted
				r.callback("receiver", "RECEIVE_VIDEO", "OnRenderVideoFrame", "SubscribeVideo", parameters, true)
			}()
			if cleaning.Load() || ctx.Err() != nil || !streaming.Load() || channel != job.Channel || user != p.SenderUID || f == nil || f.Type != rtc.VideoPixelI420 || f.Width <= 0 || f.Height <= 0 || f.YStride < f.Width || f.UStride < (f.Width+1)/2 || f.VStride < (f.Width+1)/2 || len(f.YBuffer) < f.YStride*f.Height || len(f.UBuffer) < f.UStride*((f.Height+1)/2) || len(f.VBuffer) < f.VStride*((f.Height+1)/2) {
				return true
			}
			accepted = true
			first := false
			values["receiver"].update(func(e *protocol.Evidence) {
				first = e.ReceivedVideoFrames == 0
				e.ReceivedVideoFrames++
				e.VideoWidth = f.Width
				e.VideoHeight = f.Height
				if first {
					e.FirstVideoMS = time.Since(r.at).Milliseconds()
				}
			})
			if first {
				parameters["first_valid_frame"] = true
			}
			return true
		}}
		if err := r.call("receiver", "REGISTER", "RegisterVideoFrameObserver", func() int { return b.RegisterVideoFrameObserver(video) }); err != nil {
			return err
		}
		advance("CONFIGURE_VIDEO")
		encoder := rtc.NewVideoEncoderConfiguration()
		encoder.Width = 352
		encoder.Height = 288
		encoder.Framerate = 15
		encoder.Bitrate = 400
		encoder.CodecType = rtc.VideoCodecTypeH264
		if err := r.callArgs("sender", "CONFIGURE_VIDEO", "SetVideoEncoderConfiguration", map[string]any{"config": snapshotObject(encoder)}, false, func() int { return connections["sender"].SetVideoEncoderConfiguration(encoder) }); err != nil {
			return err
		}
		advance("JOIN")
		for _, role := range []string{"sender", "receiver"} {
			token := ""
			if job.Credentials != nil {
				token = job.Credentials.SenderToken
				if role == "receiver" {
					token = job.Credentials.ReceiverToken
				}
			}
			uid := p.SenderUID
			if role == "receiver" {
				uid = p.ReceiverUID
			}
			if err := r.call(role, phase, "Connect", func() int {
				return connections[role].Connect(token, job.Channel, strconv.FormatUint(uint64(uid), 10), "")
			}); err != nil {
				return err
			}
		}
		for _, role := range []string{"sender", "receiver"} {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case failure := <-failures:
				return errors.New(failure)
			case <-joined[role]:
			}
		}
		advance("SUBSCRIBE")
		if err := r.call("receiver", phase, "SubscribeAudio", func() int { return r.user("receiver", b).SubscribeAudio(job.PublisherUID) }); err != nil {
			return err
		}
		values["receiver"].update(func(e *protocol.Evidence) { e.Subscribed = true })
		if err := r.call("receiver", phase, "SubscribeVideo", func() int {
			return r.user("receiver", b).SubscribeVideo(job.PublisherUID, &rtc.VideoSubscriptionOptions{StreamType: rtc.VideoStreamHigh, EncodedFrameOnly: false})
		}); err != nil {
			return err
		}
		values["receiver"].update(func(e *protocol.Evidence) { e.VideoSubscribed = true })
		for _, role := range []string{"sender", "receiver"} {
			r.out <- r.message("run_ready", role)
		}
		advance("WAIT_START")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-start:
		}
		advance("PUBLISH")
		streaming.Store(true)
		if err := r.call("sender", phase, "PublishAudio", func() int { return connections["sender"].PublishAudio() }); err != nil {
			return err
		}
		values["sender"].update(func(e *protocol.Evidence) { e.Published = true })
		if err := r.call("sender", phase, "PublishVideo", func() int { return connections["sender"].PublishVideo() }); err != nil {
			return err
		}
		values["sender"].update(func(e *protocol.Evidence) { e.VideoPublished = true })
		advance("MEDIA")
		return sendFiles(ctx, r, connections["sender"], values, failures, policy)
	}()
	outcome, reason := "SUCCESS", "MEDIA_VERIFIED"
	if err != nil {
		outcome = "FAILED"
		reason = "SDK_" + phase + "_FAILED"
		if phase == "INITIALIZE" || phase == "CREATE_CONNECTION" || phase == "CONFIGURE_ACCESS" || phase == "REGISTER" || phase == "CONFIGURE_VIDEO" || phase == "CONFIGURE_MEDIA_EDGE" {
			outcome = "ERROR"
		}
		if errors.Is(err, context.Canceled) {
			outcome = "CANCELLED"
			reason = "CANCELLED"
		}
		if errors.Is(err, errLocalMedia) {
			outcome = "ERROR"
			reason = "LOCAL_MEDIA_UNAVAILABLE"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "MEDIA_RECEIVE_TIMEOUT"
			if phase == "WAIT_START" {
				outcome = "ERROR"
				reason = "CONTROL_START_TIMEOUT"
			}
			if phase == "JOIN" {
				reason = "JOIN_TIMEOUT"
			}
		}
		r.event("receiver", "stage_event", phase, "stage_failed", "", nil, map[string]any{"reason": reason, "detail": sanitize(err.Error(), job.Credentials)}, 0)
	}
	snapshots := map[string]protocol.Evidence{}
	for role, s := range values {
		snapshots[role] = s.snapshot()
	}
	businessPhase := phase
	advance("CLEANUP")
	cleaning.Store(true)
	cleanup := "SUCCESS"
	waiting := map[string]bool{}
	for _, role := range []string{"sender", "receiver"} {
		if con := connections[role]; con != nil {
			waiting[role] = live[role].Load()
			if err := r.call(role, "CLEANUP", "Disconnect", con.Disconnect); err != nil {
				cleanup = "FAILED"
				waiting[role] = false
			}
		}
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), protocol.CleanupGrace-2*time.Second)
	for _, role := range []string{"sender", "receiver"} {
		if waiting[role] {
			select {
			case <-disconnected[role]:
			case <-cleanupCtx.Done():
				cleanup = "FAILED"
				r.event(role, "stage_event", "CLEANUP", "cleanup_timeout", "", nil, map[string]any{"waiting_for": "OnDisconnected"}, 0)
			}
		}
	}
	cleanupCancel()
	for _, role := range []string{"sender", "receiver"} {
		if con := connections[role]; con != nil {
			span := r.trace.Begin(role, "CLEANUP", "Connection.Release", r.arguments(role, "Connection.Release"), false)
			con.Release()
			r.trace.End(span, "void", nil, nil)
		}
	}

	if serviceInitialized {
		if e := r.call("sender", "CLEANUP", "Service.Release", rtc.Release); e != nil {
			cleanup = "FAILED"
		}
	}
	advance("DONE")
	traceErr = r.trace.Close()
	if traceErr != nil {
		outcome = "ERROR"
		reason = "API_TRACE_INCOMPLETE"
	}
	if r.dropped.Load() > 0 {
		outcome = "ERROR"
		reason = "LOCAL_TELEMETRY_OVERFLOW"
	}
	for _, role := range []string{"sender", "receiver"} {
		m := r.message("run_result", role)
		roleOutcome, roleReason := outcome, reason
		if businessPhase == "MEDIA" && errors.Is(err, context.DeadlineExceeded) {
			a, b := snapshots["sender"], snapshots["receiver"]
			if role == "sender" && a.SentFrames > 0 && a.SentVideoFrames > 0 {
				roleOutcome = "SUCCESS"
				roleReason = "MEDIA_SUBMITTED"
			}
			if role == "receiver" {
				if b.MediaDurationMS >= 1000 && b.ReceivedVideoFrames < 5 {
					roleReason = "VIDEO_RECEIVE_TIMEOUT"
				} else if b.MediaDurationMS < 1000 && b.ReceivedVideoFrames >= 5 {
					roleReason = "AUDIO_RECEIVE_TIMEOUT"
				}
			}
		}
		m.Result = &protocol.Result{Stages: append([]protocol.StageTiming(nil), stages...), ID: protocol.ID(), WorkerID: r.worker, Driver: "agora", Outcome: roleOutcome, Reason: roleReason, Cleanup: cleanup, Evidence: snapshots[role], FinishedAt: time.Now().UTC(), TraceComplete: traceErr == nil, TraceDropped: r.trace.Dropped()}
		r.out <- m
	}
	r.mu.Lock()
	r.closed = true
	close(r.out)
	r.mu.Unlock()
	return <-written
}

func sendFiles(ctx context.Context, r *recorder, a *rtc.RtcConnection, values map[string]*stats, failures <-chan string, policy mediaPolicy) error {
	pcm, err := os.Open(filepath.Join(DemoDirectory, "send_audio_16k_1ch.pcm"))
	if err != nil {
		return fmt.Errorf("%w:demo_audio", errLocalMedia)
	}
	defer pcm.Close()
	yuv, err := os.Open(filepath.Join(DemoDirectory, "send_video_cif_352x288.yuv"))
	if err != nil {
		return fmt.Errorf("%w:demo_video", errLocalMedia)
	}
	defer yuv.Close()
	readLoop := func(f *os.File, b []byte) error {
		_, err := io.ReadFull(f, b)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if _, e := f.Seek(0, 0); e != nil {
				return e
			}
			_, err = io.ReadFull(f, b)
		}
		return err
	}
	audioTicker := time.NewTicker(10 * time.Millisecond)
	defer audioTicker.Stop()
	videoTicker := time.NewTicker(time.Second / 15)
	defer videoTicker.Stop()
	summary := time.NewTicker(time.Second)
	defer summary.Stop()
	end := time.NewTimer(time.Duration(r.job.MediaDurationMS) * time.Millisecond)
	defer end.Stop()
	audioClock, videoClock := audioTicker.C, videoTicker.C
	if policy.omitAudio {
		audioClock = nil
	}
	if policy.omitVideo {
		videoClock = nil
	}
	mediaEnded := false
	audio := make([]byte, 320)
	video := make([]byte, 352*288*3/2)
	started := time.Now()
	baseTimestamp := time.Now().UnixMilli()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case failure := <-failures:
			return errors.New(failure)
		case <-end.C:
			mediaEnded = true
			audioClock = nil
			videoClock = nil
		case <-summary.C:
			r.trace.Flush()
			for _, role := range []string{"sender", "receiver"} {
				v := values[role].snapshot()
				r.event(role, "media_summary", "MEDIA", "media_window", "", nil, map[string]any{"sent_frames": v.SentFrames, "received_frames": v.ReceivedFrames, "valid_samples": v.ValidSamples, "media_duration_ms": v.MediaDurationMS, "sent_video_frames": v.SentVideoFrames, "received_video_frames": v.ReceivedVideoFrames, "video_width": v.VideoWidth, "video_height": v.VideoHeight}, 0)
			}
		case <-audioClock:
			if err := readLoop(pcm, audio); err != nil {
				return fmt.Errorf("%w: %v", errLocalMedia, err)
			}
			pts := baseTimestamp + time.Since(started).Milliseconds()
			send := func() int {
				return a.PushAudioPcmData(audio, 16000, 1, pts)
			}
			args := map[string]any{"data": map[string]any{"buffer_bytes": len(audio), "payload_omitted": true}, "sample_rate": 16000, "channels": 1, "start_pts_in_ms": pts}
			if err := r.callArgs("sender", "MEDIA", "PushAudioPcmData", args, true, send); err != nil {
				return err
			}
			values["sender"].update(func(v *protocol.Evidence) { v.SentFrames++; v.SentBytes += len(audio) })
		case <-videoClock:
			if err := readLoop(yuv, video); err != nil {
				return fmt.Errorf("%w: %v", errLocalMedia, err)
			}
			pts := baseTimestamp + time.Since(started).Milliseconds()
			send := func() int {
				return a.PushVideoFrame(&rtc.ExternalVideoFrame{Type: rtc.VideoBufferRawData, Format: rtc.VideoPixelI420, Buffer: video, Stride: 352, Height: 288, Timestamp: pts})
			}
			args := map[string]any{"frame": map[string]any{"type": int(rtc.VideoBufferRawData), "format": int(rtc.VideoPixelI420), "buffer_bytes": len(video), "stride": 352, "height": 288, "timestamp": pts, "payload_omitted": true}}
			if err := r.callArgs("sender", "MEDIA", "PushVideoFrame", args, true, send); err != nil {
				return err
			}
			values["sender"].update(func(v *protocol.Evidence) { v.SentVideoFrames++ })
		}
		v := values["receiver"].snapshot()
		if mediaEnded && v.MediaDurationMS >= 1000 && v.ReceivedVideoFrames >= 5 {
			return nil
		}
	}
}

func (r *recorder) user(role string, connection *rtc.RtcConnection) *rtc.LocalUser {
	span := r.trace.Begin(role, "ACCESS", "GetLocalUser", map[string]any{"connection_id": r.worker + "/" + role}, false)
	value := connection.GetLocalUser()
	r.trace.End(span, "object", nil, map[string]any{"present": value != nil, "local_user_id": r.worker + "/" + role + "/local_user"})
	return value
}
