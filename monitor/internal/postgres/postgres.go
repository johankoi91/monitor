// Package postgres provides the single center's transactional durable state.
package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/johankoi91/monitor/runtime/internal/storage"
	_ "github.com/lib/pq"
	"io"
	"net"
	"net/url"
	"sync"
	"time"
)

//go:embed schema.sql
var schema string

type DB struct {
	Pool        *sql.DB
	mu          sync.Mutex
	writer      *sql.Conn
	aead        cipher.AEAD
	failed      bool
	maintenance maintenanceState
}

func Open(dsn string, key []byte, migrate bool) (*DB, error) {
	u, e := url.Parse(dsn)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		return nil, errors.New("a PostgreSQL URL is required")
	}
	ip := net.ParseIP(u.Hostname())
	if (ip == nil || !ip.IsLoopback()) && u.Query().Get("sslmode") != "verify-full" {
		return nil, errors.New("remote PostgreSQL requires verified TLS")
	}
	if len(key) != 32 {
		return nil, errors.New("PostgreSQL storage needs a separate 32-byte encryption key")
	}
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(4)
	pool.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL unavailable")
	}
	if migrate {
		tx, e := pool.BeginTx(ctx, nil)
		if e != nil {
			pool.Close()
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, schema); e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
		if e != nil {
			pool.Close()
			return nil, errors.New("PostgreSQL schema migration failed")
		}
	}
	var version int
	if err = pool.QueryRowContext(ctx, "SELECT max(version) FROM avops.schema_migrations").Scan(&version); err != nil || version != 1 {
		pool.Close()
		return nil, errors.New("PostgreSQL schema version mismatch")
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	var locked bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(716017,1)").Scan(&locked); err != nil || !locked {
		conn.Close()
		pool.Close()
		return nil, errors.New("another center owns the PostgreSQL writer lock")
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	return &DB{Pool: pool, writer: conn, aead: aead}, nil
}
func (d *DB) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	d.writer.ExecContext(ctx, "SELECT pg_advisory_unlock(716017,1)")
	d.writer.Close()
	d.Pool.Close()
	d.failed = true
}
func (d *DB) Healthy() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return false
	}
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if d.writer.PingContext(ctx) != nil {
		d.failed = true
		return false
	}
	return true
}
func (d *DB) transaction(fn func(context.Context, *sql.Tx) error) error {
	return d.transact(8*time.Second, fn)
}
func (d *DB) transact(timeout time.Duration, fn func(context.Context, *sql.Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return errors.New("PostgreSQL writer unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	tx, err := d.writer.BeginTx(ctx, nil)
	if err != nil {
		d.failed = true
		return errors.New("PostgreSQL writer unavailable")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL synchronous_commit=on"); err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) seal(name string, data []byte) ([]byte, error) {
	nonce := make([]byte, d.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return d.aead.Seal(nonce, nonce, data, []byte(name)), nil
}
func (d *DB) unseal(name string, data []byte) ([]byte, error) {
	n := d.aead.NonceSize()
	if len(data) < n {
		return nil, errors.New("encrypted settings corrupt")
	}
	return d.aead.Open(nil, data[:n], data[n:], []byte(name))
}
func (d *DB) LoadDocument(name string) ([]byte, bool, error) {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	var body, encrypted []byte
	err := d.Pool.QueryRowContext(ctx, "SELECT payload,encrypted_payload FROM avops.documents WHERE name=$1", name).Scan(&body, &encrypted)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("PostgreSQL document read failed")
	}
	if encrypted != nil {
		body, err = d.unseal(name, encrypted)
	}
	return body, true, err
}
func (d *DB) SaveDocument(name string, data []byte) error {
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error { return d.saveDocument(ctx, tx, name, data) })
}
func (d *DB) saveDocument(ctx context.Context, tx *sql.Tx, name string, data []byte) error {
	if !json.Valid(data) {
		return errors.New("invalid durable document")
	}
	var body any = string(data)
	var encrypted any
	if name == "notification_settings" {
		b, err := d.seal(name, data)
		if err != nil {
			return err
		}
		encrypted = b
		body = nil
	}
	var header struct {
		Baseline struct {
			Revision string `json:"revision"`
		} `json:"baseline"`
		Revision string `json:"revision"`
	}
	json.Unmarshal(data, &header)
	revision := header.Revision
	if name == "baseline" {
		revision = header.Baseline.Revision
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO avops.documents(name,payload,encrypted_payload,revision) VALUES($1,$2,$3,$4) ON CONFLICT(name) DO UPDATE SET payload=excluded.payload,encrypted_payload=excluded.encrypted_payload,revision=excluded.revision,updated_at=now()", name, body, encrypted, revision); err != nil {
		return err
	}
	if name == "baseline" {
		return projectBaseline(ctx, tx, data)
	}
	return nil
}

type pgLog struct {
	db   *DB
	name string
}

func (d *DB) OpenLog(name string, limit int64, restore func(json.RawMessage) error) (storage.Log, error) {
	rows, err := d.restoreLog(name)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err = restore(r); err != nil {
			return nil, err
		}
	}
	return &pgLog{d, name}, nil
}
func (l *pgLog) Append(value any, _ int64) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return l.db.transaction(func(ctx context.Context, tx *sql.Tx) error { return projectLog(ctx, tx, l.name, b) })
}
func (l *pgLog) Scan(visit func(json.RawMessage) error) error {
	rows, err := l.db.ReadLog(l.name, "", 0)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err = visit(row); err != nil {
			return err
		}
	}
	return nil
}
func (l *pgLog) Healthy() bool         { return l.db.Healthy() }
func (l *pgLog) Usage() (int64, int64) { return 0, 0 }
func (l *pgLog) Close()                {}
func (d *DB) restoreLog(name string) ([]json.RawMessage, error) {
	switch name {
	case "operations":
		return d.queryRaw("SELECT jsonb_build_object('at',requested_at,'kind','RESTORED','operation',payload) FROM avops.operations ORDER BY requested_at", nil)
	case "access":
		return d.queryRaw("SELECT payload FROM avops.access_audit ORDER BY id", nil)
	case "notifications":
		return d.queryRaw("SELECT jsonb_build_object('changes',COALESCE((SELECT jsonb_agg(payload) FROM avops.notification_facts),'[]'::jsonb),'deliveries',COALESCE((SELECT jsonb_agg(payload) FROM avops.notification_events),'[]'::jsonb),'revision',revision,'revision_set',true,'dropped',dropped) FROM avops.notification_state WHERE id=true", nil)
	default:
		return nil, errors.New("unknown PostgreSQL journal")
	}
}
func (d *DB) queryRaw(query string, args []any) ([]json.RawMessage, error) {
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
	rows, err := d.Pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}
