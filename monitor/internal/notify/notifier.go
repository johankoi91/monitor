package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/journal"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/storage"
)

type Config struct {
	EndpointMode     bool                   `yaml:"endpoint_mode"`
	Endpoints        [maxEndpoints]Endpoint `yaml:"endpoints"`
	Routes           [3]Route               `yaml:"routes"`
	RoutesConfigured bool                   `yaml:"routes_configured"`
	AuthScheme       string                 `yaml:"auth_scheme"`
	Enabled          bool                   `yaml:"enabled"`
	URL              string                 `yaml:"url"`
	TLSServerName    string                 `yaml:"tls_server_name"`
	ID               string                 `yaml:"id"`
	Secret           string                 `yaml:"secret"`
	TimeoutSeconds   int                    `yaml:"timeout_seconds"`
	MaxAttempts      int                    `yaml:"max_attempts"`
	MaxPending       int                    `yaml:"max_pending"`
}
type Fact struct {
	Key         string     `json:"key"`
	Cluster     string     `json:"cluster"`
	Service     string     `json:"service"`
	Node        string     `json:"node,omitempty"`
	Status      string     `json:"status"`
	ReasonCode  string     `json:"reason_code"`
	Reason      string     `json:"reason"`
	CollectedAt *time.Time `json:"collected_at"`
	Stale       bool       `json:"stale"`
}
type Delivery struct {
	NormalEpoch      string             `json:"normal_epoch,omitempty"`
	EndpointID       string             `json:"endpoint_id,omitempty"`
	Event            model.Notification `json:"event"`
	Status           string             `json:"status"`
	SettingsRevision string             `json:"settings_revision,omitempty"`
	Attempts         int                `json:"attempts"`
	NextAt           time.Time          `json:"next_at"`
	LastError        string             `json:"last_error"`
}
type record struct {
	Changes     []Fact     `json:"changes,omitempty"`
	Removed     []string   `json:"removed,omitempty"`
	Deliveries  []Delivery `json:"deliveries,omitempty"`
	Revision    string     `json:"revision,omitempty"`
	RevisionSet bool       `json:"revision_set,omitempty"`
	Dropped     int        `json:"dropped,omitempty"`
}
type Notifier struct {
	normalQueued     map[string]bool
	mu               sync.Mutex
	config           Config
	client           *http.Client
	journal          storage.Log
	facts            map[string]Fact
	deliveries       map[string]Delivery
	revision         string
	dropped          int
	failed           bool
	settingsPath     string
	settingsRevision string
	inflightCancel   context.CancelFunc
	writeSettings    func(settingsRecord) error
	newClient        func(Config) *http.Client
}

