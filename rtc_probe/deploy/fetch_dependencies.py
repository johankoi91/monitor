#!/usr/bin/env python3
"""Fetch the pinned vendor Linux SDK and only the fixed demo media files."""
import hashlib
from pathlib import Path
import subprocess
import zipfile

SDK_URL = "https://download.agora.io/sdk/release/shengwang_rtc_sdk.v4.6.31.135_28737_SERVER_20260930_1604_1321736_external-20260824_1848-3a.zip"
SDK_SHA = "902126d5518add1594ef480700c0503a0d11b3964dd22f0a5ee3ddc2b5204259"
DEMO_URL = "https://download.agora.io/demo/test/server_sdk_test_data_202410252135.zip"
MEDIA = {
    "send_audio_16k_1ch.pcm": "b323ff5e13a7a460bad0cc3b4293720c37e4df1f14a726b5a8658fb8c885a7b7",
    "send_video_cif_352x288.yuv": "49d59cbeb74c061539d4b27310e097abd9e093523f01d41e0e26c2bbae77b408",
}


def download(url, target):
    if not target.exists():
        subprocess.run(["curl", "--fail", "--location", "--silent", "--show-error", "--connect-timeout", "10", "--max-time", "180", url, "-o", str(target)], check=True)


def main():
    repo = Path(__file__).resolve().parents[2]
    cache = repo / "rtc_probe/build/native-sdk"
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / "native-sdk.zip"
    download(SDK_URL, archive)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != SDK_SHA:
        raise RuntimeError("vendor SDK archive digest mismatch")
    sdk_root = repo / "Agora-Golang-Server-SDK-2.6.6"
    with zipfile.ZipFile(archive) as bundle:
        for item in bundle.infolist():
            name = Path(item.filename)
            if name.is_absolute() or ".." in name.parts or not name.parts or name.parts[0] != "agora_sdk":
                raise RuntimeError("unexpected native SDK entry")
        bundle.extractall(sdk_root)
    demo_archive = cache / "demo-test-data.zip"
    download(DEMO_URL, demo_archive)
    output = repo / "rtc_probe/build/demo-media"
    output.mkdir(exist_ok=True)
    with zipfile.ZipFile(demo_archive) as bundle:
        for name, expected in MEDIA.items():
            data = bundle.read("test_data/" + name)
            if hashlib.sha256(data).hexdigest() != expected:
                raise RuntimeError("demo media digest mismatch: " + name)
            (output / name).write_bytes(data)
    print("Pinned native SDK and fixed demo PCM/I420 files verified")


if __name__ == "__main__":
    main()
