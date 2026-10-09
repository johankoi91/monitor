// restart-demo exercises only the self-created avops-v1-canary container.
// It never stops a container, grants a business-node whitelist, or prints keys.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/access"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"gopkg.in/yaml.v3"
)

const nodeID = "rtc-192-59/avops-v1-canary"

type privateState struct {
	Restart  access.Issued `json:"restart"`
	Wrong    access.Issued `json:"wrong"`
	Revoked  access.Issued `json:"revoked"`
	Requests []string      `json:"requests"`
}
type state struct {
	ContainerID string         `json:"container_id"`
	SnapshotID  string         `json:"snapshot_id"`
	Image       string         `json:"image"`
	OperationID string         `json:"operation_id"`
	RequestKey  string         `json:"request_key"`
	Results     map[string]any `json:"results"`
}
type api struct {
	ID, Secret string
	Client     *http.Client
}

func (a api) call(method, path string, body any, target any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, "http://127.0.0.1:18084"+path, reader)
	if a.ID != "" {
		r.SetBasicAuth(a.ID, a.Secret)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.Client.Do(r)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if target != nil && len(data) > 0 {
		if err = json.Unmarshal(data, target); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
func wait(limit time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}
func main() {
	step := flag.String("step", "", "stage/register/deny/center-rule/node-deny/agent-deny/start/finish/replay/agent-rule")
	flag.Parse()
	env, err := os.ReadFile("/etc/avops-monitor/center.env")
	if err != nil {
		fatal("cannot load private configuration")
	}
	vars := map[string]string{}
	for _, line := range strings.Split(string(env), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			vars[k] = strings.Trim(v, "\"'")
		}
	}
	admin := api{ID: vars["AVOPS_ADMIN_ID"], Secret: vars["AVOPS_ADMIN_SECRET"], Client: &http.Client{Timeout: 5 * time.Second}}
	proofPath := "/var/lib/avops-monitor/restart-demo-proof-20261006.json"
	keyPath := "/etc/avops-monitor/restart-demo-private.json"
	p := state{Results: map[string]any{}}
	if b, e := os.ReadFile(proofPath); e == nil {
		json.Unmarshal(b, &p)
	}
	var keys privateState
	if b, e := os.ReadFile(keyPath); e == nil {
		json.Unmarshal(b, &keys)
	}
	client := func(k access.Issued) api { return api{ID: k.Key.ID, Secret: k.Secret, Client: admin.Client} }
	request := func(key string) model.RestartRequest {
		return model.RestartRequest{NodeID: nodeID, Operator: "重启权限演练", Reason: "只操作新建演练容器，验证注册 Key 与双端白名单", RequestKey: key}
	}
	check := func(a api, body model.RestartRequest, want int, wantCode string) {
		var result map[string]any
		status, e := a.call("POST", "/api/v1/operations/restart", body, &result)
		if e != nil || status != want || wantCode != "" && result["code"] != wantCode {
			fatal(fmt.Sprintf("permission check failed: HTTP %d / %v", status, result["code"]))
		}
		p.Results[wantCode] = map[string]any{"http": status, "code": result["code"], "message": result["message"]}
	}
	switch *step {
	case "stage":
		var c model.Container
		if !wait(45*time.Second, func() bool {
			var response struct {
				Containers []model.Container `json:"containers"`
			}
			_, e := admin.call("GET", "/api/v1/containers?agent_id=rtc-192-59&name=avops-v1-canary", nil, &response)
			if e != nil {
				return false
			}
			for _, item := range response.Containers {
				if item.ContainerName == "avops-v1-canary" && !item.Stale {
					c = item
					return true
				}
			}
			return false
		}) {
			fatal("canary discovery timeout")
		}
		if c.Image != "avops-canary:1.0.0" {
			fatal("wrong canary image")
		}
		var b model.Baseline
		admin.call("GET", "/api/v1/baseline", nil, &b)
		for _, cl := range b.Definition.Clusters {
			for _, s := range cl.Services {
				for _, n := range s.Nodes {
					if n.ID == nodeID {
						fatal("demo already registered")
					}
				}
			}
		}
		selection := model.Selection{ExpectedRevision: b.Revision, Additions: []model.Addition{{AgentID: c.AgentID, ContainerID: c.ContainerID, SnapshotID: c.SnapshotID, ClusterCode: "restart-demo", ClusterName: "真实重启权限演练", ServiceCode: "ops-restart-demo", ServiceName: "重启权限演练容器（非 RTC 业务）", Checks: []model.Check{{Type: "tcp", Host: "127.0.0.1", Port: 18098, TimeoutMS: 2000}}}}, Removals: []string{}}
		status, e := admin.call("POST", "/api/v1/baseline/selection", selection, &b)
		if e != nil || status != 200 {
			fatal("stage failed")
		}
		p.ContainerID = c.ContainerID
		p.SnapshotID = c.SnapshotID
		p.Image = c.Image
		p.Results["stage"] = true
	case "register":
		for _, purpose := range []string{"RESTART"} {
			var response struct {
				ID string `json:"request_id"`
			}
			status, e := admin.call("POST", "/api/v1/access-key-requests", map[string]any{"name": "20261006 真实演练 " + purpose, "reason": "在试点新建的独立容器验证权限与恢复"}, &response)
			if e != nil || status != 202 {
				fatal("key application failed")
			}
			keys.Requests = append(keys.Requests, response.ID)
			nodes := []string{nodeID}
			var issued access.Issued
			status, e = admin.call("POST", "/api/v1/access-keys", access.IssueRequest{RequestID: response.ID, AllowedNodes: nodes, ApprovalReason: "本次授权演练，只含新建非业务容器"}, &issued)
			if e != nil || status != 201 {
				fatal("key issuance failed")
			}
			keys.Restart = issued
		}
		for _, kind := range []string{"wrong", "revoked"} {
			nodes := []string{nodeID}
			if kind == "wrong" {
				nodes = []string{"rtc-192-59/agora_event_collector"}
			}
			var issued access.Issued
			status, e := admin.call("POST", "/api/v1/access-keys", access.IssueRequest{Name: "20261006 演练 " + kind, Purpose: "RESTART", AllowedNodes: nodes, ApprovalReason: "仅做拒绝验证，验证后立即停用"}, &issued)
			if e != nil || status != 201 {
				fatal("scope test key failed")
			}
			if kind == "wrong" {
				keys.Wrong = issued
			} else {
				keys.Revoked = issued
				var result map[string]any
				admin.call("POST", "/api/v1/access-keys/"+issued.Key.ID+"/revoke", map[string]any{}, &result)
			}
		}
		encoded, _ := json.Marshal(keys)
		if os.WriteFile(keyPath, encoded, 0600) != nil {
			fatal("private key handoff write failed")
		}
		p.Results["key_registration"] = map[string]any{"restart_key_id": keys.Restart.Key.ID, "node_scope": []string{nodeID}, "requests": keys.Requests}
	case "deny":
		check(api{Client: admin.Client}, request("unauth"), 401, "UNAUTHORIZED")
		check(api{ID: "invalid", Secret: "invalid", Client: admin.Client}, request("bad"), 401, "UNAUTHORIZED")
		check(admin, request("bootstrap"), 403, "RESTART_KEY_REQUIRED")
		check(client(keys.Wrong), request("wrong"), 403, "KEY_NODE_DENIED")
		check(client(keys.Revoked), request("revoked"), 401, "UNAUTHORIZED")
		var result map[string]any
		admin.call("POST", "/api/v1/access-keys/"+keys.Wrong.Key.ID+"/revoke", map[string]any{}, &result)
	case "node-deny":
		check(client(keys.Restart), request("demo-white-denied-20261006"), 409, "NODE_NOT_RESTARTABLE")
	case "center-rule", "agent-rule":
		rule := model.RestartRule{AgentID: "rtc-192-59", ContainerName: "avops-v1-canary", ServiceCode: "ops-restart-demo", Image: "avops-canary:1.0.0", Approved: true, ApprovalRef: "用户授权在 RTC 试点的独立容器做真实恢复演练，不含现有业务"}
		if *step == "center-rule" {
			cfg, e := center.ReadConfig("/etc/avops-monitor/center.yaml")
			if e != nil {
				fatal("cannot read center configuration")
			}
			cfg.RestartRules = append(cfg.RestartRules, rule)
			data, _ := yaml.Marshal(cfg)
			if os.WriteFile("/etc/avops-monitor/restart-demo-center.yaml", data, 0600) != nil {
				fatal("cannot stage rule")
			}
		} else {
			data, _ := yaml.Marshal([]model.RestartRule{rule})
			if os.WriteFile("/etc/avops-monitor/restart-demo-agent-rules.yaml", data, 0600) != nil {
				fatal("cannot stage Agent rule")
			}
		}
		p.Results[*step] = true
	case "agent-deny", "start":
		key := "demo-agent-denied-20261006"
		if *step == "start" {
			key = "demo-recovery-20261006"
		}
		var response struct {
			Operation model.Operation `json:"operation"`
		}
		status, e := client(keys.Restart).call("POST", "/api/v1/operations/restart", request(key), &response)
		if e != nil || status != 202 {
			fatal(fmt.Sprintf("restart acceptance failed: %d", status))
		}
		p.OperationID = response.Operation.OperationID
		p.RequestKey = key
		p.Results[*step] = p.OperationID
	case "finish":
		var operation model.Operation
		if !wait(90*time.Second, func() bool {
			var response struct {
				Operation model.Operation `json:"operation"`
			}
			_, e := admin.call("GET", "/api/v1/operations/"+p.OperationID, nil, &response)
			if e != nil {
				return false
			}
			operation = response.Operation
			return operation.Status == "SUCCESS" || operation.Status == "REJECTED" || operation.Status == "FAILED" || operation.Status == "TIMEOUT"
		}) {
			fatal("operation result timeout")
		}
		p.Results["last_operation"] = operation
		if operation.Status != "SUCCESS" && operation.Status != "REJECTED" {
			fatal("unexpected operation outcome")
		}
		if operation.Status == "SUCCESS" && (operation.Task.ContainerID != p.ContainerID || operation.Task.SnapshotID != p.SnapshotID) {
			fatal("original identity mismatch")
		}
	case "replay":
		var response struct {
			Operation model.Operation `json:"operation"`
		}
		status, e := client(keys.Restart).call("POST", "/api/v1/operations/restart", request(p.RequestKey), &response)
		if e != nil || status != 202 || response.Operation.OperationID != p.OperationID {
			fatal("idempotency failed")
		}
		changed := request(p.RequestKey)
		changed.Reason = "different payload"
		check(client(keys.Restart), changed, 409, "IDEMPOTENCY_CONFLICT")
		p.Results["replay_same_operation"] = true
	default:
		fatal("unknown demo step")
	}
	encoded, _ := json.MarshalIndent(p, "", "  ")
	if os.WriteFile(proofPath, encoded, 0600) != nil {
		fatal("proof write failed")
	}
	summary, _ := json.Marshal(map[string]any{"step": *step, "results": p.Results})
	fmt.Println(string(summary))
}
