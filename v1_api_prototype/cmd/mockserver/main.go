package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type restartRequest struct {
	NodeID     string `json:"node_id"`
	Operator   string `json:"operator"`
	Reason     string `json:"reason"`
	RequestKey string `json:"request_key"`
}

type operation struct {
	OperationID  string `json:"operation_id"`
	RequestKey   string `json:"request_key"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	NodeID       string `json:"node_id"`
	Operator     string `json:"operator"`
	Reason       string `json:"reason"`
	RequestedAt  string `json:"requested_at"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	BeforeStatus string `json:"before_status"`
	AfterStatus  string `json:"after_status,omitempty"`
	Message      string `json:"message"`
}

type nodeRule struct {
	restartEnabled bool
	currentStatus  string
	detail         string
}

type mockServer struct {
	mu             sync.RWMutex
	operations     map[string]*operation
	requestKeys    map[string]string
	restartableMap map[string]nodeRule
}

func main() {
	server := &mockServer{
		operations:  make(map[string]*operation),
		requestKeys: make(map[string]string),
		restartableMap: map[string]nodeRule{
			"rtc-zw-edge-01/agora_local_ap": {restartEnabled: true, currentStatus: "HEALTHY"},
			"rtm-zw-01/forwarder0":          {restartEnabled: true, currentStatus: "HEALTHY"},
			"rtm-zw-01/forwarder1":          {restartEnabled: false, currentStatus: "UNHEALTHY", detail: "container is missing"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", server.handleLive)
	mux.HandleFunc("/health/ready", server.handleReady)
	mux.HandleFunc("/version", server.handleVersion)
	mux.HandleFunc("/openapi.yaml", server.handleOpenAPI)
	mux.HandleFunc("/api/v1/services/status", server.handleStatus)
	mux.HandleFunc("/api/v1/operations/restart", server.handleRestart)
	mux.HandleFunc("/api/v1/operations/", server.handleOperation)

	port := env("AVOPS_MOCK_PORT", "18083")
	log.Printf("av-ops V1.0 API prototype listening on http://127.0.0.1:%s", port)
	log.Fatal(http.ListenAndServe("127.0.0.1:"+port, requestLogger(mux)))
}

func (s *mockServer) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	respond(w, http.StatusOK, map[string]any{"status": "UP", "checked_at": now()})
}

func (s *mockServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	respond(w, http.StatusOK, map[string]any{"status": "READY", "checked_at": now()})
}

func (s *mockServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	respond(w, http.StatusOK, map[string]string{"version": "1.0.0-prototype", "api_version": "v1"})
}

func (s *mockServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	http.ServeFile(w, r, "openapi.yaml")
}

func (s *mockServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	serviceFilter := strings.TrimSpace(r.URL.Query().Get("service_code"))
	statusFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))

	services := mockServices()
	filtered := make([]map[string]any, 0, len(services))
	for _, service := range services {
		if serviceFilter != "" && service["service_code"] != serviceFilter {
			continue
		}
		if statusFilter != "" && service["status"] != statusFilter {
			continue
		}
		filtered = append(filtered, service)
	}

	respond(w, http.StatusOK, map[string]any{
		"code":         0,
		"message":      "OK",
		"request_id":   requestID(),
		"generated_at": now(),
		"services":     filtered,
	})
}

func (s *mockServer) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	var request restartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	request.NodeID = strings.TrimSpace(request.NodeID)
	request.Operator = strings.TrimSpace(request.Operator)
	request.Reason = strings.TrimSpace(request.Reason)
	request.RequestKey = strings.TrimSpace(request.RequestKey)
	if request.NodeID == "" || request.Operator == "" || len(request.Reason) < 2 || request.RequestKey == "" {
		respondError(w, http.StatusBadRequest, "REQUIRED_FIELD_MISSING", "node_id, operator, reason and request_key are required")
		return
	}

	s.mu.Lock()
	if operationID := s.requestKeys[request.RequestKey]; operationID != "" {
		item := *s.operations[operationID]
		s.mu.Unlock()
		respond(w, http.StatusAccepted, operationEnvelope(item))
		return
	}
	rule, exists := s.restartableMap[request.NodeID]
	if !exists {
		s.mu.Unlock()
		respondError(w, http.StatusNotFound, "NODE_NOT_FOUND", "node is not present in the service baseline")
		return
	}
	if !rule.restartEnabled || rule.currentStatus == "UNKNOWN" || rule.currentStatus == "UNHEALTHY" {
		s.mu.Unlock()
		message := "node is not restartable in its current state"
		if rule.detail != "" {
			message += ": " + rule.detail
		}
		respondError(w, http.StatusConflict, "NODE_NOT_RESTARTABLE", message)
		return
	}
	for _, existing := range s.operations {
		if existing.NodeID == request.NodeID && (existing.Status == "PENDING" || existing.Status == "RUNNING") {
			s.mu.Unlock()
			respondError(w, http.StatusConflict, "OPERATION_IN_PROGRESS", "another operation is already running for this node")
			return
		}
	}

	operationID := "OP-" + time.Now().Format("20060102-150405") + "-" + requestID()[:4]
	startedAt := now()
	item := &operation{
		OperationID: operationID, RequestKey: request.RequestKey, Type: "RESTART_CONTAINER", Status: "RUNNING",
		NodeID: request.NodeID, Operator: request.Operator, Reason: request.Reason, RequestedAt: startedAt, StartedAt: startedAt,
		BeforeStatus: rule.currentStatus, Message: "重启任务已下发，等待状态复查",
	}
	s.operations[operationID] = item
	s.requestKeys[request.RequestKey] = operationID
	s.mu.Unlock()

	go s.completeMockOperation(operationID)
	respond(w, http.StatusAccepted, operationEnvelope(*item))
}

