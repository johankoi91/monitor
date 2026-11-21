#!/usr/bin/env python3
"""Build application images or assemble the public V1.0 offline bundle."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

REPO = Path(__file__).resolve().parents[2]
VERSION = "V1.0"
APPLICATIONS = ("center", "agent", "notification-receiver", "cadvisor")
ARCHIVES = tuple("avops-" + role + "-linux-amd64.tar" for role in (*APPLICATIONS, "probe")) + ("avops-postgres-17-linux-amd64.tar",)
BINARIES = ("center", "agent", "provision", "pgprovision", "dbmigrate", "canary", "notification-receiver")
DOCUMENTS = ("VERSION", "产品PRD.md", "技术实现.md", "部署说明.md")


def digest(file):
    result = hashlib.sha256()
    with file.open("rb") as source:
        for block in iter(lambda: source.read(1 << 20), b""):
            result.update(block)
    return result.hexdigest()


def build_images(args):
    args.output.mkdir(parents=True, exist_ok=True)
    images = []
    templates = Path(__file__).resolve().parent / "images"
    for role in APPLICATIONS:
        with tempfile.TemporaryDirectory(prefix="avops-image-") as directory:
            root = Path(directory)
            shutil.copyfile(args.bin / role, root / role)
            (root / role).chmod(0o755)
            shutil.copyfile(args.ca, root / "ca-bundle.crt")
            shutil.copyfile(templates / ("Dockerfile." + role), root / "Dockerfile")
            with (root / "Dockerfile").open("a") as file:
                file.write('\nLABEL org.opencontainers.image.version="' + VERSION + '"\n')
            image = "avops-" + role + ":" + VERSION
            subprocess.run(["docker", "build", "--network", "none", "-t", image, str(root)], check=True, stdout=subprocess.DEVNULL)
            info = json.loads(subprocess.check_output(["docker", "image", "inspect", image]))[0]
            if info["Architecture"] != "amd64" or info["Os"] != "linux":
                raise RuntimeError("Linux amd64 image required")
            archive = args.output / ("avops-" + role + "-linux-amd64.tar")
            with archive.open("wb") as output:
                subprocess.run(["docker", "save", image], stdout=output, check=True)
            images.append({"image": image, "id": info["Id"], "platform": "linux/amd64", "archive": archive.name, "binary_sha256": digest(root / role)})
    (args.output / "image-manifest.json").write_text(json.dumps({"version": VERSION, "images": images}, indent=2) + "\n")
    print("V1.0 application images exported.")


def copy_public_source(repo, destination, excluded_paths=()):
    excluded = {".git", "node_modules", "build", "releases", "__pycache__", ".DS_Store", "data", "dist"}
    private_suffixes = {".env", ".pem", ".key", ".log", ".jsonl", ".dump", ".sqlite", ".db", ".pid", ".tar", ".gz", ".zip"}
    private_names = {"admin-curl.conf", "receiver-curl.conf", *DOCUMENTS[1:]}
    excluded_roots = {path.resolve() for path in (*excluded_paths, destination)}
    for directory, folders, files in os.walk(repo):
        folders[:] = [name for name in folders if name not in excluded and (Path(directory) / name).resolve() not in excluded_roots]
        for name in files:
            source = Path(directory) / name
            if source.suffix in private_suffixes or name in private_names or name.startswith(".env") or name in excluded:
                continue
            target = destination / source.relative_to(repo)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)


def assemble_bundle(args):
    repo = args.repo.resolve()
    if (repo / "VERSION").read_text().strip() != VERSION:
        raise RuntimeError("V1.0 source tree required")
    for name in ARCHIVES:
        if not (args.images / name).is_file():
            raise RuntimeError("Required image archive missing: " + name)
    root = args.output / "avops-monitor-V1.0-linux-amd64"
    root.mkdir(parents=True, exist_ok=False)
    for directory in ("bin", "images", "deploy", "source"):
        (root / directory).mkdir()
    for name in BINARIES:
        if (args.bin / name).is_file():
            shutil.copyfile(args.bin / name, root / "bin" / name)
            (root / "bin" / name).chmod(0o755)
    for name in ARCHIVES:
        shutil.copyfile(args.images / name, root / "images" / name)
    for name in ("image-manifest.json", "probe-manifest.json"):
        if (args.images / name).exists():
            shutil.copyfile(args.images / name, root / "images" / name)
    for name in ("install.py", "backup-postgres.sh", "verify-bundle-images.py"):
        shutil.copyfile(repo / "monitor/deploy" / name, root / "deploy" / name)
    (root / "deploy/probe").mkdir()
    for name in ("install.py", "upgrade.py"):
        shutil.copyfile(repo / "rtc_probe/tools/deploy" / name, root / "deploy/probe" / name)
    for name in DOCUMENTS:
        source = repo / name
        if not source.exists():
            source = repo.parent / name
        shutil.copyfile(source, root / name)
    copy_public_source(repo, root / "source", excluded_paths=(root,))
    subprocess.run(["python3", str(root / "deploy/verify-bundle-images.py"), str(root)], check=True)
    lines = [digest(file) + "  " + str(file.relative_to(root)) for file in sorted(root.rglob("*")) if file.is_file()]
    (root / "SHA256SUMS").write_text("\n".join(lines) + "\n")
    archive = root.parent / (root.name + ".tar.gz")
    with tarfile.open(archive, "w:gz", compresslevel=6) as output:
        output.add(root, arcname=root.name)
    (archive.parent / (archive.name + ".sha256")).write_text(digest(archive) + "  " + archive.name + "\n")
    print(str(archive))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    images = commands.add_parser("images", help="Build and export the four application images")
    images.add_argument("--bin", type=Path, required=True)
    images.add_argument("--ca", type=Path, required=True)
    images.add_argument("--output", type=Path, required=True)
    images.set_defaults(run=build_images)
    bundle = commands.add_parser("bundle", help="Verify and assemble the six-image offline release")
    bundle.add_argument("--repo", type=Path, default=REPO)
    bundle.add_argument("--bin", type=Path, required=True)
    bundle.add_argument("--images", type=Path, required=True)
    bundle.add_argument("--output", type=Path, default=REPO / "releases")
    bundle.set_defaults(run=assemble_bundle)
    args = parser.parse_args()
    args.run(args)


if __name__ == "__main__":
    main()
