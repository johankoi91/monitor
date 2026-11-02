package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

const maxEndpoints = 20

var eventCategories = [4]string{ContainerAbnormal, CollectionAbnormal, Recovered, Normal}

// Fixed-size values keep configuration comparisons and revision checks exact.
type Endpoint struct {
	ID            string
	Enabled       bool
	URL           string
	TLSServerName string
	Secret        string
	Events        [4]bool
	NormalEpoch   string
}
type EndpointSettings struct {
	ID             string   `json:"id"`
	Enabled        bool     `json:"enabled"`
	URL            string   `json:"url"`
	TLSServerName  string   `json:"tls_server_name"`
	Events         []string `json:"events"`
	AuthConfigured bool     `json:"auth_configured"`
}
type EndpointRequest struct {
	ID            string   `json:"id"`
	Enabled       bool     `json:"enabled"`
	URL           string   `json:"url"`
	TLSServerName string   `json:"tls_server_name"`
	Events        []string `json:"events"`
	AuthMode      string   `json:"auth_mode"`
	Secret        string   `json:"secret,omitempty"`
}

func endpoints(c Config) []Endpoint {
	result := []Endpoint{}
	if c.EndpointMode {
		for _, e := range c.Endpoints {
			if e.ID != "" {
				result = append(result, e)
			}
		}
		return result
	}
	// Expose existing routes as editable configurations without changing their
	// signing secret or persisting a migration during a read.
	if c.URL == "" {
		return result
	}
	for i, category := range eventCategories[:3] {
		target, selected := routeConfig(c, category)
		if !selected {
			continue
		}
		index := -1
		for j, e := range result {
			if e.URL == target.URL && e.TLSServerName == target.TLSServerName {
				index = j
				break
			}
		}
		if index < 0 {
			index = len(result)
			result = append(result, Endpoint{ID: "legacy-" + category, Enabled: c.Enabled, URL: target.URL, TLSServerName: target.TLSServerName, Secret: target.Secret})
		}
		result[index].Events[i] = true
	}
	return result
}
func endpointViews(c Config) []EndpointSettings {
	result := []EndpointSettings{}
	for _, e := range endpoints(c) {
		events := []string{}
		for i, on := range e.Events {
			if on {
				events = append(events, eventCategories[i])
			}
		}
		result = append(result, EndpointSettings{ID: e.ID, Enabled: e.Enabled, URL: e.URL, TLSServerName: e.TLSServerName, Events: events, AuthConfigured: e.Secret != ""})
	}
	return result
}
func endpointConfig(c Config, e Endpoint) Config {
	c.EndpointMode = false
	c.URL = e.URL
	c.TLSServerName = e.TLSServerName
	c.Secret = e.Secret
	return c
}

type destination struct {
	ID          string
	Config      Config
	NormalEpoch string
}

func destinations(c Config, category string) []destination {
	result := []destination{}
	if !c.EndpointMode {
		target, on := routeConfig(c, category)
		if on {
			result = append(result, destination{Config: target})
		}
		return result
	}
	for _, e := range endpoints(c) {
		if !e.Enabled {
			continue
		}
		for i, v := range eventCategories {
			if (v == category || category == Recovered && v == Normal) && e.Events[i] {
				result = append(result, destination{ID: e.ID, Config: endpointConfig(c, e), NormalEpoch: e.NormalEpoch})
				break
			}
		}
	}
	return result
}
func deliveryKey(d Delivery) string {
	if d.EndpointID == "" {
		return d.Event.ID
	}
	return d.Event.ID + "/" + d.EndpointID
}
func deliveryConfig(c Config, d Delivery) (Config, bool) {
	if !c.EndpointMode {
		return routeConfig(c, d.Event.Category)
	}
	for _, e := range endpoints(c) {
		if e.ID == d.EndpointID && e.Enabled {
			if d.Event.Type == "WEBHOOK_TEST" {
				return endpointConfig(c, e), true
			}
			for i, v := range eventCategories {
				if (v == d.Event.Category || d.Event.Category == Recovered && v == Normal) && e.Events[i] {
					return endpointConfig(c, e), true
				}
			}
		}
	}
	return c, false
}

