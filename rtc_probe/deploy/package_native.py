#!/usr/bin/env python3
"""Package the compiled SDK probe and its actual Linux runtime dependencies.

Run on the designated Linux build/probe host after compiling with -tags agora.
"""
import argparse
import glob
import os
from pathlib import Path
import re
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--sdk", type=Path, required=True)
    parser.add_argument("--media", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root = args.output.resolve()
    root.mkdir(parents=True, exist_ok=True)
    sdk = args.sdk.resolve()
    environment = dict(os.environ, LD_LIBRARY_PATH=str(sdk) + ":" + os.environ.get("LD_LIBRARY_PATH", ""))
    linked = subprocess.check_output(["ldd", str(args.binary.resolve())], env=environment, text=True)
    if "not found" in linked:
        raise RuntimeError("native runtime dependency missing")
    paths = set(re.findall(r"(?:=>\s*)?(/[^\s]+)", linked))
    paths.update(glob.glob("/usr/lib64/libnss_*.so.2"))
    paths.update(glob.glob("/lib64/libnss_*.so.2"))
    for name in ["/etc/nsswitch.conf", "/etc/pki/tls/certs/ca-bundle.crt"]:
        if Path(name).exists():
            paths.add(name)
    for value in paths:
        source = Path(value)
        if not source.is_file() or os.path.commonpath([str(source.resolve()),str(sdk)])==str(sdk):
            continue
        target = root / "system" / source.relative_to("/")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
        target.chmod(source.stat().st_mode & 0o777)
        if ".so" in source.name:
            flat = root / "system/opt/rtc-probe/runtime-libs" / source.name
            flat.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, flat)
            flat.chmod(source.stat().st_mode & 0o777)
    ca = root / "system/etc/ssl/certs/ca-certificates.crt"
    ca.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile("/etc/pki/tls/certs/ca-bundle.crt", ca)
    capture = root / "system/opt/rtc-probe/libroute_capture.so"
    capture.parent.mkdir(parents=True,exist_ok=True)
    subprocess.run(["gcc","-shared","-fPIC","-O2","-o",str(capture),str(Path(__file__).resolve().parent.parent/"native/route_capture.c"),"-ldl","-pthread"],check=True)
    capture.chmod(0o755)
    shutil.copyfile(args.binary, root / "rtc-probe")
    (root / "rtc-probe").chmod(0o755)
    (root/"sdk").mkdir(exist_ok=True)
    subprocess.run(["cp","-a",str(sdk)+"/.",str(root/"sdk")],check=True)
    (root/"test_data").mkdir(exist_ok=True)
    subprocess.run(["cp","-a",str(args.media)+"/.",str(root/"test_data")],check=True)
    (root / "Dockerfile").write_text('''FROM scratch
COPY system/ /
COPY sdk/ /opt/agora_sdk/
COPY test_data/ /opt/rtc-probe/test_data/
COPY rtc-probe /rtc-probe
ENV LD_LIBRARY_PATH=/opt/agora_sdk:/opt/rtc-probe/runtime-libs:/usr/lib64:/lib64
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV RTC_PROBE_DATA_DIR=/data
USER 65532:65532
ENTRYPOINT ["/rtc-probe"]
''')
    (root / ".dockerignore").write_text("*\n!Dockerfile\n!system/**\n!sdk/**\n!test_data/**\n!rtc-probe\n")
    print("Native image context prepared:", root)


if __name__ == "__main__":
    main()
