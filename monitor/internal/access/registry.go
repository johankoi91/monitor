// Package access issues and restores account-owned restart Tokens.
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
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Kind      string     `json:"kind,omitempty"`
	OwnerID   string     `json:"owner_id,omitempty"`
	ID        string     `json:"key_id"`
	Name      string     `json:"name"`
	Purpose   string     `json:"purpose"`
	CreatedAt time.Time  `json:"created_at"`
	CreatedBy string     `json:"created_by"`
	Enabled   bool       `json:"enabled"`
	RequestID string     `json:"request_id,omitempty"`
}
type IssueRequest struct {
	ExpiresHours int    `json:"expires_hours,omitempty"`
	OwnerID      string `json:"-"`
	RequestID    string `json:"request_id,omitempty"`
	Name         string `json:"name"`
	Purpose      string `json:"purpose"`
}
type Issued struct {
	Key    Key    `json:"key"`
	Secret string `json:"secret"`
	Token  string `json:"token,omitempty"`
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
	mu     sync.Mutex
	log    storage.Log
	keys   map[string]Key
	hashes map[string]string
}

func random() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func hash(secret string) string { h := sha256.Sum256([]byte(secret)); return hex.EncodeToString(h[:]) }
func New(dir string, backends ...storage.Backend) (*Registry, error) {
	r := &Registry{keys: map[string]Key{}, hashes: map[string]string{}}
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
func (r *Registry) Issue(input IssueRequest, source string) (Issued, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.RequestID != "" {
		return Issued{}, errors.New("申请流程已移除，请直接签发重启安全 Key")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || input.Purpose != "RESTART" {
		return Issued{}, errors.New("仅支持签发重启安全 Key，请填写名称")
	}
	secret := random()
	if input.ExpiresHours == 0 {
		input.ExpiresHours = 24
	}
	if input.ExpiresHours < 1 || input.ExpiresHours > 720 {
		return Issued{}, errors.New("Token有效期须为1至720小时")
	}
	expires := time.Now().UTC().Add(time.Duration(input.ExpiresHours) * time.Hour)
	key := Key{Kind: string(credential.KindRestart), ID: "KEY-" + random()[:24], Name: input.Name, Purpose: input.Purpose, CreatedAt: time.Now().UTC(), CreatedBy: source, Enabled: true, RequestID: input.RequestID}
	key.OwnerID = input.OwnerID
	key.ExpiresAt = &expires
	if err := r.persist(record{At: time.Now().UTC(), Kind: "ISSUE", Source: source, Key: &key, SecretHash: hash(secret)}); err != nil {
		return Issued{}, err
	}
	return Issued{Key: key, Secret: secret, Token: "rt_" + key.ID + "." + secret}, nil
}
func (r *Registry) Authenticate(id, secret string) (Key, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[id]
	actual := hash(secret)
	expected := r.hashes[id]
	valid := subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
	return key, ok && key.Kind == string(credential.KindRestart) && key.Purpose == "RESTART" && key.Enabled && valid && (key.ExpiresAt == nil || time.Now().Before(*key.ExpiresAt))
}
func (r *Registry) AuthenticateToken(token string) (Key, bool) {
	if !strings.HasPrefix(token, "rt_") {
		return Key{}, false
	}
	id, secret, ok := strings.Cut(strings.TrimPrefix(token, "rt_"), ".")
	if !ok {
		return Key{}, false
	}
	k, valid := r.Authenticate(id, secret)
	return k, valid && k.ExpiresAt != nil
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