func (d *DB) ReadLog(name, selector string, limit int) ([]json.RawMessage, error) {
	if name == "operations" && selector != "" {
		if limit < 1 || limit > 500 {
			limit = 200
		}
		return d.queryRaw("SELECT payload FROM (SELECT id,payload FROM avops.operation_audit WHERE operation_id=$1 ORDER BY id DESC LIMIT $2) q ORDER BY id", []any{selector, limit})
	}
	tables := map[string]string{"operations": "operation_audit", "access": "access_audit", "notifications": "notification_audit"}
	table := tables[name]
	if table == "" {
		return nil, errors.New("unknown audit log")
	}
	return d.queryRaw("SELECT payload FROM avops."+table+" ORDER BY id", nil)
}
func (d *DB) SaveSnapshot(id string, b []byte) error {
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error { return projectSnapshot(ctx, tx, id, b) })
}
func (d *DB) LoadSnapshots() (map[string]json.RawMessage, error) {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	rows, err := d.Pool.QueryContext(ctx, "SELECT agent_id,payload FROM avops.agent_snapshots")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]json.RawMessage{}
	for rows.Next() {
		var id string
		var b []byte
		if err = rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		result[id] = b
	}
	return result, rows.Err()
}
func jsonText(value any) string { b, _ := json.Marshal(value); return string(b) }
func randomID() string          { var b [16]byte; io.ReadFull(rand.Reader, b[:]); return fmt.Sprintf("%x", b) }
