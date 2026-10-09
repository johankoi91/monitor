// Package cadvisor reads local cAdvisor v2 stats and attaches only numeric fields
// to Docker observations. It cannot change Docker, health, or restart decisions.
package cadvisor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

const maxBody = 16 << 20

var dockerPath = regexp.MustCompile(`(?:^|/)(?:docker/|docker-)([a-f0-9]{64})(?:\.scope)?$`)

type Client struct {
	endpoint       string
	http           *http.Client
	FilesystemRoot string
}

func New(endpoint string) (*Client, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("AVOPS_CADVISOR_URL must be a credential-free loopback HTTP base URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() == "localhost" {
		u.Host = net.JoinHostPort("127.0.0.1", port(u))
	} else if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("cAdvisor endpoint must be loopback; do not expose its API publicly")
	}
	u.Path = "/api/v2.0/stats/"
	u.RawQuery = "type=docker&count=2&recursive=true"
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 32 << 10}
	return &Client{endpoint: u.String(), http: &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func port(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	return "80"
}

type stats struct {
	Timestamp time.Time `json:"timestamp"`
	HasDiskIO *bool     `json:"has_diskio"`
	CPU       *struct {
		Usage struct {
			Total  *uint64 `json:"total"`
			User   *uint64 `json:"user"`
			System *uint64 `json:"system"`
		} `json:"usage"`
		CFS *struct {
			Periods          *uint64 `json:"periods"`
			ThrottledPeriods *uint64 `json:"throttled_periods"`
			ThrottledTime    *uint64 `json:"throttled_time"`
		} `json:"cfs"`
	} `json:"cpu"`
	Memory *struct {
		Usage      *uint64 `json:"usage"`
		WorkingSet *uint64 `json:"working_set"`
		RSS        *uint64 `json:"rss"`
		Cache      *uint64 `json:"cache"`
		Swap       *uint64 `json:"swap"`
		MaxUsage   *uint64 `json:"max_usage"`
		Failures   *uint64 `json:"failcnt"`
	} `json:"memory"`
	Network *struct {
		Interfaces []iface `json:"interfaces"`
	} `json:"network"`
	Filesystem []struct {
		Device string  `json:"device"`
		Usage  *uint64 `json:"usage"`
	} `json:"filesystem"`
	DiskIO *struct {
		Bytes      []deviceStats `json:"io_service_bytes"`
		Operations []deviceStats `json:"io_serviced"`
	} `json:"diskio"`
}
type deviceStats struct {
	Device string             `json:"device"`
	Major  uint64             `json:"major"`
	Minor  uint64             `json:"minor"`
	Stats  map[string]*uint64 `json:"stats"`
}
type iface struct {
	Name      string  `json:"name"`
	RX        *uint64 `json:"rx_bytes"`
	TX        *uint64 `json:"tx_bytes"`
	RXPackets *uint64 `json:"rx_packets"`
	TXPackets *uint64 `json:"tx_packets"`
	RXDropped *uint64 `json:"rx_dropped"`
	TXDropped *uint64 `json:"tx_dropped"`
	RXErrors  *uint64 `json:"rx_errors"`
	TXErrors  *uint64 `json:"tx_errors"`
}

func (c *Client) fetch(ctx context.Context) (map[string][]stats, string) {
	r, err := http.NewRequestWithContext(ctx, "GET", c.endpoint, nil)
	if err != nil {
		return nil, "CADVISOR_UNAVAILABLE"
	}
	response, err := c.http.Do(r)
	if err != nil {
		return nil, "CADVISOR_UNAVAILABLE"
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, "CADVISOR_HTTP_ERROR"
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, "CADVISOR_UNAVAILABLE"
	}
	if len(b) > maxBody {
		return nil, "CADVISOR_RESPONSE_TOO_LARGE"
	}
	var raw map[string][]stats
	if json.Unmarshal(b, &raw) != nil || raw == nil || len(raw) > 4096 {
		return nil, "CADVISOR_INVALID_RESPONSE"
	}
	result := map[string][]stats{}
	duplicates := map[string]bool{}
	for path, samples := range raw {
		match := dockerPath.FindStringSubmatch(path)
		if match == nil {
			continue
		}
		id := match[1]
		if _, exists := result[id]; exists {
			duplicates[id] = true
		}
		if len(samples) > 2 {
			return nil, "CADVISOR_INVALID_RESPONSE"
		}
		result[id] = samples
	}
	for id := range duplicates {
		delete(result, id)
	}
	return result, ""
}

// Enrich preserves Docker completeness even if cAdvisor is down or incomplete.
// It uses exact full IDs from the current Docker discovery, never names/labels.
func (c *Client) Enrich(ctx context.Context, report *model.Report) {
	if c == nil || !report.Complete {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	type oomResult struct {
		counts map[string]uint64
		at     time.Time
	}
	oomCh := make(chan oomResult, 1)
	go func() { counts, at := c.fetchOOM(ctx); oomCh <- oomResult{counts, at} }()
	data, reason := c.fetch(ctx)
	ooms := <-oomCh
	now := time.Now().UTC()
	var storage *model.HostFilesystem
	if c.FilesystemRoot != "" && filepath.IsAbs(c.FilesystemRoot) {
		storage = hostFilesystem(c.FilesystemRoot, now)
	}
	for i := range report.Containers {
		o := &report.Containers[i]
		m := &model.ResourceMetrics{Source: "cadvisor", Status: "UNAVAILABLE", ReasonCode: reason, FetchedAt: now, Stale: true, NetworkScope: "UNAVAILABLE"}
		o.Container.Resources = m
		m.HostFilesystem = storage
		m.DiskIOStatus = "UNAVAILABLE"
		if o.Container.Limits != nil {
			m.LimitsKnown = true
			m.CPUQuotaCores = o.Container.Limits.CPUQuotaCores
			m.MemoryLimitBytes = o.Container.Limits.MemoryLimitBytes
		}
		if count, ok := ooms.counts[o.Container.ContainerID]; ok {
			copy := count
			m.OOMEventsTotal = &copy
			m.OOMObservedAt = &ooms.at
		}
		if o.Container.RuntimeStatus != "running" {
			m.ReasonCode = "CONTAINER_NOT_RUNNING"
			continue
		}
		if reason != "" {
			continue
		}
		samples := data[o.Container.ContainerID]
		if len(samples) == 0 {
			m.ReasonCode = "CADVISOR_CONTAINER_NOT_FOUND"
			continue
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i].Timestamp.Before(samples[j].Timestamp) })
		last := samples[len(samples)-1]
		m.SampledAt = &last.Timestamp
		started, err := time.Parse(time.RFC3339Nano, o.StartedAt)
		if last.Timestamp.IsZero() || last.Timestamp.Before(now.Add(-45*time.Second)) || last.Timestamp.After(now.Add(5*time.Second)) || err == nil && last.Timestamp.Before(started) {
			m.ReasonCode = "CADVISOR_SAMPLE_STALE"
			continue
		}
		m.Stale = false
		m.Status = "PARTIAL"
		m.ReasonCode = "PARTIAL_METRICS"
		if last.Memory != nil {
			m.MemoryUsageBytes = last.Memory.Usage
			m.MemoryWorkingSetBytes = last.Memory.WorkingSet
			m.MemoryRSSBytes = last.Memory.RSS
			m.MemoryCacheBytes = last.Memory.Cache
			m.MemorySwapBytes = last.Memory.Swap
			m.MemoryPeakBytes = last.Memory.MaxUsage
			m.MemoryFailuresTotal = last.Memory.Failures
			if m.MemoryLimitBytes != nil && *m.MemoryLimitBytes > 0 && m.MemoryWorkingSetBytes != nil {
				ratio := float64(*m.MemoryWorkingSetBytes) / float64(*m.MemoryLimitBytes)
				m.MemoryWorkingSetRatio = &ratio
			}
		}
		var disk uint64
		diskValid := len(last.Filesystem) > 0
		seen := map[string]bool{}
		for _, fs := range last.Filesystem {
			if fs.Usage == nil || seen[fs.Device] || math.MaxUint64-disk < *fs.Usage {
				diskValid = false
				break
			}
			seen[fs.Device] = true
			disk += *fs.Usage
		}
		if diskValid {
			m.FilesystemUsageBytes = &disk
		}
		if o.Startup != nil && (o.Startup.Config.NetworkMode == "host" || len(o.Startup.Config.NetworkMode) > 10 && o.Startup.Config.NetworkMode[:10] == "container:") {
			m.NetworkScope = "HOST_SHARED"
		} else if last.Network != nil {
			m.NetworkScope = "CONTAINER"
		}
		if len(samples) == 2 {
			previous := samples[0]
			seconds := last.Timestamp.Sub(previous.Timestamp).Seconds()
			// Never derive rates across a restart, a stale baseline, or a reset counter.
			if seconds > 0 && seconds <= 45 && previous.Timestamp.After(now.Add(-45*time.Second)) && (err != nil || !previous.Timestamp.Before(started)) {
				m.SampleWindowSeconds = &seconds
				if last.CPU != nil && previous.CPU != nil {
					m.CPUUserUsageCores = counterRate(previous.CPU.Usage.User, last.CPU.Usage.User, seconds*1e9)
					m.CPUSystemUsageCores = counterRate(previous.CPU.Usage.System, last.CPU.Usage.System, seconds*1e9)
					if last.CPU.CFS != nil && previous.CPU.CFS != nil {
						m.CPUThrottledSecondsPerSecond = counterRate(previous.CPU.CFS.ThrottledTime, last.CPU.CFS.ThrottledTime, seconds*1e9)
						periods := counterDelta(previous.CPU.CFS.Periods, last.CPU.CFS.Periods)
						throttled := counterDelta(previous.CPU.CFS.ThrottledPeriods, last.CPU.CFS.ThrottledPeriods)
						if m.CPUQuotaCores != nil && periods != nil && throttled != nil && *periods > 0 && *throttled <= *periods {
							ratio := float64(*throttled) / float64(*periods)
							m.CPUThrottledRatio = &ratio
						}
					}
				}
				if last.CPU != nil && previous.CPU != nil && last.CPU.Usage.Total != nil && previous.CPU.Usage.Total != nil && *last.CPU.Usage.Total >= *previous.CPU.Usage.Total {
					cores := float64(*last.CPU.Usage.Total-*previous.CPU.Usage.Total) / 1e9 / seconds
					m.CPUUsageCores = &cores
				}
				if m.NetworkScope == "CONTAINER" && previous.Network != nil {
					m.NetworkReceiveBPS, m.NetworkTransmitBPS = networkRates(previous.Network.Interfaces, last.Network.Interfaces, seconds)
					m.NetworkReceivePacketsPerSecond, m.NetworkTransmitPacketsPerSecond = interfaceRates(previous.Network.Interfaces, last.Network.Interfaces, seconds, func(i iface) (*uint64, *uint64) { return i.RXPackets, i.TXPackets })
					m.NetworkReceiveDroppedPerSecond, m.NetworkTransmitDroppedPerSecond = interfaceRates(previous.Network.Interfaces, last.Network.Interfaces, seconds, func(i iface) (*uint64, *uint64) { return i.RXDropped, i.TXDropped })
					m.NetworkReceiveErrorsPerSecond, m.NetworkTransmitErrorsPerSecond = interfaceRates(previous.Network.Interfaces, last.Network.Interfaces, seconds, func(i iface) (*uint64, *uint64) { return i.RXErrors, i.TXErrors })
				}
				if last.DiskIO != nil && previous.DiskIO != nil && (last.HasDiskIO == nil || *last.HasDiskIO) {
					m.DiskDevices = diskDevices(previous.DiskIO.Bytes, last.DiskIO.Bytes, previous.DiskIO.Operations, last.DiskIO.Operations, seconds)
					if len(m.DiskDevices) > 0 {
						m.DiskIOStatus = "AVAILABLE"
						m.DiskIOScope = "PER_DEVICE"
						if len(m.DiskDevices) == 1 {
							d := m.DiskDevices[0]
							m.DiskIOScope = "SINGLE_DEVICE"
							m.DiskReadBPS = d.ReadBPS
							m.DiskWriteBPS = d.WriteBPS
							m.DiskReadOpsPerSecond = d.ReadOpsPerSecond
							m.DiskWriteOpsPerSecond = d.WriteOpsPerSecond
						}
					} else {
						m.DiskIOStatus = "NO_VALID_SAMPLES"
					}
				}
			}
		}
		if last.HasDiskIO != nil && !*last.HasDiskIO {
			m.DiskIOStatus = "UNSUPPORTED"
		}
		if m.CPUUsageCores != nil && m.MemoryUsageBytes != nil && m.MemoryWorkingSetBytes != nil && m.FilesystemUsageBytes != nil && (m.NetworkScope == "HOST_SHARED" || m.NetworkReceiveBPS != nil && m.NetworkTransmitBPS != nil) {
			m.Status = "AVAILABLE"
			m.ReasonCode = "OK"
		}
	}
}

