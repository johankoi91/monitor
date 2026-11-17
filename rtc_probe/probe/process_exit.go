package probe

import (
	"context"
	"errors"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"os/exec"
	"syscall"
	"time"
)

func processExit(cmd *exec.Cmd, startErr error, ctx context.Context) protocol.ProcessExitInfo {
	info := protocol.ProcessExitInfo{At: time.Now().UTC(), Started: startErr == nil, DeadlineExceeded: errors.Is(ctx.Err(), context.DeadlineExceeded), ForcedCancellation: errors.Is(ctx.Err(), context.Canceled)}
	if cmd.Process != nil {
		info.PID = cmd.Process.Pid
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		info.ExitCode = &code
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			info.Signal = status.Signal().String()
		}
	}
	return info
}
