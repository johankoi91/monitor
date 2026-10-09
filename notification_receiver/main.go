package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxBodyBytes = 256 << 10

var errConflict = errors.New("event ID already has a different payload")
var errCapacity = errors.New("receiver storage capacity reached")
var errUnavailable = errors.New("receiver storage unavailable")

type event struct {
	ID             string     `json:"event_id"`
	Type           string     `json:"event_type"`
	OccurredAt     time.Time  `json:"occurred_at"`
	Product        string     `json:"product"`
	Cluster        string     `json:"cluster"`
	Service        string     `json:"service_code"`
	Node           string     `json:"node_id,omitempty"`
	PreviousStatus string     `json:"previous_status"`
	CurrentStatus  string     `json:"current_status"`
	ReasonCode     string     `json:"reason_code"`
	Reason         string     `json:"reason"`
	CollectedAt    *time.Time `json:"collected_at"`
	Stale          *bool      `json:"stale"`
}

type record struct {
	ReceivedAt time.Time `json:"received_at"`
	Event      event     `json:"event"`
	RawBody    string    `json:"raw_body,omitempty"`
}

type store struct {
	mu       sync.Mutex
	file     *os.File
	records  []record
	digests  map[string][32]byte
	bytes    int64
	maxBytes int64
	failed   bool
}

func openStore(dir string, maxBytes int64) (*store, error) {
	if maxBytes <= 0 {
		return nil, errors.New("storage limit must be positive")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another receiver is using the storage file")
	}
	parent, err := os.Open(dir)
	if err == nil {
		err = parent.Sync()
		parent.Close()
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	s := &store{file: f, maxBytes: maxBytes, digests: make(map[string][32]byte)}
	info, err := f.Stat()
	if err == nil {
		s.bytes = info.Size()
		if s.bytes > maxBytes {
			err = errors.New("existing data exceeds configured storage limit")
		}
	}
	// A truncated last record must not be silently acknowledged or joined to a new record.
	if err == nil && s.bytes > 0 {
		last := make([]byte, 1)
		_, err = f.ReadAt(last, s.bytes-1)
		if err == nil && last[0] != '\n' {
			err = errors.New("incomplete last record; preserve file and repair before starting")
		}
	}
	if err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), maxBodyBytes*4)
		line := 0
		for scanner.Scan() {
			line++
			var r record
			if err = json.Unmarshal(scanner.Bytes(), &r); err != nil {
				err = fmt.Errorf("invalid stored record at line %d", line)
				break
			}
			if err = r.Event.validate(); err != nil {
				err = fmt.Errorf("invalid stored event at line %d", line)
				break
			}
			if _, exists := s.digests[r.Event.ID]; exists {
				err = fmt.Errorf("duplicate stored event at line %d", line)
				break
			}
			s.digests[r.Event.ID] = digest(r.Event)
			s.records = append(s.records, r)
		}
		if err == nil {
			err = scanner.Err()
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

func digest(e event) [32]byte {
	data, _ := json.Marshal(e)
	return sha256.Sum256(data)
}

func (e event) validate() error {
	for _, field := range []string{e.ID, e.Product, e.Cluster, e.Service, e.ReasonCode} {
		if strings.TrimSpace(field) == "" || len(field) > 256 {
			return errors.New("required identifier missing or too long")
		}
	}
	if (e.Type != "STATUS_CHANGED" && e.Type != "WEBHOOK_TEST") || e.Product != "RTC" || e.OccurredAt.IsZero() || e.Stale == nil {
		return errors.New("invalid event type, product, timestamp or stale flag")
	}
	if len(e.Node) > 512 || len(e.Reason) > 4096 {
		return errors.New("node ID or reason too long")
	}
	validStatus := func(v string) bool {
		return v == "HEALTHY" || v == "DEGRADED" || v == "UNHEALTHY" || v == "UNKNOWN"
	}
	if !validStatus(e.PreviousStatus) || !validStatus(e.CurrentStatus) {
		return errors.New("invalid health status")
	}
	return nil
}

func (s *store) accept(e event) (bool, error) {
	return s.acceptBody(e, "")
}

func (s *store) acceptBody(e event, rawBody string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return false, errUnavailable
	}
	d := digest(e)
	if previous, exists := s.digests[e.ID]; exists {
		if previous != d {
			return false, errConflict
		}
		return true, nil
	}
	r := record{ReceivedAt: time.Now().UTC(), Event: e, RawBody: rawBody}
	line, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	line = append(line, '\n')
	if s.bytes+int64(len(line)) > s.maxBytes {
		return false, errCapacity
	}
	n, err := s.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = s.file.Sync()
	}
	if err != nil {
		// Roll back a partial append; fail closed even if rollback succeeds.
		s.failed = true
		_ = s.file.Truncate(s.bytes)
		_ = s.file.Sync()
		return false, errUnavailable
	}
	s.bytes += int64(len(line))
	s.records = append(s.records, r)
	s.digests[e.ID] = d
	return false, nil
}

