//go:build linux

package websdk

import (
	"bytes"
	"fmt"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// The browser may detach from Node before its startup promise resolves. Match
// the per-run profile directory so crash cleanup also covers that early window.
func CleanupBrowser(job protocol.Job) error {
	if job.WorkDir == "" {
		return nil
	}
	profile := []byte("--user-data-dir=" + filepath.Join(job.WorkDir, "browser-tmp") + "/")
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || !bytes.Contains(cmd, profile) {
			continue
		}
		group, err := syscall.Getpgid(pid)
		if err == syscall.ESRCH {
			continue
		}
		if err != nil {
			return err
		}
		target := pid
		if group == pid {
			target = -group
		}
		if err = syscall.Kill(target, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("cleanup browser %d: %w", pid, err)
		}
	}
	return nil
}
