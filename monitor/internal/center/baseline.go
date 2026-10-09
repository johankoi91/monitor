package center

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
	"gopkg.in/yaml.v3"
)

var udpEdge = regexp.MustCompile(`^agora_udp_media_edge_[0-9]+$`)
var webEdge = regexp.MustCompile(`^agora_web_media_edge_[0-9]+$`)
var autEdge = regexp.MustCompile(`^agora_aut_media_edge_[0-9]+$`)

func automaticService(name string) (string, string) {
	switch {
	case name == "agora_local_ap":
		return "rtc-ap", "RTC AP"
	case name == "agora_local_balancer":
		return "rtc-balancer", "RTC Balancer"
	case udpEdge.MatchString(name):
		return "rtc-native-edge-udp", "RTC Native Edge UDP"
	case webEdge.MatchString(name):
		return "rtc-web-edge", "RTC Web Edge"
	case autEdge.MatchString(name):
		return "rtc-native-edge-aut", "RTC Native Edge AUT"
	}
	hash := sha256.Sum256([]byte(name))
	label := name
	if len(label) > 128 {
		label = label[:125] + "..."
	}
	return "docker-" + hex.EncodeToString(hash[:8]), label
}
func (s *Store) recordClusters() []model.Cluster {
	if s.record.Baseline.Definition == nil {
		return nil
	}
	return s.record.Baseline.Definition.Clusters
}
func validateChecks(checks []model.Check, address string) error {
	if len(checks) > 8 {
		return problem(400, "INVALID_CHECK", "每个容器最多配置 8 个 TCP 端口")
	}
	seen := map[string]bool{}
	for _, check := range checks {
		if check.Type != "tcp" || check.Port < 1 || check.Port > 65535 || check.TimeoutMS < 1 || check.TimeoutMS > 5000 || (check.Host != "127.0.0.1" && check.Host != "::1" && check.Host != "localhost" && check.Host != address) {
			return problem(400, "INVALID_CHECK", "TCP 检查应为本机端口，端口 1–65535，超时 1–5000ms")
		}
		host := check.Host
		if host == "localhost" {
			host = "127.0.0.1"
		}
		key := fmt.Sprintf("%s:%d", host, check.Port)
		if seen[key] {
			return problem(400, "DUPLICATE_CHECK", "TCP 端口不能重复")
		}
		seen[key] = true
	}
	return nil
}
func (s *Store) UpdateChecks(revision, nodeID, source string, checks []model.Check) (model.Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageBad {
		return model.Baseline{}, problem(503, "STORAGE_UNAVAILABLE", "存储不可用")
	}
	if revision != s.record.Baseline.Revision {
		return model.Baseline{}, problem(409, "BASELINE_CONFLICT", "基准已变化，请刷新后重新确认")
	}
	if checks == nil {
		return model.Baseline{}, problem(400, "INVALID_CHECK", "必须提交端口清单，空数组表示不配置 TCP")
	}
	if s.busyOperationLocked(nodeID) != "" {
		return model.Baseline{}, problem(409, "OPERATION_IN_PROGRESS", "目标正在重启或结果未知，不能修改检查")
	}
	n, _, _ := s.findNodeLocked(nodeID)
	if n.ID == "" {
		return model.Baseline{}, problem(404, "NODE_NOT_FOUND", "节点不在基准中")
	}
	if err := validateChecks(checks, n.HostAddress); err != nil {
		return model.Baseline{}, err
	}
	record := clone(s.record)
	for ci := range record.Baseline.Definition.Clusters {
		for si := range record.Baseline.Definition.Clusters[ci].Services {
			for ni := range record.Baseline.Definition.Clusters[ci].Services[si].Nodes {
				node := &record.Baseline.Definition.Clusters[ci].Services[si].Nodes[ni]
				if node.ID == nodeID {
					node.Checks = clone(checks)
				}
			}
		}
	}
	at := time.Now().UTC()
	record.Baseline.Revision = ID()
	record.Baseline.SavedAt = &at
	url := "/api/v1/baseline/yaml?revision=" + record.Baseline.Revision
	record.Baseline.YAMLURL = &url
	record.Source = source + ":checks:" + nodeID
	record.Change = model.Selection{ExpectedRevision: revision, Additions: []model.Addition{}, Removals: []string{}}
	data, err := yaml.Marshal(record.Baseline.Definition)
	if err != nil {
		return model.Baseline{}, err
	}
	record.YAML = string(data)
	encoded, err := json.Marshal(record)
	if err != nil {
		return model.Baseline{}, err
	}
	if err = s.write(encoded); err != nil {
		return model.Baseline{}, problem(503, "BASELINE_WRITE_FAILED", "端口保存失败，当前基准未改变")
	}
	s.record = record
	delete(s.checks, nodeID)
	return clone(record.Baseline), nil
}

func (s *Store) UpdateRestartEnabled(revision, nodeID, source string, enabled bool) (model.Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageBad {
		return model.Baseline{}, problem(503, "STORAGE_UNAVAILABLE", "存储不可用")
	}
	if revision != s.record.Baseline.Revision {
		return model.Baseline{}, problem(409, "BASELINE_CONFLICT", "基准已变化，请刷新后重新确认")
	}
	if s.busyOperationLocked(nodeID) != "" {
		return model.Baseline{}, problem(409, "OPERATION_IN_PROGRESS", "目标正在重启或结果未知，不能修改重启权限")
	}
	n, _, _ := s.findNodeLocked(nodeID)
	if n.ID == "" {
		return model.Baseline{}, problem(404, "NODE_NOT_FOUND", "节点不在基准中")
	}
	record := clone(s.record)
	for ci := range record.Baseline.Definition.Clusters {
		for si := range record.Baseline.Definition.Clusters[ci].Services {
			for ni := range record.Baseline.Definition.Clusters[ci].Services[si].Nodes {
				node := &record.Baseline.Definition.Clusters[ci].Services[si].Nodes[ni]
				if node.ID == nodeID {
					node.RestartEnabled = enabled
				}
			}
		}
	}
	at := time.Now().UTC()
	record.Baseline.Revision = ID()
	record.Baseline.SavedAt = &at
	url := "/api/v1/baseline/yaml?revision=" + record.Baseline.Revision
	record.Baseline.YAMLURL = &url
	record.Source = source + ":restart-policy:" + nodeID
	record.Change = model.Selection{ExpectedRevision: revision, Additions: []model.Addition{}, Removals: []string{}}
	data, err := yaml.Marshal(record.Baseline.Definition)
	if err != nil {
		return model.Baseline{}, err
	}
	record.YAML = string(data)
	encoded, err := json.Marshal(record)
	if err != nil {
		return model.Baseline{}, err
	}
	if err = s.write(encoded); err != nil {
		return model.Baseline{}, problem(503, "BASELINE_WRITE_FAILED", "重启权限保存失败，当前基准未改变")
	}
	s.record = record
	delete(s.checks, nodeID)
	return clone(record.Baseline), nil
}
