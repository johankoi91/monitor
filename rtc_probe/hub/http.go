package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (h *Hub) ServeProbe(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || !h.Authenticate(id, secret) {
		reply(w, 401, map[string]string{"code": "PROBE_UNAUTHORIZED"})
		return
	}
	if r.Header.Get("Origin") != "" {
		reply(w, 403, map[string]string{"code": "ORIGIN_DENIED"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/probe/uploads/") {
		if strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/v1/probe/uploads/"), "/") {
			h.uploadParts(w, r, id)
			return
		}
		h.upload(w, r, id)
		return
	}
	if r.URL.Path != "/api/v1/probe/connect" || r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	disabled := h.state.DisabledProbes[id]
	h.mu.Unlock()
	if disabled {
		reply(w, 403, map[string]string{"code": "PROBE_DISABLED"})
		return
	}
	conn, err := (&websocket.Upgrader{HandshakeTimeout: 5 * time.Second}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	s := h.Session(id)
	defer h.Disconnect(id, s)
	conn.SetReadLimit(protocol.MaxMessage)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		write := func(m protocol.Message) bool {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			return conn.WriteJSON(m) == nil
		}
		for {
			select {
			case <-done:
				return
			case <-s.Stop:
				_ = conn.Close()
				return
			case m := <-s.Out:
				if !write(m) {
					_ = conn.Close()
					return
				}
			case <-tick.C:
				for _, m := range h.Commands(id, s) {
					if !write(m) {
						_ = conn.Close()
						return
					}
				}
			case <-ping.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			return
		}
		m, err := protocol.Decode(data)
		if err != nil {
			return
		}
		ack, err := h.Receive(id, s, m)
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if ack.Type != "" {
			select {
			case s.Out <- ack:
			default:
				return
			}
		}
	}
}

// ServeAdmin is called only after the existing center's account/permission checks.
func (h *Hub) ServeAdmin(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/rtc"), "/"), "/")
	if len(parts) == 1 && parts[0] == "summary" && r.Method == "GET" {
		reply(w, 200, h.Summary())
		return
	}
	if len(parts) == 1 && parts[0] == "probes" && r.Method == "POST" {
		var v struct {
			ID       string `json:"probe_id"`
			Name     string `json:"name"`
			Location string `json:"location"`
			Host     string `json:"host_address"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&v) != nil || d.Decode(&struct{}{}) != io.EOF {
			reply(w, 400, map[string]string{"message": "探针登记参数无效"})
			return
		}
		secret, err := h.RegisterProbe(ProbeConfig{ID: v.ID, Name: v.Name, Location: v.Location, HostAddress: v.Host})
		if err != nil {
			reply(w, 409, map[string]string{"message": err.Error()})
			return
		}
		reply(w, 201, map[string]any{"probe_id": v.ID, "secret": secret, "message": "机器凭证只在本次返回，请妥善保存"})
		return
	}
	if len(parts) == 1 && parts[0] == "history" && r.Method == "GET" {
		limit, offset := 20, 0
		var err error
		if q := r.URL.Query().Get("limit"); q != "" {
			limit, err = strconv.Atoi(q)
		}
		if err == nil && r.URL.Query().Get("offset") != "" {
			offset, err = strconv.Atoi(r.URL.Query().Get("offset"))
		}
		if err != nil || limit < 1 || limit > 100 || offset < 0 {
			reply(w, 400, map[string]string{"message": "分页参数无效"})
			return
		}
		v, e := h.History(r.URL.Query().Get("task_id"), limit, offset)
		if e != nil {
			reply(w, 503, map[string]string{"message": e.Error()})
			return
		}
		reply(w, 200, v)
		return
	}
	if len(parts) == 1 && parts[0] == "usage" && r.Method == "GET" {
		v, err := h.Usage()
		if err != nil {
			reply(w, 503, map[string]string{"message": "历史容量查询不可用"})
			return
		}
		reply(w, 200, v)
		return
	}
	if len(parts) == 1 && parts[0] == "settings" && r.Method == "POST" {
		var request struct {
			Expected string `json:"expected_version"`
			Settings
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil || d.Decode(&struct{}{}) != io.EOF {
			reply(w, 400, map[string]string{"message": "保留设置无效"})
			return
		}
		if err := h.UpdateSettings(request.Expected, request.Settings); err != nil {
			reply(w, 409, map[string]string{"message": err.Error()})
			return
		}
		reply(w, 200, h.Snapshot())
		return
	}
	if len(parts) == 1 && parts[0] == "config" && r.Method == "POST" {
		var input configurationInput
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(&struct{}{}) != io.EOF {
			reply(w, 400, map[string]string{"message": "配置格式无效"})
			return
		}
		if err := h.UpdatePlan(input.Expected, h.configurationPlan(input)); err != nil {
			reply(w, 409, map[string]string{"message": err.Error()})
			return
		}
		reply(w, 200, h.Snapshot())
		return
	}
	if len(parts) == 1 && r.Method == "GET" && (parts[0] == "status" || parts[0] == "probes" || parts[0] == "targets" || parts[0] == "tasks" || parts[0] == "runs") {
		reply(w, 200, h.Snapshot())
		return
	}
	if len(parts) == 2 && parts[0] == "runs" && r.Method == "GET" {
		v, err := h.Detail(parts[1])
		if err != nil {
			reply(w, 404, map[string]string{"error": err.Error()})
		} else {
			reply(w, 200, v)
		}
		return
	}
	if len(parts) == 2 && parts[0] == "logs" && r.Method == "GET" {
		h.download(w, r, parts[1])
		return
	}
	if len(parts) != 3 || r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	var err error
	if parts[0] == "probes" && parts[2] == "enabled" {
		var v struct {
			Enabled *bool `json:"enabled"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&v) != nil || v.Enabled == nil {
			reply(w, 400, map[string]string{"message": "enabled 必填"})
			return
		}
		err = h.EnableProbe(parts[1], *v.Enabled)
	} else if parts[0] == "tasks" {
		switch parts[2] {
		case "run":
			var run Run
			run, err = h.Start(parts[1])
			if err == nil {
				reply(w, 202, run)
				return
			}
		case "enabled":
			var input struct {
				Enabled *bool `json:"enabled"`
			}
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
			d.DisallowUnknownFields()
			if d.Decode(&input) != nil || input.Enabled == nil || d.Decode(&struct{}{}) != io.EOF {
				reply(w, 400, map[string]string{"error": "enabled boolean required"})
				return
			}
			err = h.Enable(parts[1], *input.Enabled)
		default:
			http.NotFound(w, r)
			return
		}
	} else if parts[0] == "runs" {
		switch parts[2] {
		case "cancel":
			err = h.Cancel(parts[1])
		case "logs":
			err = h.Collect(parts[1])
		default:
			http.NotFound(w, r)
			return
		}
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		reply(w, 409, map[string]string{"error": err.Error()})
		return
	}
	reply(w, 202, map[string]bool{"accepted": true})
}
func (h *Hub) logOwner(id, probe string) (protocol.LogRequest, bool) {
	for _, run := range h.state.Runs {
		v, ok := run.Logs[id]
		if ok && (probe == "" || v.ProbeID == probe) {
			return v, true
		}
	}
	if v, err := h.archive.RTCGetLog(id); err == nil && (probe == "" || v.ProbeID == probe) {
		return v, true
	}
	return protocol.LogRequest{}, false
}
func (h *Hub) upload(w http.ResponseWriter, r *http.Request, probe string) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/probe/uploads/")
	if r.Method != "PUT" || !protocol.ValidID(id) {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	request, ok := h.logOwner(id, probe)
	h.mu.Unlock()
	if !ok || time.Now().After(request.ExpiresAt) {
		reply(w, 404, map[string]string{"error": "log request not available"})
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.MaxBundle))
	if err != nil {
		reply(w, 413, map[string]string{"error": "bundle size limit exceeded"})
		return
	}
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	if r.Header.Get("X-Content-SHA256") != digest {
		reply(w, 400, map[string]string{"error": "bundle digest mismatch"})
		return
	}
	h.mu.Lock()
	run := h.state.Runs[request.RunID]
	var expected protocol.Job
	if run != nil {
		expected, _ = logJob(run, request.Role)
	} else if saved, err := h.archive.RTCGetRun(request.RunID); err == nil {
		expected, _ = logJob(&saved, request.Role)
	}
	h.mu.Unlock()
	partial, validationErr := validateDiagnosticBundle(b, request, expected)
	if validationErr != nil {
		reply(w, 400, map[string]string{"error": validationErr.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.logOwner(id, probe)
	if !ok {
		reply(w, 404, map[string]string{"error": "request expired"})
		return
	}
	if current.Status == "AVAILABLE" {
		if current.SHA256 != digest {
			reply(w, 409, map[string]string{"error": "conflicting bundle"})
			return
		}
		if _, err := os.Stat(filepath.Join(h.dir, "logs", id+".zip")); err == nil {
			reply(w, 200, current)
			return
		}
	}
	if err := h.reserveDiagnostic(int64(len(b)), id); err != nil {
		code := 503
		if err == errDiagnosticQuota {
			code = 507
		}
		reply(w, code, map[string]string{"error": "diagnostic archive capacity unavailable"})
		return
	}
	if err = durable.Bytes(filepath.Join(h.dir, "logs", id+".zip"), b); err == nil {
		err = h.change(func(s *state) error {
			if s.Runs[current.RunID] == nil {
				v, err := h.archive.RTCGetRun(current.RunID)
				if err != nil {
					return err
				}
				s.Runs[current.RunID] = &v
			}
			v := s.Runs[current.RunID].Logs[id]
			v.Status = "AVAILABLE"
			v.SHA256 = digest
			v.Bytes = int64(len(b))
			v.Partial = partial
			s.Runs[current.RunID].Logs[id] = v
			if terminal(s.Runs[current.RunID].Status) {
				h.addLogs(s.Runs[current.RunID], time.Now().UTC())
			}
			return nil
		})
	}
	if err != nil {
		reply(w, 503, map[string]string{"error": "diagnostic storage unavailable"})
		return
	}
	v, _ := h.logOwner(id, probe)
	reply(w, 201, v)
}
func (h *Hub) download(w http.ResponseWriter, r *http.Request, id string) {
	if !protocol.ValidID(id) {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	v, ok := h.logOwner(id, "")
	h.mu.Unlock()
	if !ok || v.Status != "AVAILABLE" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(h.dir, "logs", id+".zip"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		reply(w, 503, map[string]string{"error": "diagnostic unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="rtc-`+id+`.zip"`)
	http.ServeContent(w, r, id+".zip", stat.ModTime(), f)
}
