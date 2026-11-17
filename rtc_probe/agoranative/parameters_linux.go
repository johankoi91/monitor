//go:build linux && agora

package agoranative

import (
	"encoding/json"
	rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"path/filepath"
)

func (r *recorder) arguments(role, name string) map[string]any {
	p := r.job.RTC
	local := r.worker + "/" + role + "/local_user"
	connection := r.worker + "/" + role
	switch name {
	case "Initialize":
		return map[string]any{"config": map[string]any{"AppId": p.AppID, "EnableAudioProcessor": true, "EnableAudioDevice": false, "EnableVideo": true, "UseStringUid": false, "AudioScenario": p.AudioScenario, "ChannelProfile": p.ChannelProfile, "AreaCode": p.AreaCode, "DomainLimit": p.DomainLimit, "IdleMode": false, "APMModel": 0, "LogPath": filepath.Join(r.job.WorkDir, "sdk", "agorasdk.log"), "ConfigDir": filepath.Join(r.job.WorkDir, "sdk"), "DataDir": filepath.Join(r.job.WorkDir, "sdk"), "LogSize": p.LogSizeKB, "LogSizeUnit": "KB", "LogLevel": p.LogLevel}}
	case "NewRtcConnection":
		return map[string]any{"config": map[string]any{"AutoSubscribeAudio": false, "AutoSubscribeVideo": false, "EnableAudioRecordingOrPlayout": false, "ClientRole": int(rtc.ClientRoleBroadcaster), "ChannelProfile": p.ChannelProfile, "ChannelType": p.ChannelType}, "publish_config": map[string]any{"AudioScenario": p.AudioScenario, "AudioProfile": p.AudioProfile, "IsPublishAudio": role == "sender", "IsPublishVideo": role == "sender", "AudioPublishType": int(rtc.AudioPublishTypePcm), "VideoPublishType": int(rtc.VideoPublishTypeYuv)}}
	case "SetLocalAccessPoint":
		return map[string]any{"config": map[string]any{"IPList": p.AccessIPs, "DomainList": p.AccessDomains, "VerifyDomainName": p.VerifyDomain, "Mode": p.ProxyMode, "DisableAut": p.DisableAut}}
	case "RegisterObserver":
		return map[string]any{"connection_id": connection, "callbacks": []string{"OnConnecting", "OnConnected", "OnDisconnected", "OnConnectionFailure", "OnConnectionLost", "OnReconnecting", "OnReconnected", "OnUserJoined", "OnUserLeft", "OnError", "OnEncryptionError", "OnTokenPrivilegeWillExpire", "OnTokenPrivilegeDidExpire"}, "native_return_exposed": false}
	case "RegisterLocalUserObserver":
		return map[string]any{"local_user_id": local, "callbacks": []string{"OnAudioTrackPublishSuccess", "OnVideoTrackPublishSuccess", "OnAudioTrackUnpublished", "OnVideoTrackUnpublished", "OnAudioPublishStateChanged", "OnUserAudioTrackSubscribed", "OnUserVideoTrackSubscribed", "OnUserAudioTrackStateChanged", "OnUserVideoTrackStateChanged", "OnUserInfoUpdated"}}
	case "RegisterAudioFrameObserver":
		return map[string]any{"local_user_id": local, "callbacks": []string{"OnGetAudioFramePosition", "OnPlaybackAudioFrameBeforeMixing"}, "enable_vad": 0, "vad_config": nil, "native_return_exposed": false}
	case "RegisterVideoFrameObserver":
		return map[string]any{"local_user_id": local, "callbacks": []string{"OnRenderVideoFrame"}, "format_preference": "I420", "process_mode": "read_only"}
	case "SetPlaybackAudioFrameBeforeMixingParameters":
		return map[string]any{"local_user_id": local, "channels": 1, "sample_rate": 16000}
	case "SetVideoEncoderConfiguration":
		return map[string]any{"config": map[string]any{"CodecType": int(rtc.VideoCodecTypeH264), "Width": 352, "Height": 288, "Framerate": 15, "Bitrate": 400, "MinBitrate": -1, "OrientationMode": 0, "DegradePreference": 2, "MirrorMode": 0, "EncodeAlpha": false}}
	case "Connect":
		uid := protocol.SenderUID
		token := ""
		if r.job.Credentials != nil {
			token = r.job.Credentials.SenderToken
		}
		if role == "receiver" {
			uid = protocol.ReceiverUID
			if r.job.Credentials != nil {
				token = r.job.Credentials.ReceiverToken
			}
		}
		return map[string]any{"connection_id": connection, "token": token, "token_present": token != "", "channel": r.job.Channel, "uid": uid, "info": ""}
	case "SubscribeAudio":
		return map[string]any{"local_user_id": local, "uid": r.job.PublisherUID}
	case "SubscribeVideo":
		return map[string]any{"local_user_id": local, "uid": r.job.PublisherUID, "options": map[string]any{"StreamType": int(rtc.VideoStreamHigh), "EncodedFrameOnly": false}}
	case "PublishAudio", "PublishVideo":
		return map[string]any{"connection_id": connection}
	case "Disconnect", "Connection.Release":
		return map[string]any{"connection_id": connection}
	case "Service.Release":
		return map[string]any{"service_id": r.worker + "/service"}
	default:
		return map[string]any{}
	}
}
func connectionParameters(info *rtc.RtcConnectionInfo, reason *int) map[string]any {
	out := map[string]any{}
	if info == nil {
		out["connection_info"] = nil
	} else {
		out["connection_info"] = map[string]any{"connection_id": info.ConnectionId, "channel_id": info.ChannelId, "state": info.State, "local_user_id": info.LocalUserId, "internal_uid": info.InternalUid}
	}
	if reason != nil {
		out["reason"] = *reason
	}
	return out
}
func videoTrackParameters(info *rtc.VideoTrackInfo) any {
	if info == nil {
		return nil
	}
	return map[string]any{"is_local": info.IsLocal, "owner_uid": info.OwnerUid, "track_id": info.TrackId, "channel_id": info.ChannelId, "stream_type": info.StreamType, "codec_type": info.CodecType, "encoded_frame_only": info.EncodedFrameOnly, "source_type": info.SourceType, "observation_position": info.ObservationPosition}
}
func audioParameters(channel, uid string, f *rtc.AudioFrame, vad rtc.VadState, vadFrame *rtc.AudioFrame) map[string]any {
	out := map[string]any{"channel_id": channel, "uid": uid, "vad_state": int(vad), "vad_frame_present": vadFrame != nil, "return_value": true}
	if f == nil {
		out["frame"] = nil
		return out
	}
	out["frame"] = map[string]any{"type": int(f.Type), "samples_per_channel": f.SamplesPerChannel, "samples_per_sec": f.SamplesPerSec, "bytes_per_sample": f.BytesPerSample, "channels": f.Channels, "buffer_bytes": len(f.Buffer), "render_time_ms": f.RenderTimeMs, "present_time_ms": f.PresentTimeMs, "avsync_type": f.AvsyncType, "rms": f.Rms, "voice_prob": f.VoiceProb, "music_prob": f.MusicProb, "pitch": f.Pitch, "payload_omitted": true}
	return out
}
func videoParameters(channel string, uid uint32, f *rtc.VideoFrame) map[string]any {
	out := map[string]any{"channel_id": channel, "uid": uid, "return_value": true}
	if f == nil {
		out["frame"] = nil
		return out
	}
	out["frame"] = map[string]any{"type": int(f.Type), "width": f.Width, "height": f.Height, "y_stride": f.YStride, "u_stride": f.UStride, "v_stride": f.VStride, "y_buffer_bytes": len(f.YBuffer), "u_buffer_bytes": len(f.UBuffer), "v_buffer_bytes": len(f.VBuffer), "rotation": int(f.Rotation), "render_time_ms": f.RenderTimeMs, "avsync_type": f.AVSyncType, "metadata_bytes": len(f.MetadataBuffer), "alpha_bytes": len(f.AlphaBuffer), "alpha_mode": f.AlphaMode, "texture_id": f.TextureID, "payload_omitted": true}
	return out
}
func relatedAPI(name string) string {
	switch name {
	case "OnAudioTrackPublishSuccess", "OnAudioPublishStateChanged":
		return "PublishAudio"
	case "OnVideoTrackPublishSuccess":
		return "PublishVideo"
	case "OnUserAudioTrackSubscribed", "OnUserAudioTrackStateChanged":
		return "SubscribeAudio"
	case "OnUserVideoTrackSubscribed", "OnUserVideoTrackStateChanged":
		return "SubscribeVideo"
	case "OnDisconnected", "OnAudioTrackUnpublished", "OnVideoTrackUnpublished":
		return "Disconnect"
	default:
		return "Connect"
	}
}

func snapshotObject(value any) map[string]any {
	b, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"snapshot_error": true}
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return map[string]any{"snapshot_error": true}
	}
	return out
}
func callbackStage(name string) string {
	switch relatedAPI(name) {
	case "PublishAudio", "PublishVideo":
		return "PUBLISH"
	case "SubscribeAudio", "SubscribeVideo":
		return "SUBSCRIBE"
	case "Disconnect":
		return "CLEANUP"
	default:
		return "CONNECTION"
	}
}
