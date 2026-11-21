//go:build linux && agora

package agoranative

import (
	"testing"

	rtc "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
)

func TestYUVCallbackEvidenceRejectsOtherPeersAndIncompleteFrames(t *testing.T) {
	valid := rtc.VideoFrame{Type: rtc.VideoPixelI420, Width: 4, Height: 4, YStride: 4, UStride: 2, VStride: 2, YBuffer: make([]byte, 16), UBuffer: make([]byte, 4), VBuffer: make([]byte, 4)}
	if reason := yuvFrameRejection("probe", "10001", "probe", "10001", &valid); reason != "" {
		t.Fatal(reason)
	}
	for _, test := range []struct {
		channel, uid string
		frame        *rtc.VideoFrame
		reason       string
	}{
		{"other", "10001", &valid, "CHANNEL_MISMATCH"},
		{"probe", "10002", &valid, "PUBLISHER_UID_MISMATCH"},
		{"probe", "10001", nil, "EMPTY_VIDEO_FRAME"},
	} {
		if got := yuvFrameRejection("probe", "10001", test.channel, test.uid, test.frame); got != test.reason {
			t.Fatalf("want %s, got %s", test.reason, got)
		}
	}
	broken := valid
	broken.UBuffer = broken.UBuffer[:3]
	if got := yuvFrameRejection("probe", "10001", "probe", "10001", &broken); got != "INCOMPLETE_YUV_PLANES" {
		t.Fatal(got)
	}
	broken = valid
	broken.YStride = 3
	if got := yuvFrameRejection("probe", "10001", "probe", "10001", &broken); got != "INVALID_YUV_DIMENSIONS" {
		t.Fatal(got)
	}
}