func validateEndpoints(c Config) error {
	if !c.EndpointMode {
		return nil
	}
	ids, urls := map[string]bool{}, map[string]bool{}
	for _, e := range endpoints(c) {
		if ids[e.ID] || urls[e.URL+"|"+e.TLSServerName] {
			return errors.New("接收地址不能重复配置")
		}
		ids[e.ID] = true
		urls[e.URL+"|"+e.TLSServerName] = true
		if e.URL == "" || e.Secret == "" || len(e.Secret) > 4096 || e.Events == [4]bool{} {
			return errors.New("请填写接收地址、签名密钥并至少选择一种事件")
		}
		if err := validSettings(endpointConfig(c, e)); err != nil {
			return err
		}
	}
	return nil
}

// Caller holds n.mu; probing releases it and checks the revision before saving.
func (n *Notifier) updateEndpointsLocked(r SettingsRequest, forbidden []string) (Settings, error) {
	if len(*r.Endpoints) > maxEndpoints {
		return Settings{}, errors.New("最多配置20个接收地址")
	}
	previous := map[string]Endpoint{}
	for _, e := range endpoints(n.config) {
		previous[e.ID] = e
	}
	c := n.config
	c.EndpointMode = true
	c.Endpoints = [maxEndpoints]Endpoint{}
	c.Enabled = false
	targets := []Config{}
	for i, input := range *r.Endpoints {
		old, exists := previous[input.ID]
		if input.ID != "" && !exists {
			return Settings{}, errors.New("通知配置已变化，请重新读取")
		}
		e := Endpoint{ID: input.ID, Enabled: input.Enabled, URL: strings.TrimSpace(input.URL), TLSServerName: strings.TrimSpace(input.TLSServerName)}
		if e.ID == "" {
			e.ID = eventID()
		}
		for _, v := range input.Events {
			found := false
			for j, category := range eventCategories {
				if v == category {
					if e.Events[j] {
						return Settings{}, errors.New("事件类型重复")
					}
					e.Events[j] = true
					found = true
					break
				}
			}
			if !found {
				return Settings{}, errors.New("事件类型不合法")
			}
		}
		switch input.AuthMode {
		case "retain":
			if !exists || old.Secret == "" || old.URL != e.URL || old.TLSServerName != e.TLSServerName || input.Secret != "" {
				return Settings{}, errors.New("新增或更换接收地址时请填写签名密钥")
			}
			e.Secret = old.Secret
		case "set":
			if input.Secret == "" || len(input.Secret) > 4096 {
				return Settings{}, errors.New("请填写签名密钥")
			}
			for _, v := range forbidden {
				if v != "" && input.Secret == v {
					return Settings{}, errors.New("通知接收端必须使用独立凭证")
				}
			}
			e.Secret = input.Secret
		default:
			return Settings{}, errors.New("请选择保留或更新签名密钥")
		}
		e.NormalEpoch = old.NormalEpoch
		if e.Enabled && e.Events[3] && (!exists || !old.Enabled || !old.Events[3] || e.URL != old.URL || e.TLSServerName != old.TLSServerName || e.Secret != old.Secret || e.NormalEpoch == "") {
			e.NormalEpoch = eventID()
		}
		c.Endpoints[i] = e
		c.Enabled = c.Enabled || e.Enabled
		if e.Enabled && (!exists || !old.Enabled || old.URL != e.URL || old.TLSServerName != e.TLSServerName || old.Secret != e.Secret) {
			targets = append(targets, endpointConfig(c, e))
		}
	}
	if err := validateEndpoints(c); err != nil {
		return Settings{}, err
	}
	if c == n.config {
		return n.settingsLocked(), nil
	}
	if len(targets) > 0 {
		now := time.Now().UTC()
		body, _ := json.Marshal(model.Notification{ID: eventID(), Type: "WEBHOOK_TEST", Product: "RTC", Cluster: "webhook-check", Service: "webhook-test", PreviousStatus: "UNKNOWN", CurrentStatus: "UNKNOWN", ReasonCode: "MONITOR_WEBHOOK_TEST", Reason: "通知接收配置验证", OccurredAt: now, CollectedAt: &now})
		revision, factory := n.settingsRevision, n.newClient
		n.mu.Unlock()
		reason := probeTargets(targets, factory, body)
		n.mu.Lock()
		if revision != n.settingsRevision {
			return Settings{}, ErrSettingsConflict
		}
		if n.failed {
			return Settings{}, ErrSettingsWrite
		}
		if reason != "" {
			return Settings{}, errors.New("接收地址验证失败：" + reason + "；接收端需返回 HTTP 200 和 JSON")
		}
	}
	revision := eventID()
	if err := n.writeSettings(settingsRecord{Version: 1, Revision: revision, Config: c}); err != nil {
		return Settings{}, ErrSettingsWrite
	}
	if n.inflightCancel != nil {
		n.inflightCancel()
		n.inflightCancel = nil
	}
	n.client.CloseIdleConnections()
	n.config = c
	n.settingsRevision = revision
	n.client = n.newClient(c)
	n.cancelOldDeliveries()
	n.refreshNormalQueued()
	return n.settingsLocked(), nil
}