func networkRates(before, after []iface, seconds float64) (*float64, *float64) {
	return interfaceRates(before, after, seconds, func(i iface) (*uint64, *uint64) { return i.RX, i.TX })
}
func interfaceRates(before, after []iface, seconds float64, values func(iface) (*uint64, *uint64)) (*float64, *float64) {
	old := map[string]iface{}
	for _, item := range before {
		if _, ok := old[item.Name]; ok {
			return nil, nil
		}
		old[item.Name] = item
	}
	seen := map[string]bool{}
	var rx, tx float64
	count := 0
	for _, item := range after {
		if item.Name == "lo" {
			continue
		}
		previous, ok := old[item.Name]
		prx, ptx := values(previous)
		crx, ctx := values(item)
		if !ok || seen[item.Name] || crx == nil || ctx == nil || prx == nil || ptx == nil || *crx < *prx || *ctx < *ptx {
			return nil, nil
		}
		seen[item.Name] = true
		count++
		rx += float64(*crx-*prx) / seconds
		tx += float64(*ctx-*ptx) / seconds
	}
	for name := range old {
		if name != "lo" && !seen[name] {
			return nil, nil
		}
	}
	if count == 0 {
		return nil, nil
	}
	return &rx, &tx
}

// Valid rejects malformed numeric data received from an Agent without affecting
// Docker state. Status freshness is recalculated by the center at read time.
func Valid(m *model.ResourceMetrics) bool {
	if m == nil {
		return true
	}
	if m.Source != "cadvisor" || len(m.ReasonCode) > 64 || m.FetchedAt.IsZero() {
		return false
	}
	if m.Status != "AVAILABLE" && m.Status != "PARTIAL" && m.Status != "UNAVAILABLE" {
		return false
	}
	if m.NetworkScope != "CONTAINER" && m.NetworkScope != "HOST_SHARED" && m.NetworkScope != "UNAVAILABLE" {
		return false
	}
	for _, v := range []*float64{m.CPUUsageCores, m.NetworkReceiveBPS, m.NetworkTransmitBPS, m.SampleWindowSeconds, m.CPUUserUsageCores, m.CPUSystemUsageCores, m.CPUQuotaCores, m.CPUThrottledRatio, m.CPUThrottledSecondsPerSecond, m.MemoryWorkingSetRatio, m.DiskReadBPS, m.DiskWriteBPS, m.DiskReadOpsPerSecond, m.DiskWriteOpsPerSecond, m.NetworkReceivePacketsPerSecond, m.NetworkTransmitPacketsPerSecond, m.NetworkReceiveDroppedPerSecond, m.NetworkTransmitDroppedPerSecond, m.NetworkReceiveErrorsPerSecond, m.NetworkTransmitErrorsPerSecond} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return false
		}
	}
	if m.CPUThrottledRatio != nil && *m.CPUThrottledRatio > 1 {
		return false
	}
	if m.DiskIOStatus != "" && m.DiskIOStatus != "AVAILABLE" && m.DiskIOStatus != "UNAVAILABLE" && m.DiskIOStatus != "UNSUPPORTED" && m.DiskIOStatus != "NO_VALID_SAMPLES" {
		return false
	}
	if len(m.DiskDevices) > 32 {
		return false
	}
	for _, d := range m.DiskDevices {
		if len(d.Device) > 160 {
			return false
		}
		for _, v := range []*float64{d.ReadBPS, d.WriteBPS, d.ReadOpsPerSecond, d.WriteOpsPerSecond} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
				return false
			}
		}
	}
	if m.HostFilesystem != nil {
		f := m.HostFilesystem
		if f.Scope != "HOST_DOCKER_STORAGE" || f.Source != "agent_statfs" || f.CollectedAt.IsZero() || f.CapacityBytes == 0 || f.FreeBytes > f.CapacityBytes || f.AvailableBytes > f.CapacityBytes || math.IsNaN(f.UsedRatio) || math.IsInf(f.UsedRatio, 0) || f.UsedRatio < 0 || f.UsedRatio > 1 {
			return false
		}
		if f.InodesUsedRatio != nil && (math.IsNaN(*f.InodesUsedRatio) || math.IsInf(*f.InodesUsedRatio, 0) || *f.InodesUsedRatio < 0 || *f.InodesUsedRatio > 1) {
			return false
		}
		if f.InodesTotal != nil && f.InodesFree != nil && *f.InodesFree > *f.InodesTotal {
			return false
		}
	}
	if m.NetworkScope == "HOST_SHARED" && (m.NetworkReceiveBPS != nil || m.NetworkTransmitBPS != nil || m.NetworkReceivePacketsPerSecond != nil || m.NetworkTransmitPacketsPerSecond != nil || m.NetworkReceiveDroppedPerSecond != nil || m.NetworkTransmitDroppedPerSecond != nil || m.NetworkReceiveErrorsPerSecond != nil || m.NetworkTransmitErrorsPerSecond != nil) {
		return false
	}
	return true
}
