//go:build linux

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/websdk"
)

func main() {
	cfg, err := probe.EnvironmentConfig(protocol.WebDriver)
	if err != nil {
		log.Fatal(err)
	}
	runtimeDir := os.Getenv("AVOPS_PROBE_WEB_RUNTIME")
	if runtimeDir == "" {
		runtimeDir = "/opt/rtc-probe/websdk/custom-video"
	}
	cfg.RuntimeVersion = protocol.ProductVersion
	cfg.CleanupWorker = websdk.CleanupBrowser
	cfg.WorkerCommand = func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "node", filepath.Join(runtimeDir, "worker.mjs"))
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "AVOPS_PROBE_ENROLL_TOKEN=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
		return cmd
	}
	client, err := probe.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = client.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
