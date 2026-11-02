// Package history stores bounded numeric resource observations. It never stores
// Docker startup parameters or turns missing counters into measured zeroes.
package history

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

const Retention = 7 * 24 * time.Hour
const Interval = 30 * time.Second

type Sample struct {
	NodeID        string                    `json:"-"`
	At            time.Time                 `json:"at"`
	ContainerID   string                    `json:"container_id"`
	StartedAt     string                    `json:"started_at"`
	CPU           *float64                  `json:"cpu"`
	CPUUser       *float64                  `json:"cpu_user"`
	CPUSystem     *float64                  `json:"cpu_system"`
	Memory        *uint64                   `json:"memory"`
	RSS           *uint64                   `json:"rss"`
	MemoryUsage   *uint64                   `json:"memory_usage"`
	DiskStatus    string                    `json:"disk_status"`
	Disks         []model.DiskDeviceMetrics `json:"disks"`
	DiskRead      *float64                  `json:"disk_read"`
	DiskWrite     *float64                  `json:"disk_write"`
	DiskReadIOPS  *float64                  `json:"disk_read_iops"`
	DiskWriteIOPS *float64                  `json:"disk_write_iops"`
	NetworkScope  string                    `json:"network_scope"`
	RX            *float64                  `json:"rx"`
	TX            *float64                  `json:"tx"`
}

type Backend interface {
	SaveResources(context.Context, []Sample) error
	ResourceSamples(context.Context, string, time.Time, time.Time, time.Duration) ([]Sample, error)
}

// Select the latest observation per display interval and container incarnation.
// Values stay real observations; we do not average overlapping disk devices.
func Downsample(samples []Sample, step time.Duration) []Sample {
	type key struct {
		bucket             int64
		container, started string
	}
	buckets := map[key]Sample{}
	for _, s := range samples {
		k := key{s.At.Truncate(step).Unix(), s.ContainerID, s.StartedAt}
		if p, ok := buckets[k]; !ok || s.At.After(p.At) {
			buckets[k] = s
		}
	}
	result := make([]Sample, 0, len(buckets))
	for _, s := range buckets {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].At.Before(result[j].At) })
	return result
}

// Memory is only the isolated file-dev/browser-fixture fallback. Production
// uses PostgreSQL so closing a page or restarting the center retains history.
type Memory struct {
	mu      sync.Mutex
	samples []Sample
}

func NewMemory() *Memory { return &Memory{} }
func (m *Memory) SaveResources(ctx context.Context, samples []Sample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-Retention)
	kept := m.samples[:0]
	for _, s := range m.samples {
		if !s.At.Before(cutoff) {
			kept = append(kept, s)
		}
	}
	m.samples = kept
	for _, s := range samples {
		found := false
		for i := len(m.samples) - 1; i >= 0; i-- {
			p := m.samples[i]
			if p.NodeID == s.NodeID && p.At.Truncate(Interval).Equal(s.At.Truncate(Interval)) && p.ContainerID == s.ContainerID && p.StartedAt == s.StartedAt {
				if s.At.After(p.At) {
					m.samples[i] = s
				}
				found = true
				break
			}
		}
		if !found {
			m.samples = append(m.samples, s)
		}
	}
	if len(m.samples) > 50000 {
		sort.Slice(m.samples, func(i, j int) bool { return m.samples[i].At.Before(m.samples[j].At) })
		m.samples = append([]Sample(nil), m.samples[len(m.samples)-50000:]...)
	}
	return nil
}
func (m *Memory) ResourceSamples(ctx context.Context, id string, from, to time.Time, step time.Duration) ([]Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Sample{}
	for _, s := range m.samples {
		if s.NodeID == id && !s.At.Before(from) && !s.At.After(to) {
			result = append(result, s)
		}
	}
	return Downsample(result, step), nil
}
