package deploy

import (
	"archive/zip"
	"context"
	"errors"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"io"
	"os"
	"path/filepath"
)

const sdkURL = "https://download.agora.io/sdk/release/shengwang_rtc_sdk.v4.6.31.135_28737_SERVER_20260930_1604_1321736_external-20260824_1848-3a.zip"
const sdkSHA = "902126d5518add1594ef480700c0503a0d11b3964dd22f0a5ee3ddc2b5204259"

var mediaHashes = map[string]string{"send_audio_16k_1ch.pcm": "b323ff5e13a7a460bad0cc3b4293720c37e4df1f14a726b5a8658fb8c885a7b7", "send_video_cif_352x288.yuv": "49d59cbeb74c061539d4b27310e097abd9e093523f01d41e0e26c2bbae77b408"}
var webAssets = map[string][2]string{
	"node-linux.tar.xz":           {"https://nodejs.org/dist/v22.16.0/node-v22.16.0-linux-x64.tar.xz", "f4cb75bb036f0d0eddf6b79d9596df1aaab9ddccd6a20bf489be5abe9467e84e"},
	"chromium-headless-linux.zip": {"https://cdn.playwright.dev/dbazure/download/playwright/builds/chromium/1194/chromium-headless-shell-linux.zip", "d745c7210cc88b73c7991a22c9624819960d84bc3de77ac7336d2de841a8c91f"},
}

func Dependencies(ctx context.Context, cache string) error {
	if e := d.Fetch(ctx, sdkURL, filepath.Join(cache, "native-sdk.zip"), sdkSHA); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(cache, "sdk/agora_sdk/libagora_rtc_sdk.so")); os.IsNotExist(e) {
		if e = d.ExtractSDK(filepath.Join(cache, "native-sdk.zip"), filepath.Join(cache, "sdk")); e != nil {
			return e
		}
	}
	// The upstream demo ZIP has no published complete-archive digest. Verify each
	// selected member before creating the two fixed media files.
	archive := filepath.Join(cache, "demo-test-data.zip")
	if _, e := os.Stat(archive); os.IsNotExist(e) {
		if e = downloadDemo(ctx, archive); e != nil {
			return e
		}
	}
	if e := extractMedia(archive, filepath.Join(cache, "media")); e != nil {
		return e
	}
	for name, asset := range webAssets {
		if e := d.Fetch(ctx, asset[0], filepath.Join(cache, name), asset[1]); e != nil {
			return e
		}
	}
	return nil
}
func extractMedia(archive, output string) error {
	z, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer z.Close()
	found := 0
	for _, f := range z.File {
		for name, expected := range mediaHashes {
			if f.Name != "test_data/"+name {
				continue
			}
			in, e := f.Open()
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(in, 256<<20))
			in.Close()
			if e != nil {
				return e
			}
			sum, e := bytesDigest(b)
			if e != nil || sum != expected {
				return errors.New("fixed demo media SHA256 mismatch")
			}
			if e = os.MkdirAll(output, 0755); e != nil {
				return e
			}
			target := filepath.Join(output, name)
			if e = os.WriteFile(target, b, 0644); e != nil {
				return e
			}
			found++
		}
	}
	if found != 2 {
		return errors.New("fixed demo media missing")
	}
	return nil
}