func (s *mockServer) completeMockOperation(operationID string) {
	time.Sleep(2 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.operations[operationID]
	if item == nil {
		return
	}
	item.Status = "SUCCESS"
	item.AfterStatus = "HEALTHY"
	item.FinishedAt = now()
	item.DurationMS = 2000
	item.Message = "模拟重启完成，容器和端口检查均已恢复"
}

func (s *mockServer) handleOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, r)
		return
	}
	operationID := strings.TrimPrefix(r.URL.Path, "/api/v1/operations/")
	s.mu.RLock()
	item := s.operations[operationID]
	s.mu.RUnlock()
	if item == nil {
		respondError(w, http.StatusNotFound, "OPERATION_NOT_FOUND", "operation does not exist")
		return
	}
	respond(w, http.StatusOK, operationEnvelope(*item))
}

func mockServices() []map[string]any {
	return []map[string]any{
		service("rtc-ap", "RTC AP", "HEALTHY", 1, 1, 1, 0, 0, []map[string]any{
			node("rtc-zw-edge-01/agora_local_ap", "rtc-zw-edge-01", "10.244.17.180", "agora_local_ap", "registry/agora_local_ap:3.1.3", "HEALTHY", "running", "READINESS", "OK", "容器运行且接入端口检查通过", true, false),
		}),
		service("rtc-web-edge", "RTC Web Edge", "HEALTHY", 1, 1, 1, 0, 0, []map[string]any{
			node("rtc-zw-edge-01/agora_web_media_edge_1", "rtc-zw-edge-01", "10.244.17.180", "agora_web_media_edge_1", "registry/agora_web_media_edge:4.0.9", "HEALTHY", "running", "READINESS", "OK", "TLS 端口 4501 检查通过", true, false),
		}),
		service("rtm-core-forwarder", "RTM Core Forwarder", "DEGRADED", 2, 1, 1, 1, 0, []map[string]any{
			node("rtm-zw-01/forwarder0", "rtm-zw-01", "10.244.17.218", "forwarder0", "rtm/forwarder:2.2.4", "HEALTHY", "running", "READINESS", "OK", "容器运行且端口 18011 检查通过", true, false),
			node("rtm-zw-01/forwarder1", "rtm-zw-01", "10.244.17.218", "forwarder1", "rtm/forwarder:2.2.4", "UNHEALTHY", "missing", "CONTAINER", "CONTAINER_MISSING", "应有容器未出现在最新快照中", false, false),
		}),
		service("rtm-edge-registrar", "RTM Edge Registrar", "UNKNOWN", 1, 1, 0, 0, 1, []map[string]any{
			node("rtm-zw-01/registrar0", "rtm-zw-01", "10.244.17.218", "registrar0", "rtm/registrar:2.2.4", "UNKNOWN", "unknown", "AGENT", "STATUS_STALE", "Agent 状态已超过 45 秒", false, true),
		}),
	}
}

func service(code, name, status string, expected, discovered, healthy, unhealthy, unknown int, nodes []map[string]any) map[string]any {
	return map[string]any{
		"service_code": code, "service_name": name, "status": status,
		"expected_node_count": expected, "discovered_node_count": discovered, "healthy_node_count": healthy,
		"unhealthy_node_count": unhealthy, "unknown_node_count": unknown, "nodes": nodes,
	}
}

func node(id, hostName, hostAddress, container, image, status, runtimeStatus, checkLevel, reasonCode, reason string, restartEnabled, stale bool) map[string]any {
	return map[string]any{
		"node_id": id, "host_name": hostName, "host_address": hostAddress, "container_name": container, "image": image,
		"status": status, "runtime_status": runtimeStatus, "check_level": checkLevel, "reason_code": reasonCode,
		"reason": reason, "restart_count": 0, "restart_enabled": restartEnabled, "collected_at": now(), "stale": stale,
	}
}

func operationEnvelope(item operation) map[string]any {
	return map[string]any{"code": 0, "message": "OK", "request_id": requestID(), "operation": item}
}

func respondError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"code": code, "message": message, "request_id": requestID()})
}

func respond(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", fmt.Sprintf("%s is not allowed", r.Method))
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(started).Round(time.Millisecond))
	})
}

func requestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func now() string { return time.Now().Format(time.RFC3339) }

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
