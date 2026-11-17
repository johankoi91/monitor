#!/usr/bin/env python3
"""Upgrade the existing center/probe without changing task or SDK settings."""
import argparse
import json
import os
import re
from pathlib import Path
import subprocess
import time


def docker(*args, capture=False):
    if capture:
        return subprocess.check_output(["docker", *args], text=True).strip()
    subprocess.run(["docker", *args], check=True)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("component", choices=["center", "probe"])
    p.add_argument("stage", type=Path)
    p.add_argument("--release", required=True)
    p.add_argument("--prebuilt", action="store_true", help="use an image loaded from the offline bundle")
    args = p.parse_args()
    if os.geteuid() != 0:
        raise RuntimeError("root required")
    stage = args.stage.resolve()
    stage.mkdir(mode=0o700, parents=True, exist_ok=True)
    name = "avops-center" if args.component == "center" else "rtc-probe"
    if not re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,63}", args.release):
        raise RuntimeError("invalid release tag")
    image = ("avops-center:" if args.component == "center" else "rtc-probe:sdk-") + args.release
    old = json.loads(docker("inspect", name, capture=True))[0]
    if old["Config"]["Image"] == image:
        raise RuntimeError("requested image already installed")
    previous = name + "-before-" + args.release
    if subprocess.run(["docker", "inspect", previous], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise RuntimeError("backup container already exists")
    (stage / "previous-container.json").write_text(json.dumps(old))
    (stage / "previous-container.json").chmod(0o600)
    env = stage / "runtime.env"
    env.write_text("\n".join(old["Config"]["Env"]) + "\n")
    env.chmod(0o600)
    if args.component == "center" and not args.prebuilt:
        (stage / "Dockerfile").write_text("FROM " + old["Config"]["Image"] + "\nCOPY center /center\n")
        (stage / ".dockerignore").write_text("*\n!Dockerfile\n!center\n")
        docker("build", "-q", "-t", image, str(stage))
    else:
        docker("image", "inspect", image, capture=True)
    host = old["HostConfig"]
    command = ["run", "-d", "--name", name, "--restart", "unless-stopped", "--network", host["NetworkMode"], "--env-file", str(env)]
    for bind in host.get("Binds") or []:
        command += ["-v", bind]
    for alias in host.get("ExtraHosts") or []:
        command += ["--add-host", alias]
    for option in host.get("SecurityOpt") or []:
        command += ["--security-opt", option]
    for capability in host.get("CapDrop") or []:
        command += ["--cap-drop", capability]
    for mount, options in (host.get("Tmpfs") or {}).items():
        command += ["--tmpfs", mount + ":" + options]
    if host.get("ReadonlyRootfs"):
        command += ["--read-only"]
    if host.get("Memory"):
        command += ["--memory", str(host["Memory"])]
    if host.get("NanoCpus"):
        command += ["--cpus", str(host["NanoCpus"] / 1e9)]
    if host.get("PidsLimit"):
        command += ["--pids-limit", str(host["PidsLimit"])]
    command.append(image)
    docker("stop", "--time", "15", name)
    docker("rename", name, previous)
    try:
        docker(*command)
        time.sleep(2)
        state = json.loads(docker("inspect", name, capture=True))[0]["State"]
        if not state["Running"]:
            raise RuntimeError("new container exited")
        if args.component == "center":
            for _ in range(20):
                result = subprocess.run(["curl", "--fail", "--silent", "--config", "/etc/avops-monitor/admin-curl.conf", "http://127.0.0.1:18084/api/v1/rtc/status"], stdout=subprocess.DEVNULL)
                if result.returncode == 0:
                    break
                time.sleep(1)
            else:
                raise RuntimeError("center API readiness failed")
    except BaseException:
        log = subprocess.run(["docker", "logs", "--tail", "150", name], capture_output=True, text=True)
        (stage / "failed-startup.log").write_text(log.stdout + log.stderr)
        (stage / "failed-startup.log").chmod(0o600)
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        docker("rename", previous, name)
        docker("start", name)
        raise
    print(json.dumps({"component": args.component, "image": image, "container": name, "backup_container": previous}))


if __name__ == "__main__":
    main()