func New(dir string, c Config, backends ...storage.Backend) (*Notifier, error) {
	revision := "deployment"
	override, found, err := loadSettings(filepath.Join(dir, "notification-settings.json"))
	var backend storage.Backend
	if len(backends) > 0 {
		backend = backends[0]
	}
	if backend != nil {
		var b []byte
		b, found, err = backend.LoadDocument("notification_settings")
		if err == nil && found {
			err = json.Unmarshal(b, &override)
		}
	}
	if err != nil {
		return nil, err
	}
	if found {
		c = override.Config
		revision = override.Revision
	}
	// The receiving contract is 10 seconds and three retries (four attempts).
	c.TimeoutSeconds = 10
	c.MaxAttempts = 4
	if c.MaxPending == 0 {
		c.MaxPending = 1000
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 30 || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.MaxPending < 1 || c.MaxPending > 1000 {
		return nil, errors.New("invalid notification timeout/retry/queue budget")
	}
	legacy := c.AuthScheme != "webhook_hmac"
	c.AuthScheme = "webhook_hmac"
	c.ID = "" // A Webhook uses a signing secret, not a Basic username.
	if !c.RoutesConfigured {
		c.Routes = defaultRoutes()
		c.RoutesConfigured = true
	}
	if err := validateRoutes(c); err != nil {
		return nil, err
	}
	if err := validSettings(c); err != nil {
		return nil, err
	}
	if c.Enabled && c.Secret == "" && !c.EndpointMode {
		return nil, errors.New("Webhook signing secret required")
	}
	if found && legacy {
		revision = eventID()
		b, e := json.Marshal(settingsRecord{Version: 1, Revision: revision, Config: c})
		if e != nil {
			return nil, e
		}
		if backend != nil {
			e = backend.SaveDocument("notification_settings", b)
		} else {
			writer := &Notifier{settingsPath: filepath.Join(dir, "notification-settings.json")}
			e = writer.saveSettings(settingsRecord{Version: 1, Revision: revision, Config: c})
		}
		if e != nil {
			return nil, e
		}
	}
	n := &Notifier{config: c, facts: map[string]Fact{}, deliveries: map[string]Delivery{}, normalQueued: map[string]bool{}, client: webhookClient(c)}
	n.newClient = webhookClient
	n.settingsPath = filepath.Join(dir, "notification-settings.json")
	n.settingsRevision = revision
	if backend != nil && !found {
		b, e := json.Marshal(settingsRecord{Version: 1, Revision: revision, Config: c})
		if e != nil {
			return nil, e
		}
		if e = backend.SaveDocument("notification_settings", b); e != nil {
			return nil, e
		}
	}
	n.writeSettings = n.saveSettings
	if backend != nil {
		n.writeSettings = func(r settingsRecord) error {
			b, e := json.Marshal(r)
			if e != nil {
				return e
			}
			return backend.SaveDocument("notification_settings", b)
		}
	}
	restore := func(line json.RawMessage) error {
		var r record
		if json.Unmarshal(line, &r) != nil {
			return errors.New("invalid notification journal")
		}
		n.apply(r)
		return nil
	}
	var j storage.Log
	if backend != nil {
		j, err = backend.OpenLog("notifications", 64<<20, restore)
	} else {
		j, err = journal.Open(filepath.Join(dir, "notifications.jsonl"), 64<<20, restore)
	}
	if err != nil {
		return nil, err
	}
	n.journal = j
	n.cancelOldDeliveries()
	n.refreshNormalQueued()
	return n, nil
}
func (n *Notifier) apply(r record) {
	for _, f := range r.Changes {
		n.facts[f.Key] = f
	}
	for _, key := range r.Removed {
		delete(n.facts, key)
	}
	for _, d := range r.Deliveries {
		n.deliveries[deliveryKey(d)] = d
		if d.NormalEpoch != "" && d.Event.CurrentStatus == "HEALTHY" && d.LastError != "CONFIGURATION_CHANGED" && d.Status != "CANCELLED" {
			n.normalQueued[normalKey(d.EndpointID, d.NormalEpoch, d.Event.Cluster, d.Event.Node)] = true
		}
	}
	if r.RevisionSet {
		n.revision = r.Revision
	}
	n.dropped += r.Dropped
}
func (n *Notifier) persist(r record) bool {
	if n.failed {
		return false
	}
	if n.journal.Append(r, 0) != nil {
		n.failed = true
		return false
	}
	n.apply(r)
	return true
}
func (n *Notifier) pending() int {
	count := 0
	for _, d := range n.deliveries {
		if d.Status == "PENDING" || d.Status == "INFLIGHT" {
			count++
		}
	}
	return count
}
func eventID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}
func (n *Notifier) Observe(revision string, facts []Fact) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failed {
		return
	}
	current := map[string]bool{}
	configChanged := revision != n.revision
	batch := record{}
	flush := func() bool {
		if len(batch.Changes)+len(batch.Removed)+len(batch.Deliveries) == 0 {
			return true
		}
		ok := n.persist(batch)
		batch = record{}
		return ok
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Key < facts[j].Key })
	for _, f := range facts {
		current[f.Key] = true
		previous, known := n.facts[f.Key]
		// A newly enabled normal subscription receives fresh current facts once.
		// Read the incoming observation, not the older persisted change record.
		if !known || previous.Status == f.Status && category(previous) == category(f) {
			initial := n.normalSnapshots(f, known, n.pending()+len(batch.Deliveries))
			batch.Deliveries = append(batch.Deliveries, initial...)
		}
		if len(batch.Deliveries) >= 32 && !flush() {
			return
		}
		if known && previous.Status == f.Status && category(previous) == category(f) && previous.Cluster == f.Cluster && previous.Service == f.Service {
			continue
		}
		batch.Changes = append(batch.Changes, f)
		targets := destinations(n.config, category(f))
		selected := len(targets) > 0
		if n.config.Enabled && selected && known && (previous.Status != f.Status || category(previous) != category(f)) && previous.Cluster == f.Cluster && previous.Service == f.Service && !(configChanged && f.Node == "") {
			e := model.Notification{ID: eventID(), Type: "STATUS_CHANGED", Category: category(f), OccurredAt: time.Now().UTC(), Product: "RTC", Cluster: f.Cluster, Service: f.Service, Node: f.Node, PreviousStatus: previous.Status, CurrentStatus: f.Status, ReasonCode: f.ReasonCode, Reason: f.Reason, CollectedAt: f.CollectedAt, Stale: f.Stale}
			for _, target := range targets {
				if n.pending()+len(batch.Deliveries) >= n.config.MaxPending {
					batch.Dropped++
				} else {
					batch.Deliveries = append(batch.Deliveries, Delivery{EndpointID: target.ID, NormalEpoch: target.NormalEpoch, Event: e, Status: "PENDING", NextAt: time.Now().UTC(), SettingsRevision: n.settingsRevision})
				}
			}
		}
		if len(batch.Changes) >= 32 && !flush() {
			return
		}
	}
	for key := range n.facts {
		if !current[key] {
			batch.Removed = append(batch.Removed, key)
			if len(batch.Removed) >= 32 && !flush() {
				return
			}
		}
	}
	if !flush() {
		return
	}
	if configChanged {
		n.persist(record{Revision: revision, RevisionSet: true})
	}
}
func (n *Notifier) SendOne(ctx context.Context) {
	n.sendOne(ctx, "")
}
func (n *Notifier) sendOne(ctx context.Context, retryID string) {
	n.mu.Lock()
	if n.failed || !n.config.Enabled {
		n.mu.Unlock()
		return
	}
	var chosen *Delivery
	for _, d := range n.deliveries {
		if retryID != "" && deliveryKey(d) != retryID {
			continue
		}
		if (d.Status == "PENDING" || d.Status == "INFLIGHT") && !time.Now().Before(d.NextAt) {
			if chosen == nil || d.Event.OccurredAt.Before(chosen.Event.OccurredAt) {
				item := d
				chosen = &item
			}
		}
	}
	if chosen == nil {
		n.mu.Unlock()
		return
	}
	d := *chosen
	cfg, client, settingsRevision := n.config, n.client, n.settingsRevision
	cfg, selected := deliveryConfig(cfg, d)
	if !selected {
		d.Status = "CANCELLED"
		d.LastError = "CATEGORY_DISABLED"
		n.persist(record{Deliveries: []Delivery{d}})
		n.mu.Unlock()
		return
	}
	if d.EndpointID != "" || cfg.URL != n.config.URL || cfg.TLSServerName != n.config.TLSServerName {
		client = n.newClient(cfg)
		defer client.CloseIdleConnections()
	}
	if d.Attempts >= n.config.MaxAttempts {
		d.Status = "FAILED"
		d.LastError = "RETRY_EXHAUSTED"
		n.persist(record{Deliveries: []Delivery{d}})
		n.mu.Unlock()
		return
	}
	d.Attempts++
	d.Status = "INFLIGHT"
	d.NextAt = time.Now().Add(time.Duration(n.config.TimeoutSeconds+1) * time.Second)
	if !n.persist(record{Deliveries: []Delivery{d}}) {
		n.mu.Unlock()
		return
	}
	requestCtx, cancel := context.WithCancel(ctx)
	n.inflightCancel = cancel
	defer cancel()
	n.mu.Unlock()
	data, _ := json.Marshal(d.Event)
	success, _, reason := postWebhook(requestCtx, client, cfg, data)
	n.mu.Lock()
	if settingsRevision != n.settingsRevision {
		n.mu.Unlock()
		return
	}
	n.inflightCancel = nil
	if success {
		d.Status = "DELIVERED"
		d.LastError = ""
	} else {
		d.LastError = reason
		if d.Attempts >= cfg.MaxAttempts {
			d.Status = "FAILED"
		} else {
			d.Status = "PENDING"
			// First retry is immediate; later intervals grow. The documentation
			// does not specify exact intervals; 1s/5s are this system's choices.
			delay := time.Duration(0)
			if d.Attempts == 2 {
				delay = time.Second
			}
			if d.Attempts == 3 {
				delay = 5 * time.Second
			}
			d.NextAt = time.Now().Add(delay)
		}
	}
	persisted := n.persist(record{Deliveries: []Delivery{d}})
	n.mu.Unlock()
	if persisted && !success && d.Attempts == 1 && ctx.Err() == nil {
		// Retry this exact event now, rather than waiting for the scheduler.
		n.sendOne(ctx, deliveryKey(d))
	}
}
func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.SendOne(ctx)
		}
	}
}
func (n *Notifier) Summary() any {
	n.mu.Lock()
	defer n.mu.Unlock()
	delivered, failed := 0, 0
	recent := []Delivery{}
	for _, d := range n.deliveries {
		if d.Status == "DELIVERED" {
			delivered++
		}
		if d.Status == "FAILED" {
			failed++
		}
		recent = append(recent, d)
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].Event.OccurredAt.After(recent[j].Event.OccurredAt) })
	if len(recent) > 50 {
		recent = recent[:50]
	}
	return map[string]any{"enabled": n.config.Enabled, "pending": n.pending(), "delivered": delivered, "failed": failed, "dropped": n.dropped, "storage_available": !n.failed, "recent": recent}
}
func (n *Notifier) Close() { n.journal.Close() }
