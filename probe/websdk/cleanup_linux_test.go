//go:build linux

package websdk

import (
	"flag"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var profileFlag = flag.String("user-data-dir", "", "")

func TestBrowserProcessWorker(t *testing.T) {
	if os.Getenv("WEB_CLEANUP_TEST_WORKER") != "1" {
		return
	}
	os.WriteFile(*profileFlag+".ready", []byte("ready"), 0600)
	time.Sleep(30 * time.Second)
}
func TestCrashCleanupFindsOnlyThisRunEvenBeforePIDMarker(t *testing.T) {
	job := protocol.Job{RunID: "cleanup", WorkDir: t.TempDir()}
	start := func(dir string) *exec.Cmd {
		profile := filepath.Join(dir, "browser-tmp", "profile")
		os.MkdirAll(filepath.Dir(profile), 0700)
		cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserProcessWorker$", "--user-data-dir="+profile)
		cmd.Env = append(os.Environ(), "WEB_CLEANUP_TEST_WORKER=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill() })
		limit := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(profile + ".ready"); err == nil {
				break
			}
			if time.Now().After(limit) {
				t.Fatal("child not ready")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return cmd
	}
	owned, foreign := start(job.WorkDir), start(t.TempDir())
	if err := CleanupBrowser(job); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- owned.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("orphan browser survived")
	}
	if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unrelated browser killed", err)
	}
	foreign.Process.Kill()
	foreign.Wait()
}
