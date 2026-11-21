package probe

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// The resource ceiling is independent of the number of discovered Edges.
// The center dispatches at most min(Edge count, this ceiling, global capacity).
func workerCapacity(cpus int, quota, period, memory int64, memoryPerWorker ...int64) int {
	if cpus < 1 {
		cpus = 1
	}
	if quota > 0 && period > 0 {
		slots := int((quota + period - 1) / period)
		if slots < cpus {
			cpus = slots
		}
	}
	if memory > 0 {
		per := int64(256 << 20)
		if len(memoryPerWorker) > 0 && memoryPerWorker[0] > 0 {
			per = memoryPerWorker[0]
		}
		slots := int((memory - (256 << 20)) / per)
		if slots < 1 {
			slots = 1
		}
		if slots < cpus {
			cpus = slots
		}
	}
	if cpus > protocol.MaxWorkerCapacity {
		cpus = protocol.MaxWorkerCapacity
	}
	return cpus
}
func resourceWorkerCapacity(memoryPerWorker ...int64) int {
	read := func(paths ...string) int64 {
		for _, path := range paths {
			if data, err := os.ReadFile(path); err == nil {
				if n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil && n > 0 && n < (1<<60) {
					return n
				}
			}
		}
		return 0
	}
	quota, period := int64(0), int64(0)
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) == 2 {
			quota, _ = strconv.ParseInt(parts[0], 10, 64)
			period, _ = strconv.ParseInt(parts[1], 10, 64)
		}
	} else {
		quota = read("/sys/fs/cgroup/cpu/cpu.cfs_quota_us", "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us")
		period = read("/sys/fs/cgroup/cpu/cpu.cfs_period_us", "/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us")
	}
	memory := read("/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes")
	return workerCapacity(runtime.NumCPU(), quota, period, memory, memoryPerWorker...)
}
