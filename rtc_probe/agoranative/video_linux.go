//go:build linux && agora

package agoranative

import rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"

// Receive YUV data from the SDK's decoded-frame callback, scoped to this peer.
func yuvFrameRejection(expectedChannel, expectedUID, channel, uid string, frame *rtc.VideoFrame) string {
	if channel != expectedChannel {
		return "CHANNEL_MISMATCH"
	}
	if uid != expectedUID {
		return "PUBLISHER_UID_MISMATCH"
	}
	if frame == nil {
		return "EMPTY_VIDEO_FRAME"
	}
	if frame.Type != rtc.VideoPixelI420 {
		return "UNEXPECTED_PIXEL_FORMAT"
	}
	if frame.Width <= 0 || frame.Height <= 0 || frame.YStride < frame.Width || frame.UStride < (frame.Width+1)/2 || frame.VStride < (frame.Width+1)/2 {
		return "INVALID_YUV_DIMENSIONS"
	}
	if len(frame.YBuffer) < frame.YStride*frame.Height || len(frame.UBuffer) < frame.UStride*((frame.Height+1)/2) || len(frame.VBuffer) < frame.VStride*((frame.Height+1)/2) {
		return "INCOMPLETE_YUV_PLANES"
	}
	return ""
}
