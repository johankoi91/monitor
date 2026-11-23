package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/johankoi91/monitor/runtime/shared/durable"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type stagedBundle struct {
	SHA256    string    `json:"sha256"`
	Bytes     int       `json:"bytes"`
	ExpiresAt time.Time `json:"expires_at"`
}

func bundleHeaders(r *http.Request) (stagedBundle, error) {
	b := stagedBundle{SHA256: r.Header.Get("X-Bundle-SHA256")}
	raw, err := hex.DecodeString(b.SHA256)
	if err != nil || len(raw) != 32 || b.SHA256 != strings.ToLower(b.SHA256) {
		return b, errors.New("invalid bundle SHA256")
	}
	b.Bytes, err = strconv.Atoi(r.Header.Get("X-Bundle-Bytes"))
	if err != nil || b.Bytes < 1 || b.Bytes > protocol.MaxBundle {
		return b, errors.New("invalid bundle size")
	}
	return b, nil
}
func (h *Hub) uploadParts(w http.ResponseWriter, r *http.Request, probe string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/probes/uploads/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !protocol.ValidID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if !((parts[1] == "parts" && (r.Method == "GET" && len(parts) == 2 || r.Method == "PUT" && len(parts) == 3)) || (parts[1] == "complete" && len(parts) == 2 && r.Method == "POST")) {
		http.NotFound(w, r)
		return
	}
	desired, err := bundleHeaders(r)
	if err != nil {
		reply(w, 400, map[string]string{"error": err.Error()})
		return
	}
	h.mu.Lock()
	request, ok := h.logOwner(id, probe)
	budget := int64(h.state.Settings.LogsMB) << 18
	h.mu.Unlock()
	if !ok || time.Now().After(request.ExpiresAt) {
		reply(w, 404, map[string]string{"error": "request expired"})
		return
	}
	desired.ExpiresAt = request.ExpiresAt
	h.uploadMu.Lock()
	defer h.uploadMu.Unlock()
	dir := filepath.Join(h.dir, "logs", "incoming", id)
	var saved stagedBundle
	exists := true
	if err = durable.Read(filepath.Join(dir, "manifest.json"), 4096, &saved); os.IsNotExist(err) {
		exists = false
	} else if err != nil {
		reply(w, 503, map[string]string{"error": "staging unavailable"})
		return
	}
	if exists && (saved.SHA256 != desired.SHA256 || saved.Bytes != desired.Bytes) {
		reply(w, 409, map[string]string{"error": "conflicting bundle"})
		return
	}
	count := (desired.Bytes + protocol.LogChunkBytes - 1) / protocol.LogChunkBytes
	partPath := func(index int) string { return filepath.Join(dir, strconv.Itoa(index)+".part") }
	partBytes := func(index int) int {
		size := protocol.LogChunkBytes
		if left := desired.Bytes - index*size; left < size {
			return left
		}
		return size
	}
	validPart := func(index int) bool {
		b, err := os.ReadFile(partPath(index))
		if err != nil || len(b) != partBytes(index) {
			return false
		}
		digest, err := os.ReadFile(partPath(index) + ".sha256")
		if err != nil {
			return false
		}
		hash := sha256.Sum256(b)
		return hex.EncodeToString(hash[:]) == string(digest)
	}
	if r.Method == "GET" {
		completed := request.Status == "AVAILABLE" && request.SHA256 == desired.SHA256 && request.Bytes == int64(desired.Bytes)
		if completed {
			_, err := os.Stat(filepath.Join(h.dir, "logs", id+".zip"))
			completed = err == nil
		}
		missing := []int{}
		if !completed {
			for i := 0; i < count; i++ {
				if !validPart(i) {
					missing = append(missing, i)
				}
			}
		}
		reply(w, 200, map[string]any{"completed": completed, "missing": missing, "chunk_bytes": protocol.LogChunkBytes})
		return
	}
	if parts[1] == "parts" {
		index, err := strconv.Atoi(parts[2])
		if err != nil || index < 0 || index >= count || strconv.Itoa(index) != parts[2] {
			reply(w, 400, map[string]string{"error": "invalid chunk index"})
			return
		}
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.LogChunkBytes))
		if err != nil || len(b) != partBytes(index) {
			reply(w, 400, map[string]string{"error": "invalid chunk size"})
			return
		}
		hash := sha256.Sum256(b)
		digest := hex.EncodeToString(hash[:])
		if r.Header.Get("X-Content-SHA256") != digest {
			reply(w, 400, map[string]string{"error": "chunk digest mismatch"})
			return
		}
		if !exists {
			if budget < int64(protocol.MaxBundle*2) {
				budget = int64(protocol.MaxBundle * 2)
			}
			var reserved int64
			entries, err := os.ReadDir(filepath.Dir(dir))
			if err != nil && !os.IsNotExist(err) {
				reply(w, 503, map[string]string{"error": "staging unavailable"})
				return
			}
			for _, entry := range entries {
				var v stagedBundle
				if durable.Read(filepath.Join(filepath.Dir(dir), entry.Name(), "manifest.json"), 4096, &v) == nil {
					reserved += int64(v.Bytes)
				}
			}
			if reserved+int64(desired.Bytes) > budget {
				reply(w, 507, map[string]string{"error": "staging capacity exhausted"})
				return
			}
			if err = durable.Write(filepath.Join(dir, "manifest.json"), desired); err != nil {
				reply(w, 503, map[string]string{"error": "staging persistence failed"})
				return
			}
		}
		if validPart(index) {
			old, _ := os.ReadFile(partPath(index) + ".sha256")
			if string(old) != digest {
				reply(w, 409, map[string]string{"error": "conflicting chunk"})
				return
			}
			reply(w, 200, map[string]bool{"stored": true})
			return
		}
		if err = durable.Bytes(partPath(index)+".sha256", []byte(digest)); err == nil {
			err = durable.Bytes(partPath(index), b)
		}
		if err != nil {
			reply(w, 503, map[string]string{"error": "chunk persistence failed"})
			return
		}
		reply(w, 201, map[string]bool{"stored": true})
		return
	}
	if !exists {
		reply(w, 409, map[string]string{"error": "chunks not uploaded"})
		return
	}
	var assembled bytes.Buffer
	for i := 0; i < count; i++ {
		if !validPart(i) {
			reply(w, 409, map[string]string{"error": "missing or corrupt chunk"})
			return
		}
		b, _ := os.ReadFile(partPath(i))
		assembled.Write(b)
	}
	clone := r.Clone(r.Context())
	url := *r.URL
	url.Path = "/api/v1/probes/uploads/" + id
	clone.URL = &url
	clone.Method = "PUT"
	clone.Body = io.NopCloser(bytes.NewReader(assembled.Bytes()))
	clone.ContentLength = int64(assembled.Len())
	clone.Header = r.Header.Clone()
	clone.Header.Set("X-Content-SHA256", desired.SHA256)
	h.upload(w, clone, probe)
	h.mu.Lock()
	current, ok := h.logOwner(id, probe)
	h.mu.Unlock()
	if ok && current.Status == "AVAILABLE" && current.SHA256 == desired.SHA256 {
		_ = os.RemoveAll(dir)
	}
}

