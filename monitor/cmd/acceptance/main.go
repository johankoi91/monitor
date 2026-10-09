// acceptance checks an isolated canary through the real center API. It never
// stops/restarts business containers or opens restart permissions.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

type checker struct {
	url, id, secret, path string
	client                *http.Client
}
type proof struct {
	ContainerID    string         `json:"container_id"`
	SnapshotID     string         `json:"snapshot_id"`
	OperationID    string         `json:"operation_id"`
	Image          string         `json:"image"`
	AfterStartedAt string         `json:"after_started_at"`
	Results        map[string]any `json:"results"`
}

func (c checker) request(method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	r, err := http.NewRequest(method, c.url+path, reader)
	if err != nil {
		return err
	}
	r.SetBasicAuth(c.id, c.secret)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(r)
	if err != nil {
		return fmt.Errorf("API unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("API returned %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}
func wait(limit time.Duration, check func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}
func (c checker) status(want string) bool {
	var response struct {
		Services []struct {
			Code     string `json:"service_code"`
			Status   string `json:"status"`
			Expected int    `json:"expected_node_count"`
			Nodes    []struct {
				Runtime string `json:"runtime_status"`
				Status  string `json:"status"`
			}
		}
	}
	if c.request("GET", "/api/v1/services/status?service_code=rtc-pilot-canary", nil, &response) != nil {
		return false
	}
	for _, s := range response.Services {
		if s.Code == "rtc-pilot-canary" && s.Expected == 1 && s.Status == want {
			return true
		}
	}
	return false
}
func main() {
	step := flag.String("step", "", "setup/healthy/exited/restart/finish/duplicate/benchmark/notifications")
	requestKey := flag.String("request-key", "rtc-v1-acceptance-original-restart", "stable request key for this isolated test round")
	base := flag.String("url", "http://127.0.0.1:28084", "local center URL")
	envPath := flag.String("env", "/etc/avops-monitor-acceptance/center.env", "private environment configuration")
	statePath := flag.String("state", "/var/lib/avops-monitor-acceptance/acceptance-proof.json", "isolated evidence path")
	flag.Parse()
	data, err := os.ReadFile(*envPath)
	if err != nil {
		log.Fatal("cannot load private configuration")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(value, "\"'")
		}
	}
	c := checker{url: *base, id: values["AVOPS_ADMIN_ID"], secret: values["AVOPS_ADMIN_SECRET"], path: *statePath, client: &http.Client{Timeout: 5 * time.Second}}
	if c.id == "" || c.secret == "" {
		log.Fatal("private credentials unavailable")
	}
	p := proof{Results: map[string]any{}}
	if saved, err := os.ReadFile(c.path); err == nil {
		if json.Unmarshal(saved, &p) != nil {
			log.Fatal("invalid acceptance evidence")
		}
	}
	fail := func(text string) { log.Fatal(text) }
	switch *step {
	case "setup":
		var found model.Container
		if !wait(60*time.Second, func() bool {
			var response struct {
				Containers []model.Container `json:"containers"`
			}
			if c.request("GET", "/api/v1/containers?agent_id=rtc-v1-qa&name=avops-v1-canary", nil, &response) != nil {
				return false
			}
			for _, candidate := range response.Containers {
				if candidate.ContainerName == "avops-v1-canary" && !candidate.Stale {
					found = candidate
					return true
				}
			}
			return false
		}) {
			fail("canary discovery timed out")
		}
		if found.Image != "avops-canary:1.0.0" {
			fail("unexpected canary image")
		}
		var baseline model.Baseline
		if c.request("GET", "/api/v1/baseline", nil, &baseline) != nil {
			fail("baseline read failed")
		}
		if baseline.Active {
			fail("acceptance baseline already exists; refusing overwrite")
		}
		selection := model.Selection{ExpectedRevision: baseline.Revision, Additions: []model.Addition{{AgentID: found.AgentID, ContainerID: found.ContainerID, SnapshotID: found.SnapshotID, ClusterCode: "rtc-v1-acceptance", ClusterName: "V1.0 隔离验收", ServiceCode: "rtc-pilot-canary", ServiceName: "V1.0 独立验收容器", Checks: []model.Check{{Type: "tcp", Host: "127.0.0.1", Port: 18098, TimeoutMS: 2000}}}}, Removals: []string{}}
		if c.request("POST", "/api/v1/baseline/selection", selection, &baseline) != nil {
			fail("canary selection failed")
		}
		p.ContainerID = found.ContainerID
		p.SnapshotID = found.SnapshotID
		p.Image = found.Image
		p.Results["discovery_yaml"] = true
	case "healthy", "exited":
		want := "HEALTHY"
		limit := 60 * time.Second
		if *step == "exited" {
			want = "UNHEALTHY"
			limit = 30 * time.Second
		}
		started := time.Now()
		if !wait(limit, func() bool { return c.status(want) }) {
			fail("canary status transition timed out")
		}
		p.Results[*step+"_seconds"] = time.Since(started).Seconds()
	case "restart", "duplicate":
		request := model.RestartRequest{NodeID: "rtc-v1-qa/avops-v1-canary", Operator: "V1.0 隔离验收", Reason: "仅重启本次新建的独立验收容器", RequestKey: *requestKey}
		var response struct {
			Operation model.Operation `json:"operation"`
		}
		if !wait(10*time.Second, func() bool { return c.request("POST", "/api/v1/operations/restart", request, &response) == nil }) {
			fail("controlled restart request failed")
		}
		if *step == "duplicate" {
			if response.Operation.OperationID != p.OperationID {
				fail("duplicate request created another operation")
			}
			p.Results["cross_restart_idempotency"] = true
		} else {
			p.OperationID = response.Operation.OperationID
			p.Results["operation_accepted"] = true
		}
	case "finish":
		var operation model.Operation
		if !wait(90*time.Second, func() bool {
			var response struct {
				Operation model.Operation `json:"operation"`
			}
			if c.request("GET", "/api/v1/operations/"+p.OperationID, nil, &response) != nil {
				return false
			}
			operation = response.Operation
			return operation.Status == "SUCCESS" || operation.Status == "FAILED" || operation.Status == "TIMEOUT" || operation.Status == "REJECTED"
		}) {
			fail("operation result timed out")
		}
		if operation.Status != "SUCCESS" || operation.NodeLocked || !operation.ResultKnown || operation.Evidence == nil || operation.Task.ContainerID != p.ContainerID || operation.Task.SnapshotID != p.SnapshotID {
			fail("original restart/verification failed")
		}
		p.AfterStartedAt = operation.Evidence.AfterStartedAt
		p.Results["original_restart_verified"] = true
		p.Results["operation_seconds"] = operation.DurationMS / 1000
		var records struct {
			Records []model.AuditRecord `json:"records"`
		}
		if c.request("GET", "/api/v1/operations/"+p.OperationID+"/audit", nil, &records) != nil || len(records.Records) < 4 {
			fail("audit trail missing")
		}
		p.Results["audit_records"] = len(records.Records)
	case "notifications":
		var response struct {
			Delivered int `json:"delivered"`
			Failed    int `json:"failed"`
			Pending   int `json:"pending"`
		}
		if !wait(30*time.Second, func() bool {
			return c.request("GET", "/api/v1/notifications/status", nil, &response) == nil && response.Delivered >= 2 && response.Failed == 0 && response.Pending == 0
		}) {
			fail("real notification delivery not confirmed")
		}
		p.Results["notifications_delivered"] = response.Delivered
	case "benchmark":
		var mu sync.Mutex
		var wg sync.WaitGroup
		latencies := []float64{}
		failures := 0
		for worker := 0; worker < 10; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					start := time.Now()
					var result json.RawMessage
					err := c.request("GET", "/api/v1/services/status", nil, &result)
					elapsed := float64(time.Since(start).Microseconds()) / 1000
					mu.Lock()
					latencies = append(latencies, elapsed)
					if err != nil {
						failures++
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		sort.Float64s(latencies)
		p95 := latencies[94]
		if failures > 0 || p95 >= 1000 {
			fail("status API P95 budget failed")
		}
		p.Results["status_api_p95_ms"] = p95
		p.Results["status_api_requests"] = 100
	default:
		fail("unknown acceptance step")
	}
	if err = os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		fail("evidence directory unavailable")
	}
	encoded, _ := json.MarshalIndent(p, "", "  ")
	if err = os.WriteFile(c.path, encoded, 0600); err != nil {
		fail("evidence write failed")
	}
	summary, _ := json.Marshal(map[string]any{"step": *step, "results": p.Results})
	fmt.Println(string(summary))
}
