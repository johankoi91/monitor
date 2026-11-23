package deploy

import (
	"context"
	"errors"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func ImageContext(ctx context.Context, engine d.Docker, repo, native, webImage, supervisor, webBinary, ca, output string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:@-]*$`).MatchString(webImage) {
		return errors.New("valid build runtime image required")
	}
	if e := d.NewDirectory(output); e != nil {
		return e
	}
	b, e := engine.Output(ctx, "create", webImage)
	if e != nil {
		return e
	}
	container := strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(container) {
		return errors.New("invalid temporary container ID")
	}
	defer engine.Run(context.Background(), "rm", container)
	system := filepath.Join(output, "system")
	// Resolve system libraries against the Web build image's libc. Injecting
	// the Native build host's libc into this image can prevent ldd itself from
	// starting; only FFmpeg's separately built libraries belong on this path.
	ffmpeg, e := os.MkdirTemp("", "avops-ffmpeg-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(ffmpeg)
	if e = os.Chmod(ffmpeg, 0755); e != nil {
		return e
	}
	for _, pattern := range []string{"libav*.so.*", "libswresample.so.*"} {
		paths, _ := filepath.Glob(filepath.Join(native, "system/opt/rtc-probe/runtime-libs", pattern))
		for _, path := range paths {
			if e = d.CopyFile(path, filepath.Join(ffmpeg, filepath.Base(path)), 0755); e != nil {
				return e
			}
		}
	}
	copyContainer := func(source string, optional bool) error {
		target := filepath.Join(system, strings.TrimPrefix(source, "/"))
		if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		_, e := engine.Output(ctx, "cp", "-L", container+":"+source, target)
		if e != nil && !optional {
			return errors.New("runtime dependency unavailable: " + source)
		}
		return nil
	}
	node := "/usr/local/bin/node"
	chrome := "/ms-playwright/chromium_headless_shell-1194/chrome-linux/headless_shell"
	ldd := func(paths []string, mountNative bool) (string, error) {
		args := []string{"run", "--rm", "--network", "none", "--read-only"}
		if mountNative {
			args = append(args, "-v", filepath.Join(native, "rtc-probe")+":/rtc-probe:ro", "-v", filepath.Join(native, "sdk")+":/opt/agora_sdk:ro", "-v", ffmpeg+":/opt/rtc-probe/native-libs:ro", "-e", "LD_LIBRARY_PATH=/opt/agora_sdk:/opt/rtc-probe/native-libs")
		}
		args = append(args, "--entrypoint", "/usr/bin/ldd", webImage)
		args = append(args, paths...)
		b, e := engine.Output(ctx, args...)
		if e != nil || strings.Contains(string(b), "not found") {
			return "", errors.New("unresolved runtime dependencies for " + strings.Join(paths, ", "))
		}
		return string(b), nil
	}
	linked, e := ldd([]string{node, chrome}, false)
	if e != nil {
		return e
	}
	providers := []string{}
	for _, name := range []string{"libsoftokn3.so", "libfreebl3.so", "libfreeblpriv3.so", "libnssckbi.so"} {
		providers = append(providers, "/lib/x86_64-linux-gnu/"+name)
	}
	extra, e := ldd(providers, false)
	if e != nil {
		return e
	}
	linked += "\n" + extra
	extra, e = ldd([]string{"/rtc-probe"}, true)
	if e != nil {
		return e
	}
	linked += "\n" + extra
	libraries := map[string]bool{}
	for _, path := range regexp.MustCompile(`(/[^\s:]+)`).FindAllString(linked, -1) {
		if path == node || path == chrome || path == "/rtc-probe" || strings.HasPrefix(path, "/opt/agora_sdk/") || strings.HasPrefix(path, "/opt/rtc-probe/native-libs/") {
			continue
		}
		libraries[path] = true
	}
	for _, path := range providers {
		libraries[path] = true
	}
	for path := range libraries {
		if e = copyContainer(path, false); e != nil {
			return e
		}
	}
	for _, path := range []string{node, "/ms-playwright", "/opt/rtc-probe/websdk/custom-video/node_modules", "/etc/ssl/certs/ca-certificates.crt", "/usr/share/fonts", "/etc/fonts", "/usr/share/zoneinfo", "/etc/os-release"} {
		if e = copyContainer(path, false); e != nil {
			return e
		}
	}
	for _, name := range []string{"libpthread.so.0", "libdl.so.2", "librt.so.1", "libresolv.so.2", "libnss_dns.so.2", "libnss_files.so.2"} {
		copyContainer("/lib/x86_64-linux-gnu/"+name, true)
	}
	copyContainer("/usr/share/doc", true)
	for name, path := range map[string]string{"rtc-probe": filepath.Join(native, "rtc-probe"), "probe-server": supervisor, "rtc-web-probe": webBinary} {
		if e = d.CopyFile(path, filepath.Join(system, name), 0755); e != nil {
			return e
		}
	}
	for from, to := range map[string]string{"sdk": "opt/agora_sdk", "test_data": "opt/rtc-probe/test_data"} {
		if e = d.CopyTree(filepath.Join(native, from), filepath.Join(system, to)); e != nil {
			return e
		}
	}
	if e = d.CopyFile(filepath.Join(native, "system/opt/rtc-probe/libroute_capture.so"), filepath.Join(system, "opt/rtc-probe/libroute_capture.so"), 0755); e != nil {
		return e
	}
	for _, pattern := range []string{"libav*.so.*", "libswresample.so.*"} {
		paths, _ := filepath.Glob(filepath.Join(native, "system/opt/rtc-probe/runtime-libs", pattern))
		for _, path := range paths {
			if e = d.CopyFile(path, filepath.Join(system, "opt/rtc-probe/native-libs", filepath.Base(path)), 0755); e != nil {
				return e
			}
		}
	}
	if e = CopyRuntime(repo, filepath.Join(system, "opt/rtc-probe/websdk/custom-video")); e != nil {
		return e
	}
	if ca != "" {
		target := filepath.Join(system, "etc/ssl/certs/ca-certificates.crt")
		if e = os.Remove(target); e != nil {
			return e
		}
		if e = d.CopyFile(ca, target, 0644); e != nil {
			return e
		}
	}
	for name, content := range map[string]string{"passwd": "root:x:0:0:root:/root:/bin/false\nprobe:x:65532:65532:probe:/tmp:/bin/false\n", "group": "root:x:0:\nprobe:x:65532:\n", "nsswitch.conf": "passwd: files\ngroup: files\nhosts: files dns\n"} {
		if e = os.WriteFile(filepath.Join(system, "etc", name), []byte(content), 0644); e != nil {
			return e
		}
	}
	return os.WriteFile(filepath.Join(output, "Dockerfile"), []byte(combinedDockerfile), 0644)
}

const combinedDockerfile = `FROM scratch
COPY system/ /
WORKDIR /opt/rtc-probe/websdk/custom-video
ENV PATH=/usr/local/bin:/usr/bin:/bin HOME=/tmp
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 PLAYWRIGHT_BROWSERS_PATH=/ms-playwright PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=1
ENV CHROME_PATH=/ms-playwright/chromium_headless_shell-1194/chrome-linux/headless_shell
ENV AVOPS_PROBE_WEB_RUNTIME=/opt/rtc-probe/websdk/custom-video
ENV AVOPS_PROBE_HOST_DIR=/host AVOPS_PROBE_DATA_DIR=/data AVOPS_PROBE_NATIVE_CAPACITY=1 AVOPS_PROBE_WEB_CAPACITY=1
LABEL avops.role="probe" avops.layout="three-images" avops.deployment="compose-v1" avops.probe.schema="2" avops.probe.service="rtc" avops.probe.clients="native,web" org.opencontainers.image.version="V1.0"
USER 0:0
ENTRYPOINT ["/probe-server"]
HEALTHCHECK --interval=15s --timeout=3s --start-period=60s --retries=3 CMD ["/probe-server", "health"]
`