func (h *Hub) cleanStagedUploads(now time.Time) {
	root := filepath.Join(h.dir, "logs", "incoming")
	entries, _ := os.ReadDir(root)
	removed := 0
	for _, entry := range entries {
		if removed >= 100 {
			break
		}
		if !entry.IsDir() || !protocol.ValidID(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		var staged stagedBundle
		err := durable.Read(filepath.Join(dir, "manifest.json"), 4096, &staged)
		expired := err == nil && now.After(staged.ExpiresAt)
		if err != nil {
			info, e := entry.Info()
			expired = e == nil && now.Sub(info.ModTime()) > time.Hour
		}
		if expired {
			if os.RemoveAll(dir) == nil {
				removed++
			}
		}
	}
}

// Called with the hub lock held. A log request is already scoped to one probe.
func (h *Hub) receiveLogStatus(probe string, m protocol.Message) (protocol.Message, error) {
	if m.Logs == nil || (m.Logs.Status != "EXPIRED" && m.Logs.Status != "FAILED") {
		return protocol.Message{}, errors.New("invalid log status")
	}
	v, ok := h.logOwner(m.Logs.ID, probe)
	if !ok || v.RunID != m.Logs.RunID || v.Role != m.Logs.Role || m.Logs.RuntimeID != probe {
		return protocol.Message{}, errors.New("log status owner mismatch")
	}
	if v.Status == "REQUESTED" {
		if err := h.change(func(s *state) error {
			if s.Runs[v.RunID] == nil {
				r, err := h.archive.RTCGetRun(v.RunID)
				if err != nil {
					return err
				}
				s.Runs[v.RunID] = &r
			}
			v.Status = m.Logs.Status
			if v.Status == "EXPIRED" {
				v.Error = "LOCAL_RETENTION_EXPIRED"
			} else {
				v.Error = "PROBE_LOG_UNAVAILABLE"
				if m.Logs.Error == "SDK_NOT_STARTED" || m.Logs.Error == "LOG_BUNDLE_LIMIT_EXCEEDED" {
					v.Error = m.Logs.Error
				}
			}
			s.Runs[v.RunID].Logs[v.ID] = v
			return nil
		}); err != nil {
			return protocol.Message{}, err
		}
	}
	ack := protocol.Msg("log_status_ack")
	ack.ID = v.ID
	return ack, nil
}
