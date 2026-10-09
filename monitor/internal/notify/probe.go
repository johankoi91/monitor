package notify

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

// Probe sends a clearly identified operational test, never an RTC channel
// event or a change in observed service health.
func (n *Notifier) Probe(ctx context.Context, cluster string) (map[string]any, error) {
	n.mu.Lock()
	if n.failed || n.config.URL == "" || n.config.Secret == "" {
		n.mu.Unlock()
		return nil, errors.New("请先保存 HTTPS 地址和 Webhook 签名密钥")
	}
	cfg, client, revision := n.config, n.client, n.settingsRevision
	now := time.Now().UTC()
	d := Delivery{Event: model.Notification{ID: eventID(), Type: "WEBHOOK_TEST", Product: "RTC", Cluster: cluster, Service: "webhook-test", PreviousStatus: "UNKNOWN", CurrentStatus: "UNKNOWN", ReasonCode: "MONITOR_WEBHOOK_TEST", Reason: "仅验证 Webhook 接收、签名和响应，不代表业务故障或恢复", OccurredAt: now, CollectedAt: &now}, SettingsRevision: revision, Attempts: 1, Status: "INFLIGHT", NextAt: now.Add(11 * time.Second)}
	if !n.persist(record{Deliveries: []Delivery{d}}) {
		n.mu.Unlock()
		return nil, errors.New("测试记录无法保存")
	}
	n.mu.Unlock()
	data, _ := json.Marshal(d.Event)
	success, status, reason := postWebhook(ctx, client, cfg, data)
	n.mu.Lock()
	defer n.mu.Unlock()
	if revision != n.settingsRevision {
		return nil, errors.New("测试期间配置已变化，请重新测试")
	}
	d.Status = "FAILED"
	d.LastError = reason
	if success {
		d.Status = "DELIVERED"
		d.LastError = ""
	}
	if !n.persist(record{Deliveries: []Delivery{d}}) {
		return nil, errors.New("测试结果无法保存")
	}
	return map[string]any{"healthy": success, "http_status": status, "reason": d.LastError, "event_id": d.Event.ID}, nil
}
