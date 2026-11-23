package deploy

import (
	"context"
	"errors"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type BuildOptions struct{ Repo, Output, Cache, CA, NativeBinary, WebImage string }

func Binaries(ctx context.Context, repo, output, nativeBinary, cache string) error {
	if e := os.MkdirAll(output, 0755); e != nil {
		return e
	}
	for name, path := range map[string]string{"probe-server": "probe/cmd/probe-server", "rtc-web-probe": "probe/cmd/rtc-web-probe"} {
		if e := d.GoBuild(ctx, repo, path, filepath.Join(output, name), false, ""); e != nil {
			return e
		}
	}
	if nativeBinary != "" {
		return d.CopyFile(nativeBinary, filepath.Join(output, "rtc-probe"), 0755)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("Native SDK compilation requires Linux amd64; use --native-binary for a prebuilt SDK worker")
	}
	staging, e := os.MkdirTemp("", "avops-native-build-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(staging)
	sdk := filepath.Join(staging, "sdk")
	if e = d.CopyTree(filepath.Join(repo, "probe/third_party/Agora-Golang-Server-SDK-2.6.6"), sdk); e != nil {
		return e
	}
	if e = os.RemoveAll(filepath.Join(sdk, "agora_sdk")); e != nil {
		return e
	}
	if e = d.CopyTree(filepath.Join(cache, "sdk/agora_sdk"), filepath.Join(sdk, "agora_sdk")); e != nil {
		return e
	}
	for source, target := range map[string]string{"go.mod": "native.mod", "go.sum": "native.sum"} {
		if e = d.CopyFile(filepath.Join(repo, source), filepath.Join(staging, target), 0644); e != nil {
			return e
		}
	}
	mod := filepath.Join(staging, "native.mod")
	if e = d.Run(ctx, repo, nil, "go", "mod", "edit", "-modfile="+mod, "-replace=github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2="+sdk); e != nil {
		return e
	}
	env := append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS=-modfile="+mod, "LD_LIBRARY_PATH="+filepath.Join(sdk, "agora_sdk")+":"+os.Getenv("LD_LIBRARY_PATH"))
	return d.Run(ctx, repo, env, "go", "build", "-tags", "agora", "-trimpath", "-ldflags", "-s -w", "-o", filepath.Join(output, "rtc-probe"), "./probe/cmd/rtc-probe")
}
func NativeContext(ctx context.Context, repo, binary, sdk, media, output string) error {
	if e := d.NewDirectory(output); e != nil {
		return e
	}
	environment := append(os.Environ(), "LD_LIBRARY_PATH="+sdk+":"+os.Getenv("LD_LIBRARY_PATH"))
	command := exec.CommandContext(ctx, "ldd", binary)
	command.Env = environment
	linked, e := command.Output()
	if e != nil || strings.Contains(string(linked), "not found") {
		return errors.New("native worker has unresolved Linux dependencies")
	}
	flat := filepath.Join(output, "system/opt/rtc-probe/runtime-libs")
	if e = os.MkdirAll(flat, 0755); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, path := range regexp.MustCompile(`(?:=>\s*)?(/[^\s]+)`).FindAllString(string(linked), -1) {
		path = strings.TrimPrefix(path, "=> ")
		path = strings.TrimSpace(path)
		info, e := os.Stat(path)
		if e != nil || !info.Mode().IsRegular() || d.Inside(sdk, path) || !strings.Contains(filepath.Base(path), ".so") || seen[filepath.Base(path)] {
			continue
		}
		seen[filepath.Base(path)] = true
		if e = d.CopyFile(path, filepath.Join(flat, filepath.Base(path)), info.Mode().Perm()); e != nil {
			return e
		}
	}
	capture := filepath.Join(output, "system/opt/rtc-probe/libroute_capture.so")
	if e = d.Run(ctx, repo, nil, "gcc", "-shared", "-fPIC", "-O2", "-o", capture, filepath.Join(repo, "probe/native/route_capture.c"), "-ldl", "-pthread"); e != nil {
		return e
	}
	if e = d.CopyFile(binary, filepath.Join(output, "rtc-probe"), 0755); e != nil {
		return e
	}
	if e = d.CopyTree(sdk, filepath.Join(output, "sdk")); e != nil {
		return e
	}
	return d.CopyTree(media, filepath.Join(output, "test_data"))
}

var runtimeFiles = []string{"package.json", "package-lock.json", "worker.mjs", "server.mjs", "contract.mjs", "remote-stats.mjs"}

func CopyRuntime(repo, target string) error {
	source := filepath.Join(repo, "probe/websdk/custom-video")
	for _, name := range runtimeFiles {
		if e := d.CopyFile(filepath.Join(source, name), filepath.Join(target, name), 0644); e != nil {
			return e
		}
	}
	for _, name := range []string{"browser", "sdk", "fixtures"} {
		if e := d.CopyTree(filepath.Join(source, name), filepath.Join(target, name)); e != nil {
			return e
		}
	}
	return nil
}
func WebContext(repo, cache, webBinary, output string) error {
	if e := d.NewDirectory(output); e != nil {
		return e
	}
	for name := range webAssets {
		if e := d.CopyFile(filepath.Join(cache, name), filepath.Join(output, "build", name), 0644); e != nil {
			return e
		}
	}
	if e := d.CopyFile(filepath.Join(repo, "probe/deploy/runtime.Dockerfile"), filepath.Join(output, "Dockerfile"), 0644); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(output, ".dockerignore"), []byte("*\n!Dockerfile\n!build/\n!build/**\n!custom-video/\n!custom-video/**\n"), 0644); e != nil {
		return e
	}
	if e := d.CopyFile(webBinary, filepath.Join(output, "build/rtc-web-probe"), 0755); e != nil {
		return e
	}
	return CopyRuntime(repo, filepath.Join(output, "custom-video"))
}
func Build(ctx context.Context, engine d.Docker, o BuildOptions) error {
	if o.Cache == "" {
		o.Cache = filepath.Join(o.Output, "cache/probe")
	}
	if e := Dependencies(ctx, o.Cache); e != nil {
		return e
	}
	bin := filepath.Join(o.Output, "bin/probe")
	if e := Binaries(ctx, o.Repo, bin, o.NativeBinary, o.Cache); e != nil {
		return e
	}
	staging, e := os.MkdirTemp("", "avops-probe-image-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(staging)
	native := filepath.Join(staging, "native")
	if e = NativeContext(ctx, o.Repo, filepath.Join(bin, "rtc-probe"), filepath.Join(o.Cache, "sdk/agora_sdk"), filepath.Join(o.Cache, "media"), native); e != nil {
		return e
	}
	webImage := o.WebImage
	if webImage == "" {
		webContext := filepath.Join(staging, "web")
		if e = WebContext(o.Repo, o.Cache, filepath.Join(bin, "rtc-web-probe"), webContext); e != nil {
			return e
		}
		webImage = "avops-probe-build-runtime:V1.0"
		if e = engine.Run(ctx, "build", "--pull=false", "-t", webImage, webContext); e != nil {
			return e
		}
	}
	combined := filepath.Join(staging, "combined")
	if e = ImageContext(ctx, engine, o.Repo, native, webImage, filepath.Join(bin, "probe-server"), filepath.Join(bin, "rtc-web-probe"), o.CA, combined); e != nil {
		return e
	}
	if e = d.CopyFile(filepath.Join(bin, "probectl"), filepath.Join(o.Output, "tools/probectl"), 0755); e != nil {
		return e
	}
	return d.ExportImage(ctx, engine, "probe", combined, o.Output)
}
