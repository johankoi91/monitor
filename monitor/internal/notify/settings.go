package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Settings struct {
	AuthScheme       string `json:"auth_scheme"`
	ResponseSeconds  int    `json:"response_seconds"`
	MaxRetries       int    `json:"max_retries"`
	Revision         string `json:"revision"`
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	TLSServerName    string `json:"tls_server_name"`
	AuthConfigured   bool   `json:"auth_configured"`
	StorageAvailable bool   `json:"storage_available"`
}
type SettingsRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	TLSServerName    string `json:"tls_server_name"`
	AuthMode         string `json:"auth_mode"`
	ID               string `json:"id,omitempty"`
	Secret           string `json:"secret,omitempty"`
}
type settingsRecord struct {
	Version  int    `json:"version"`
	Revision string `json:"revision"`
	Config   Config `json:"config"`
}

var ErrSettingsConflict = errors.New("notification settings revision conflict")
var ErrSettingsWrite = errors.New("notification settings write failed")

func loadSettings(path string) (settingsRecord, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settingsRecord{}, false, nil
	}
	if err != nil {
		return settingsRecord{}, false, err
	}
	if len(data) > 64<<10 {
		return settingsRecord{}, false, errors.New("oversized notification settings")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var record settingsRecord
	if dec.Decode(&record) != nil || dec.Decode(&struct{}{}) != io.EOF || record.Version != 1 || record.Revision == "" {
		return record, false, errors.New("corrupt notification settings; retained for repair")
	}
	return record, true, nil
}
func validSettings(c Config) error {
	if len(c.URL) > 2048 || len(c.TLSServerName) > 253 {
		return errors.New("通知地址或 TLS 域名过长")
	}
	if c.URL == "" {
		if c.Enabled {
			return errors.New("启用通知前请填写 HTTPS 地址")
		}
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || u.Fragment != "" || strings.ContainsAny(c.URL, "\r\n\t ") {
		return errors.New("Webhook 只支持 HTTPS 地址，不在 URL 中填写账号或密钥")
	}
	if strings.ContainsAny(c.TLSServerName, "/:@ \t\r\n") {
		return errors.New("TLS 校验域名格式不正确")
	}
	return nil
}
func (n *Notifier) settingsLocked() Settings {
	return Settings{AuthScheme: "webhook_hmac", ResponseSeconds: 10, MaxRetries: 3, Revision: n.settingsRevision, Enabled: n.config.Enabled, URL: n.config.URL, TLSServerName: n.config.TLSServerName, AuthConfigured: n.config.Secret != "", StorageAvailable: !n.failed}
}
func (n *Notifier) Settings() Settings { n.mu.Lock(); defer n.mu.Unlock(); return n.settingsLocked() }
func (n *Notifier) UpdateSettings(r SettingsRequest, forbidden []string) (Settings, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.ExpectedRevision != n.settingsRevision {
		return Settings{}, ErrSettingsConflict
	}
	if r.ID != "" {
		return Settings{}, errors.New("Webhook 不使用接收端 ID，请只配置签名密钥")
	}
	if n.failed {
		return Settings{}, ErrSettingsWrite
	}
	c := n.config
	c.Enabled = r.Enabled
	c.URL = strings.TrimSpace(r.URL)
	c.TLSServerName = strings.TrimSpace(r.TLSServerName)
	c.ID = ""
	c.AuthScheme = "webhook_hmac"
	if err := validSettings(c); err != nil {
		return Settings{}, err
	}
	switch r.AuthMode {
	case "retain":
		if c.URL != n.config.URL || c.TLSServerName != n.config.TLSServerName {
			return Settings{}, errors.New("更换通知目标时不能沿用原接收端密钥")
		}
		if r.ID != "" || r.Secret != "" {
			return Settings{}, errors.New("保留鉴权模式不能同时提交新凭证")
		}
	case "clear":
		c.ID = ""
		c.Secret = ""
	case "set":
		if r.Secret == "" || len(r.Secret) > 4096 {
			return Settings{}, errors.New("请填写 Webhook 签名密钥")
		}
		for _, value := range forbidden {
			if value != "" && value == r.Secret {
				return Settings{}, errors.New("通知接收端必须使用独立凭证")
			}
		}
		c.Secret = r.Secret
	default:
		return Settings{}, errors.New("请选择保留、清除或更新接收端鉴权")
	}
	if c.Enabled && c.Secret == "" {
		return Settings{}, errors.New("启用 Webhook 前必须配置签名密钥")
	}
	if c == n.config {
		return n.settingsLocked(), nil
	}
	// A new or changed enabled endpoint must acknowledge the signed health
	// check before activation, as required by the referenced Webhook design.
	if c.Enabled && (!n.config.Enabled || c.URL != n.config.URL || c.TLSServerName != n.config.TLSServerName || c.Secret != n.config.Secret) {
		now := time.Now().UTC()
		probe := model.Notification{ID: eventID(), Type: "WEBHOOK_TEST", Product: "RTC", Cluster: "webhook-check", Service: "webhook-test", PreviousStatus: "UNKNOWN", CurrentStatus: "UNKNOWN", ReasonCode: "MONITOR_WEBHOOK_TEST", Reason: "签名和响应健康检查，不代表业务状态", OccurredAt: now, CollectedAt: &now}
		body, _ := json.Marshal(probe)
		client := n.newClient(c)
		// Network validation must not block collection, polling or delivery.
		revisionBeforeProbe := n.settingsRevision
		n.mu.Unlock()
		ok, _, reason := postWebhook(context.Background(), client, c, body)
		client.CloseIdleConnections()
		n.mu.Lock()
		if revisionBeforeProbe != n.settingsRevision {
			return Settings{}, ErrSettingsConflict
		}
		if n.failed {
			return Settings{}, ErrSettingsWrite
		}
		if !ok {
			return Settings{}, errors.New("Webhook 健康检查未通过：" + reason + "；接收端须在 10 秒内返回 HTTP 200 和 JSON")
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
	n.config = c
	n.settingsRevision = revision
	n.client = n.newClient(c)
	n.cancelOldDeliveries()
	return n.settingsLocked(), nil
}
func (n *Notifier) cancelOldDeliveries() {
	batch := record{}
	for _, d := range n.deliveries {
		current := d.SettingsRevision == n.settingsRevision || d.SettingsRevision == "" && n.settingsRevision == "deployment"
		if (d.Status == "PENDING" || d.Status == "INFLIGHT") && !current {
			d.Status = "FAILED"
			d.LastError = "CONFIGURATION_CHANGED"
			batch.Deliveries = append(batch.Deliveries, d)
			if len(batch.Deliveries) >= 32 {
				if !n.persist(batch) {
					return
				}
				batch = record{}
			}
		}
	}
	if len(batch.Deliveries) > 0 {
		n.persist(batch)
	}
}
func (n *Notifier) saveSettings(record settingsRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	old, readErr := os.ReadFile(n.settingsPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	write := func(body []byte) error {
		f, err := os.CreateTemp(filepath.Dir(n.settingsPath), ".notification-settings-*")
		if err != nil {
			return err
		}
		name := f.Name()
		defer os.Remove(name)
		if _, err = f.Write(body); err == nil {
			err = f.Sync()
		}
		if e := f.Close(); err == nil {
			err = e
		}
		if err != nil {
			return err
		}
		if err = os.Rename(name, n.settingsPath); err != nil {
			return err
		}
		dir, err := os.Open(filepath.Dir(n.settingsPath))
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
	if err = write(data); err != nil {
		current, _ := os.ReadFile(n.settingsPath)
		if bytes.Equal(current, data) {
			var rollback error
			if os.IsNotExist(readErr) {
				rollback = os.Remove(n.settingsPath)
			} else {
				rollback = write(old)
			}
			if rollback != nil {
				n.failed = true
			}
		}
		return err
	}
	return nil
}
