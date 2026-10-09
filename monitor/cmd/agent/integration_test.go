package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/internal/agent"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/docker"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

func TestRealWireProtocolRestartsOnceAndVerifies(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "avops-wire-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	started := "2020-01-01T00:00:00Z"
	restarts := 0
	id := strings.Repeat("a", 64)
	dockerServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1.39/containers/json":
			json.NewEncoder(w).Encode([]map[string]string{{"Id": id}})
		case r.Method == "GET" && r.URL.Path == "/v1.39/containers/"+id+"/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": id, "Name": "/canary", "Image": "sha256:fixture", "Path": "/canary", "Args": []string{}, "Config": map[string]any{"Image": "rtc/canary:1", "Env": []string{}, "Entrypoint": []string{"/canary"}}, "HostConfig": map[string]any{"NetworkMode": "host", "RestartPolicy": map[string]any{"Name": "no"}}, "State": map[string]any{"Status": "running", "StartedAt": started, "ExitCode": 0}, "RestartCount": 0})
		case r.Method == "POST" && r.URL.Path == "/v1.39/containers/"+id+"/restart":
			restarts++
			started = time.Now().UTC().Format(time.RFC3339Nano)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected Docker mutation/path: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	})}
	go dockerServer.Serve(listener)
	defer dockerServer.Close()
	rules := []model.RestartRule{{AgentID: "fixture", ContainerName: "canary", ServiceCode: "rtc-canary", Image: "rtc/canary:1", Approved: true, ApprovalRef: "isolated protocol test"}}
	store, err := center.NewStore(filepath.Join(dir, "center"), center.Config{Site: model.Site{Code: "fixture", Name: "Fixture"}, Agents: []center.AgentConfig{{ID: "fixture", Secret: strings.Repeat("f", 32), HostName: "fixture", HostAddress: "127.0.0.1"}}, RestartRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer((&center.Server{Store: store, AdminID: "admin", AdminSecret: strings.Repeat("z", 32)}).Handler())
	defer server.Close()
	collector := docker.New(socket, docker.Policy{})
	runner, err := agent.New(filepath.Join(dir, "agent"), "fixture", rules, collector)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.SetBasicAuth("fixture", strings.Repeat("f", 32))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/agent/connect", req.Header)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); run(ctx, conn, collector, runner, 100*time.Millisecond) }()
	defer func() { cancel(); conn.Close(); <-done }()
	deadline := time.Now().Add(12 * time.Second)
	for len(store.Containers()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	containers := store.Containers()
	if len(containers) != 1 {
		t.Fatal("no wire snapshot")
	}
	c := containers[0]
	_, err = store.Save(model.Selection{Additions: []model.Addition{{AgentID: "fixture", ContainerID: c.ContainerID, SnapshotID: c.SnapshotID, ClusterCode: "fixture", ClusterName: "Fixture", ServiceCode: "rtc-canary", ServiceName: "Canary"}}, Removals: []string{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyRestartRules(); err != nil {
		t.Fatal(err)
	}
	request := model.RestartRequest{NodeID: "fixture/canary", Operator: "test", Reason: "isolated wire restart", RequestKey: "wire-once"}
	operation, err := store.CreateRestart(request, "test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		store.TickOperations(time.Now())
		o, _ := store.Operation(operation.OperationID)
		if o.Status == "SUCCESS" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	o, _ := store.Operation(operation.OperationID)
	if o.Status != "SUCCESS" {
		t.Fatalf("wire operation did not succeed: %+v", o)
	}
	duplicate, err := store.CreateRestart(request, "test", "127.0.0.1")
	if err != nil || duplicate.OperationID != o.OperationID {
		t.Fatal("wire idempotency failed")
	}
	mu.Lock()
	count := restarts
	mu.Unlock()
	if count != 1 {
		t.Fatalf("Docker restart attempts=%d", count)
	}
}