func (s *store) snapshot(limit int) ([]record, int, bool) {
	return s.snapshotPage(limit, 0)
}

func (s *store) snapshotPage(limit, offset int) ([]record, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.records)
	items := make([]record, 0, limit)
	for i := count - 1 - offset; i >= 0 && len(items) < limit; i-- {
		items = append(items, s.records[i])
	}
	return items, count, !s.failed
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func handler(s *store, id, secret string, signingSecrets ...string) http.Handler {
	webhookSecret := secret
	if len(signingSecrets) > 0 {
		webhookSecret = signingSecrets[0]
	}
	idHash, secretHash := sha256.Sum256([]byte(id)), sha256.Sum256([]byte(secret))
	mux := http.NewServeMux()
	mux.HandleFunc("/", receiverPage)
	mux.HandleFunc("/api/v1/notifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			jsonResponse(w, 405, map[string]string{"code": "METHOD_NOT_ALLOWED"})
			return
		}
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		if mediaType != "application/json" {
			jsonResponse(w, 415, map[string]string{"code": "JSON_REQUIRED"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			jsonResponse(w, 413, map[string]string{"code": "BODY_TOO_LARGE"})
			return
		}
		if !verifySignature(webhookSecret, body, r.Header) {
			jsonResponse(w, 401, map[string]string{"code": "INVALID_SIGNATURE"})
			return
		}
		var e event
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&e)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errors.New("expected exactly one JSON object")
		}
		var fields map[string]json.RawMessage
		if err == nil {
			err = json.Unmarshal(body, &fields)
		}
		if err == nil {
			for _, field := range []string{"reason", "collected_at"} {
				if _, exists := fields[field]; !exists {
					err = errors.New("missing event field")
				}
			}
		}
		if err == nil {
			err = e.validate()
		}
		if err != nil {
			jsonResponse(w, 400, map[string]string{"code": "INVALID_EVENT", "message": err.Error()})
			return
		}
		duplicate, err := s.acceptBody(e, string(body))
		if err != nil {
			status, code := 503, "STORAGE_UNAVAILABLE"
			if errors.Is(err, errConflict) {
				status, code = 409, "EVENT_ID_CONFLICT"
			} else if errors.Is(err, errCapacity) {
				status, code = 507, "STORAGE_CAPACITY_REACHED"
			}
			jsonResponse(w, status, map[string]string{"code": code})
			return
		}
		jsonResponse(w, 200, map[string]any{"code": "OK", "event_id": e.ID, "duplicate": duplicate, "persisted": true})
	})
	mux.HandleFunc("/api/v1/notifications/recent", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			jsonResponse(w, 405, map[string]string{"code": "METHOD_NOT_ALLOWED"})
			return
		}
		limit := 50
		if q := r.URL.Query().Get("limit"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 1 || v > 200 {
				jsonResponse(w, 400, map[string]string{"code": "INVALID_LIMIT"})
				return
			}
			limit = v
		}
		offset := 0
		if q := r.URL.Query().Get("offset"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 0 {
				jsonResponse(w, 400, map[string]string{"code": "INVALID_OFFSET"})
				return
			}
			offset = v
		}
		items, count, ready := s.snapshotPage(limit, offset)
		jsonResponse(w, 200, map[string]any{"events": items, "total": count, "ready": ready, "offset": offset})
	})
	for _, route := range []string{"/health/live", "/health/ready"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				jsonResponse(w, 405, map[string]string{"code": "METHOD_NOT_ALLOWED"})
				return
			}
			_, count, ready := s.snapshot(0)
			status := 200
			if r.URL.Path == "/health/ready" && !ready {
				status = 503
			}
			jsonResponse(w, status, map[string]any{"ready": ready, "persisted_count": count, "version": "1.0.0"})
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Webhook callbacks authenticate the raw body; Basic is only for the
		// receiver's separate diagnostic read endpoints.
		if r.URL.Path == "/api/v1/notifications" {
			mux.ServeHTTP(w, r)
			return
		}
		i, key, ok := r.BasicAuth()
		iH, kH := sha256.Sum256([]byte(i)), sha256.Sum256([]byte(key))
		validID := subtle.ConstantTimeCompare(iH[:], idHash[:])
		validKey := subtle.ConstantTimeCompare(kH[:], secretHash[:])
		if !ok || validID&validKey != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="avops-notification-receiver", charset="UTF-8"`)
			jsonResponse(w, 401, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func sourceGuard(next http.Handler, configured string) (http.Handler, error) {
	if strings.TrimSpace(configured) == "" {
		return next, nil
	}
	allowed := make([]net.IP, 0)
	for _, value := range strings.Split(configured, ",") {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			return nil, errors.New("invalid allowed source IP")
		}
		allowed = append(allowed, ip)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		for _, candidate := range allowed {
			if err == nil && ip != nil && candidate.Equal(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		jsonResponse(w, 403, map[string]string{"code": "SOURCE_NOT_ALLOWED"})
	}), nil
}

func main() {
	id, secret := os.Getenv("AVOPS_RECEIVER_ID"), os.Getenv("AVOPS_RECEIVER_SECRET")
	webhookSecret := os.Getenv("AVOPS_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = secret
	}
	cert, key := os.Getenv("AVOPS_RECEIVER_TLS_CERT"), os.Getenv("AVOPS_RECEIVER_TLS_KEY")
	if id == "" || secret == "" || strings.Contains(id, ":") || cert == "" || key == "" {
		log.Fatal("receiver ID, secret, TLS certificate and key must be configured; ID cannot contain ':'")
	}
	capacity := int64(64 << 20)
	if configured := os.Getenv("AVOPS_RECEIVER_MAX_BYTES"); configured != "" {
		var err error
		capacity, err = strconv.ParseInt(configured, 10, 64)
		if err != nil || capacity <= 0 {
			log.Fatal("invalid AVOPS_RECEIVER_MAX_BYTES")
		}
	}
	dir := os.Getenv("AVOPS_RECEIVER_DATA_DIR")
	if dir == "" {
		dir = "./data"
	}
	s, err := openStore(dir, capacity)
	if err != nil {
		log.Fatal(err)
	}
	defer s.file.Close()
	addr := os.Getenv("AVOPS_RECEIVER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18443"
	}
	h, err := receiverSourceGuard(handler(s, id, secret, webhookSecret), os.Getenv("AVOPS_RECEIVER_ALLOWED_IPS"), os.Getenv("AVOPS_RECEIVER_VIEW_ALLOWED_IPS"))
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Print("graceful shutdown timed out")
		}
	}()
	log.Printf("notification receiver listening on %s (HTTPS), storage capacity=%d bytes", addr, capacity)
	if err := server.ListenAndServeTLS(cert, key); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
