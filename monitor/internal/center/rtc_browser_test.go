package center

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/testkit"
	"github.com/johankoi91/monitor/runtime/internal/testfixture"
	"github.com/johankoi91/monitor/runtime/web"
)

func TestRTCUIMockWorker(t *testing.T) {
	if os.Getenv("RTC_UI_WORKER") != "1" {
		return
	}
	pipe := os.NewFile(3, "evidence")
	err := testkit.Run(context.Background(), os.Stdin, pipe)
	pipe.Close()
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestRTCUIWorkflow(t *testing.T) {
	if os.Getenv("AVOPS_BROWSER_TEST") != "1" {
		t.Skip("opt-in browser verification")
	}
	config := hub.Config{Probes: []hub.ProbeConfig{{ID: "probe-a", Secret: strings.Repeat("a", 32), Location: "模拟位置 A", HostAddress: "127.0.0.1"}, {ID: "probe-b", Secret: strings.Repeat("b", 32), Location: "模拟位置 B", HostAddress: "127.0.0.1"}}}
	h, err := hub.New(t.TempDir(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	assets, err := fs.Sub(web.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(testfixture.AccountAuth((&Server{Store: setup(t), Probes: h, AdminID: "fixture", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler()))
	defer server.Close()
	done := []chan error{}
	for _, p := range config.Probes {
		c, err := probe.New(probe.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probe/connect", ID: p.ID, Secret: p.Secret, DataDir: t.TempDir(), Driver: "agora", AllowPlaintextLoopback: true, WorkerCommand: func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRTCUIMockWorker$")
			cmd.Env = append(os.Environ(), "RTC_UI_WORKER=1")
			return cmd
		}})
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan error, 1)
		done = append(done, ch)
		go func() { ch <- c.Run(ctx) }()
	}
	defer func() {
		cancel()
		for _, ch := range done {
			select {
			case err := <-ch:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(8 * time.Second):
				t.Error("probe shutdown timed out")
			}
		}
	}()
	if stopFile := os.Getenv("AVOPS_CUA_STOP_FILE"); stopFile != "" {
		t.Logf("CUA fixture: %s", server.URL)
		timeout := time.NewTimer(20 * time.Minute)
		defer timeout.Stop()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-timeout.C:
				t.Fatal("manual UI verification timed out")
			case <-tick.C:
				if _, err := os.Stat(stopFile); err == nil {
					return
				}
			}
		}
	}
	script, err := filepath.Abs("../../tests/rtc-ui.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AVOPS_UI_URL="+server.URL, "AVOPS_ISOLATED_UI_TEST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("RTC browser workflow: %v\n%s", err, out)
	}
	t.Log(string(out))
}
