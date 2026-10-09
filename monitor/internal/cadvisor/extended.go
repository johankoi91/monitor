package cadvisor

import (
	"bufio"
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func counterDelta(before, after *uint64) *uint64 {
	if before == nil || after == nil || *after < *before {
		return nil
	}
	delta := *after - *before
	return &delta
}
func counterRate(before, after *uint64, seconds float64) *float64 {
	delta := counterDelta(before, after)
	if delta == nil || seconds <= 0 {
		return nil
	}
	value := float64(*delta) / seconds
	return &value
}
func deviceRates(before, after []deviceStats, seconds float64) (*float64, *float64) {
	// A mapped device and its backing block device may account for the same I/O.
	// Never add counters across devices without a verified device hierarchy.
	if len(before) != 1 || len(after) != 1 {
		return nil, nil
	}
	if len(before) == 0 || len(after) == 0 || len(before) != len(after) {
		return nil, nil
	}
	type key struct{ major, minor uint64 }
	old := map[key]deviceStats{}
	for _, d := range before {
		k := key{d.Major, d.Minor}
		if _, ok := old[k]; ok {
			return nil, nil
		}
		old[k] = d
	}
	seen := map[key]bool{}
	var read, write float64
	for _, d := range after {
		k := key{d.Major, d.Minor}
		b, ok := old[k]
		if !ok || seen[k] {
			return nil, nil
		}
		seen[k] = true
		r := counterRate(b.Stats["Read"], d.Stats["Read"], seconds)
		w := counterRate(b.Stats["Write"], d.Stats["Write"], seconds)
		if r == nil || w == nil {
			return nil, nil
		}
		read += *r
		write += *w
	}
	return &read, &write
}

var deviceName = regexp.MustCompile(`^/dev/[a-zA-Z0-9._/-]{1,128}$`)

func diskDevices(before, after, beforeOps, afterOps []deviceStats, seconds float64) []model.DiskDeviceMetrics {
	if len(after) > 32 || len(before) != len(after) {
		return nil
	}
	type key struct{ major, minor uint64 }
	old := map[key]deviceStats{}
	oldOps := map[key]deviceStats{}
	newOps := map[key]deviceStats{}
	for _, d := range before {
		k := key{d.Major, d.Minor}
		if _, ok := old[k]; ok {
			return nil
		}
		old[k] = d
	}
	for _, d := range beforeOps {
		oldOps[key{d.Major, d.Minor}] = d
	}
	for _, d := range afterOps {
		newOps[key{d.Major, d.Minor}] = d
	}
	result := []model.DiskDeviceMetrics{}
	seen := map[key]bool{}
	for _, d := range after {
		k := key{d.Major, d.Minor}
		b, ok := old[k]
		if !ok || seen[k] {
			return nil
		}
		seen[k] = true
		read, write := deviceRates([]deviceStats{b}, []deviceStats{d}, seconds)
		if read == nil || write == nil {
			continue
		}
		name := strconv.FormatUint(d.Major, 10) + ":" + strconv.FormatUint(d.Minor, 10)
		if deviceName.MatchString(d.Device) {
			name = d.Device
		}
		m := model.DiskDeviceMetrics{Device: name, Major: d.Major, Minor: d.Minor, ReadBPS: read, WriteBPS: write}
		bo, bok := oldOps[k]
		ao, aok := newOps[k]
		if bok && aok {
			m.ReadOpsPerSecond, m.WriteOpsPerSecond = deviceRates([]deviceStats{bo}, []deviceStats{ao}, seconds)
		}
		result = append(result, m)
	}
	return result
}

// Read the Docker data filesystem, not each container's shared backing-device
// counters. Statfs is available on Linux/Darwin; unsupported inode counts stay nil.
func hostFilesystem(root string, at time.Time) *model.HostFilesystem {
	var fs syscall.Statfs_t
	if syscall.Statfs(root, &fs) != nil || fs.Bsize <= 0 || fs.Blocks == 0 || fs.Bfree > fs.Blocks || uint64(fs.Bsize) > math.MaxUint64/fs.Blocks {
		return nil
	}
	unit := uint64(fs.Bsize)
	if fs.Bavail > fs.Blocks {
		return nil
	}
	m := &model.HostFilesystem{Scope: "HOST_DOCKER_STORAGE", Source: "agent_statfs", CollectedAt: at, CapacityBytes: fs.Blocks * unit, FreeBytes: fs.Bfree * unit, AvailableBytes: fs.Bavail * unit, UsedRatio: float64(fs.Blocks-fs.Bfree) / float64(fs.Blocks)}
	if fs.Files > 0 && fs.Ffree <= fs.Files {
		total, free := fs.Files, fs.Ffree
		ratio := float64(total-free) / float64(total)
		m.InodesTotal = &total
		m.InodesFree = &free
		m.InodesUsedRatio = &ratio
	}
	return m
}

var oomID = regexp.MustCompile(`(?:\{|,)id="([^"]+)"`)

// OOM event counters are exposed by cAdvisor's Prometheus endpoint, not v2 stats.
// They start at collector startup; only the exact ID and numeric counter survive.
func (c *Client) fetchOOM(ctx context.Context) (map[string]uint64, time.Time) {
	u, _ := url.Parse(c.endpoint)
	u.Path = "/metrics"
	u.RawQuery = ""
	r, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, time.Time{}
	}
	response, err := c.http.Do(r)
	if err != nil {
		return nil, time.Time{}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, time.Time{}
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(b) > maxBody {
		return nil, time.Time{}
	}
	return parseOOM(b), time.Now().UTC()
}
func parseOOM(data []byte) map[string]uint64 {
	result := map[string]uint64{}
	duplicates := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "container_oom_events_total{") {
			continue
		}
		end := strings.LastIndex(line, "}")
		if end < 0 {
			continue
		}
		id := oomID.FindStringSubmatch(line[:end+1])
		if id == nil {
			continue
		}
		match := dockerPath.FindStringSubmatch(id[1])
		if match == nil {
			continue
		}
		values := strings.Fields(line[end+1:])
		if len(values) == 0 {
			continue
		}
		value, err := strconv.ParseFloat(values[0], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1<<53 || value != math.Trunc(value) {
			continue
		}
		if _, exists := result[match[1]]; exists {
			duplicates[match[1]] = true
		}
		result[match[1]] = uint64(value)
	}
	if scanner.Err() != nil {
		return nil
	}
	for id := range duplicates {
		delete(result, id)
	}
	return result
}