func (n *Notifier) probeEndpoint(ctx context.Context, cluster string, e Endpoint, revision string) (map[string]any, error) {
	n.mu.Lock()
	if n.failed || revision != n.settingsRevision {
		n.mu.Unlock()
		return nil, ErrSettingsConflict
	}
	cfg := endpointConfig(n.config, e)
	client := n.newClient(cfg)
	defer client.CloseIdleConnections()
	now := time.Now().UTC()
	d := Delivery{EndpointID: e.ID, Event: model.Notification{ID: eventID(), Type: "WEBHOOK_TEST", Product: "RTC", Cluster: cluster, Service: "webhook-test", PreviousStatus: "UNKNOWN", CurrentStatus: "UNKNOWN", ReasonCode: "MONITOR_WEBHOOK_TEST", Reason: "通知接收配置验证", OccurredAt: now, CollectedAt: &now}, SettingsRevision: revision, Attempts: 1, Status: "INFLIGHT", NextAt: now.Add(11 * time.Second)}
	if !n.persist(record{Deliveries: []Delivery{d}}) {
		n.mu.Unlock()
		return nil, ErrSettingsWrite
	}
	n.mu.Unlock()
	body, _ := json.Marshal(d.Event)
	ok, status, reason := postWebhook(ctx, client, cfg, body)
	n.mu.Lock()
	defer n.mu.Unlock()
	if revision != n.settingsRevision {
		return nil, ErrSettingsConflict
	}
	d.Status = "FAILED"
	d.LastError = reason
	if ok {
		d.Status = "DELIVERED"
		d.LastError = ""
	}
	if !n.persist(record{Deliveries: []Delivery{d}}) {
		return nil, ErrSettingsWrite
	}
	return map[string]any{"healthy": ok, "http_status": status, "reason": d.LastError, "event_id": d.Event.ID}, nil
}

func (n *Notifier) ProbeEndpoint(ctx context.Context, cluster, id string) (map[string]any, error) {
	n.mu.Lock()
	c, revision := n.config, n.settingsRevision
	n.mu.Unlock()
	if !c.EndpointMode && id == "" {
		return n.Probe(ctx, cluster)
	}
	selected := []Endpoint{}
	for _, e := range endpoints(c) {
		if id != "" && e.ID == id || id == "" && e.Enabled {
			selected = append(selected, e)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("没有可测试的通知配置")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		value map[string]any
		err   error
	}
	results := make(chan result, len(selected))
	for _, e := range selected {
		go func(e Endpoint) { v, err := n.probeEndpoint(ctx, cluster, e, revision); results <- result{v, err} }(e)
	}
	var value map[string]any
	var firstErr error
	var failed map[string]any
	for range selected {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		value = r.value
		if ok, _ := r.value["healthy"].(bool); !ok {
			failed = r.value
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if failed != nil {
		return failed, nil
	}
	return value, nil
}
