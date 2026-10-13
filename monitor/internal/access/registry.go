// Package access manages system credentials, without personal accounts or RBAC.
package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/credential"
	"github.com/johankoi91/monitor/runtime/internal/journal"
	"github.com/johankoi91/monitor/runtime/internal/storage"
)

type Request struct {
	ID          string    `json:"request_id"`
	Name        string    `json:"name"`
	Purpose     string    `json:"purpose"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	RequestedAt time.Time `json:"requested_at"`
	KeyID       string    `json:"key_id,omitempty"`
}
type Key struct {
	Kind      string    `json:"kind,omitempty"`
	OwnerID   string    `json:"owner_id,omitempty"`
	ID        string    `json:"key_id"`
	Name      string    `json:"name"`
	Purpose   string    `json:"purpose"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	Enabled   bool      `json:"enabled"`
	RequestID string    `json:"request_id,omitempty"`
}
type IssueRequest struct {
	OwnerID        string `json:"-"`
	RequestID      string `json:"request_id,omitempty"`
	Name           string `json:"name"`
	Purpose        string `json:"purpose"`
	ApprovalReason string `json:"approval_reason,omitempty"` // legacy input ignored
}
type Issued struct {
	Key    Key    `json:"key"`
	Secret string `json:"secret"`
}
type record struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Request    *Request  `json:"request,omitempty"`
	Key        *Key      `json:"key,omitempty"`
	SecretHash string    `json:"secret_hash,omitempty"`
}
type Registry struct {
	mu       sync.Mutex
	log      storage.Log
	requests map[string]Request
	keys     map[string]Key
	hashes   map[string]string
}

var ErrDenied = errors.New("access key denied")

func random() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func hash(secret string) string { h := sha256.Sum256([]byte(secret)); return hex.EncodeToString(h[:]) }
func New(dir string, backends ...storage.Backend) (*Registry, error) {
	r := &Registry{requests: map[string]Request{}, keys: map[string]Key{}, hashes: map[string]string{}}
	restore := func(line json.RawMessage) error {
		var item record
		if json.Unmarshal(line, &item) != nil || item.At.IsZero() {
			return errors.New("corrupt access key registry")
		}
		r.apply(item)
		return nil
	}
	var j storage.Log
	var err error
	if len(backends) > 0 && backends[0] != nil {
		j, err = backends[0].OpenLog("access", 16<<20, restore)
	} else {
		j, err = journal.Open(filepath.Join(dir, "access-keys.jsonl"), 16<<20, restore)
	}
	if err != nil {
		return nil, err
	}
	r.log = j
	return r, nil
}
func (r *Registry) apply(item record) {
	if item.Request != nil {
		r.requests[item.Request.ID] = *item.Request
	}
	if item.Key != nil {
		if item.Key.Kind == "" {
			item.Key.Kind = string(credential.Kind(item.Key.Purpose))
		}
		r.keys[item.Key.ID] = *item.Key
		if item.SecretHash != "" {
			r.hashes[item.Key.ID] = item.SecretHash
		}
	}
}
func (r *Registry) persist(item record) error {
	if err := r.log.Append(item, 0); err != nil {
		return errors.New("接入密钥记录无法可靠保存")
	}
	r.apply(item)
	return nil
}
func (r *Registry) Apply(name, purpose, reason, source string) (Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name = strings.TrimSpace(name)
	reason = strings.TrimSpace(reason)
	if name == "" || len(name) > 128 || len(reason) < 2 || len(reason) > 1000 || purpose != "RESTART" {
		return Request{}, errors.New("仅支持申请重启安全 Key，请填写名称和申请原因")
	}
	if len(r.requests) >= 1000 {
		return Request{}, errors.New("密钥申请容量已满，请联系管理端")
	}
	item := Request{ID: "REQ-" + random()[:24], Name: name, Purpose: purpose, Reason: reason, Status: "PENDING", RequestedAt: time.Now().UTC()}
	if err := r.persist(record{At: time.Now().UTC(), Kind: "APPLY", Source: source, Request: &item}); err != nil {
		return Request{}, err
	}
	return item, nil
}
func (r *Registry) Issue(input IssueRequest, source string) (Issued, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.RequestID != "" {
		return Issued{}, errors.New("申请流程已移除，请直接签发重启安全 Key")
	}
	var request *Request
	if input.RequestID != "" {
		found, ok := r.requests[input.RequestID]
		if !ok || found.Status != "PENDING" {
			return Issued{}, errors.New("申请不存在或已处理")
		}
		request = &found
		input.Name = found.Name
		input.Purpose = found.Purpose
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || input.Purpose != "RESTART" {
		return Issued{}, errors.New("仅支持签发重启安全 Key，请填写名称")
	}
	secret := random()
	key := Key{Kind: string(credential.KindRestart), ID: "KEY-" + random()[:24], Name: input.Name, Purpose: input.Purpose, CreatedAt: time.Now().UTC(), CreatedBy: source, Enabled: true, RequestID: input.RequestID}
	key.OwnerID = input.OwnerID
	if request != nil {
		request.Status = "ISSUED"
		request.KeyID = key.ID
	}
	if err := r.persist(record{At: time.Now().UTC(), Kind: "ISSUE", Source: source, Key: &key, SecretHash: hash(secret), Request: request}); err != nil {
		return Issued{}, err
	}
	return Issued{Key: key, Secret: secret}, nil
}
func (r *Registry) Authenticate(id, secret string) (Key, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	actual := hash(secret)
	expected := r.hashes[id]
	valid := subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
	return key, ok && key.Kind == string(credential.KindRestart) && key.Purpose == "RESTART" && key.Enabled && valid
}
func (r *Registry) Revoke(id, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	if !ok {
		return errors.New("密钥不存在")
	}
	key.Enabled = false
	return r.persist(record{At: time.Now().UTC(), Kind: "REVOKE", Source: source, Key: &key})
}
func (r *Registry) List() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := []Key{}
	for _, k := range r.keys {
		if k.Purpose == "RESTART" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].CreatedAt.Equal(keys[j].CreatedAt) {
			return keys[i].ID > keys[j].ID
		}
		return keys[i].CreatedAt.After(keys[j].CreatedAt)
	})
	return map[string]any{"keys": keys}
}
func (r *Registry) Close() { r.log.Close() }
