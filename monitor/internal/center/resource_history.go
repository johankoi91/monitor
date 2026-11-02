package center

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/history"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

func (s *Store) CaptureResourceHistory(ctx context.Context) error {
	s.mu.RLock()
	now := time.Now().UTC()
	samples := []history.Sample{}
	if d := s.record.Baseline.Definition; d != nil {
		for _, g := range d.Clusters {
			for _, service := range g.Services {
				for _, node := range service.Nodes {
					p := history.Sample{NodeID: node.ID, At: now}
					a := s.agents[node.AgentID]
					if fresh(a, now) {
						for _, o := range a.Report.Containers {
							if o.Container.ContainerName != node.ContainerName {
								continue
							}
							p.ContainerID = o.Container.ContainerID
							p.StartedAt = o.StartedAt
							if o.Container.Lifecycle != nil && o.Container.Lifecycle.StartedAt != nil {
								p.StartedAt = o.Container.Lifecycle.StartedAt.UTC().Format(time.RFC3339Nano)
							}
							m := resourceView(o.Container.Resources, a, now)
							if o.Container.RuntimeStatus == "running" && m != nil && !m.Stale && m.Status != "UNAVAILABLE" && m.SampledAt != nil {
								p.At = *m.SampledAt
								p.CPU = m.CPUUsageCores
								p.CPUUser = m.CPUUserUsageCores
								p.CPUSystem = m.CPUSystemUsageCores
								p.Memory = m.MemoryWorkingSetBytes
								p.RSS = m.MemoryRSSBytes
								p.MemoryUsage = m.MemoryUsageBytes
								p.DiskStatus = m.DiskIOStatus
								if m.DiskIOStatus == "AVAILABLE" {
									p.Disks = append([]model.DiskDeviceMetrics(nil), m.DiskDevices...)
									p.DiskRead = m.DiskReadBPS
									p.DiskWrite = m.DiskWriteBPS
									p.DiskReadIOPS = m.DiskReadOpsPerSecond
									p.DiskWriteIOPS = m.DiskWriteOpsPerSecond
								}
								p.NetworkScope = m.NetworkScope
								if m.NetworkScope == "CONTAINER" {
									p.RX = m.NetworkReceiveBPS
									p.TX = m.NetworkTransmitBPS
								}
							}
							break
						}
					}
					samples = append(samples, p)
				}
			}
		}
	}
	s.mu.RUnlock()
	if len(samples) == 0 {
		return nil
	}
	return s.resourceHistory.SaveResources(ctx, samples)
}

func (s *Store) ResourceHistory(ctx context.Context, id, window string) (any, error) {
	s.mu.RLock()
	n, _, _ := s.findNodeLocked(id)
	s.mu.RUnlock()
	if n.ID == "" {
		return nil, problem(404, "NODE_NOT_FOUND", "节点不在当前基准中")
	}
	periods := map[string]struct{ duration, step time.Duration }{"15m": {15 * time.Minute, 30 * time.Second}, "1h": {time.Hour, 30 * time.Second}, "6h": {6 * time.Hour, 2 * time.Minute}, "24h": {24 * time.Hour, 5 * time.Minute}}
	if window == "" {
		window = "1h"
	}
	period, ok := periods[window]
	if !ok {
		return nil, problem(400, "INVALID_RANGE", "时间范围应为15m、1h、6h或24h")
	}
	to := time.Now().UTC()
	from := to.Add(-period.duration)
	samples, err := s.resourceHistory.ResourceSamples(ctx, n.ID, from, to, period.step)
	if err != nil {
		return nil, problem(503, "RESOURCE_HISTORY_UNAVAILABLE", "历史指标暂不可用，请稍后重试")
	}
	return map[string]any{"node_id": n.ID, "from": from, "to": to, "range": window, "step_seconds": int(period.step / time.Second), "retention_days": 7, "samples": samples}, nil
}

func (s *Store) RunResourceHistory(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.CaptureResourceHistory(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Print("resource history sample could not be saved")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
