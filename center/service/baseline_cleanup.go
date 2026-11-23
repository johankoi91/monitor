package center

import "github.com/johankoi91/monitor/runtime/shared/model"

func pruneEmptyGroups(d *model.Definition) bool {
	changed := false
	clusters := make([]model.Cluster, 0, len(d.Clusters))
	for _, cluster := range d.Clusters {
		services := make([]model.Service, 0, len(cluster.Services))
		for _, service := range cluster.Services {
			if len(service.Nodes) == 0 {
				changed = true
				continue
			}
			services = append(services, service)
		}
		if len(services) == 0 {
			changed = true
			continue
		}
		cluster.Services = services
		clusters = append(clusters, cluster)
	}
	d.Clusters = clusters
	return changed
}

// CleanupEmptyGroups creates a new current version without changing any member.
func (s *Store) CleanupEmptyGroups(expected, source string) (model.Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageBad {
		return model.Baseline{}, problem(503, "STORAGE_UNAVAILABLE", "存储不可用")
	}
	if expected != s.record.Baseline.Revision {
		return model.Baseline{}, problem(409, "BASELINE_CONFLICT", "基准已更新，请刷新后重试")
	}
	if s.record.Baseline.Definition == nil {
		return clone(s.record.Baseline), nil
	}
	definition := clone(*s.record.Baseline.Definition)
	if !pruneEmptyGroups(&definition) {
		return clone(s.record.Baseline), nil
	}
	return s.saveDefinitionLocked(definition, source, model.Selection{ExpectedRevision: expected, Additions: []model.Addition{}, Removals: []string{}})
}
