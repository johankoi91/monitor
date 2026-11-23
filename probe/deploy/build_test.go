package deploy

import (
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebContextIncludesAllRuntimeModules(t *testing.T) {
	repo, e := d.FindRepo(".")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	os.Mkdir(cache, 0755)
	for name := range webAssets {
		os.WriteFile(filepath.Join(cache, name), []byte("fixture"), 0644)
	}
	binary := filepath.Join(root, "rtc-web-probe")
	os.WriteFile(binary, []byte("fixture"), 0755)
	output := filepath.Join(root, "web")
	if e = WebContext(repo, cache, binary, output); e != nil {
		t.Fatal(e)
	}
	for _, name := range append(append([]string{}, runtimeFiles...), "browser/runtime.html", "sdk/AgoraRTC_N.js", "fixtures/probe.webm") {
		if _, e = os.Stat(filepath.Join(output, "custom-video", name)); e != nil {
			t.Fatal("runtime asset omitted", name)
		}
	}
	ignore, _ := os.ReadFile(filepath.Join(output, ".dockerignore"))
	if !strings.Contains(string(ignore), "!custom-video/**") {
		t.Fatal("Docker context drops runtime modules")
	}
}
