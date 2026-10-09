package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/internal/agent"
	"github.com/johankoi91/monitor/runtime/internal/cadvisor"
	"github.com/johankoi91/monitor/runtime/internal/docker"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func allow(key string) map[string]bool {
	result := map[string]bool{}
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			result[item] = true
		}
	}
	return result
}
func main() {
	endpoint := os.Getenv("AVOPS_CENTER_URL")
	id, secret := os.Getenv("AVOPS_AGENT_ID"), os.Getenv("AVOPS_AGENT_SECRET")
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v1/agent/connect" {
		log.Fatal("AVOPS_CENTER_URL must be a credential-free Agent websocket URL")
	}
	if u.Scheme != "wss" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "ws" || os.Getenv("AVOPS_DEV_HTTP") != "1" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			log.Fatal("Agent requires WSS; dev WS is limited to loopback")
		}
	}
	if id == "" || len(secret) < 24 {
		log.Fatal("Agent ID and independent 24+ character secret required")
	}
	seconds, err := strconv.Atoi(env("AVOPS_COLLECT_SECONDS", "15"))
	if err != nil || seconds < 10 || seconds > 60 {
		log.Fatal("collection period must be 10–60 seconds")
	}
	collector := docker.New(env("AVOPS_DOCKER_SOCKET", "/var/run/docker.sock"), docker.Policy{EnvAllow: allow("AVOPS_PUBLIC_ENV"), ArgAllow: allow("AVOPS_PUBLIC_ARGS")})
	resources, err := cadvisor.New(os.Getenv("AVOPS_CADVISOR_URL"))
	if err != nil {
		log.Fatal(err)
	}
	if resources != nil {
		resources.FilesystemRoot = env("AVOPS_DOCKER_DATA_ROOT", "/var/lib/docker")
	}
	rules, err := agent.ReadRules(os.Getenv("AVOPS_RESTART_RULES_FILE"))
	if err != nil {
		log.Fatal("cannot load Agent restart rules")
	}
	runner, err := agent.New(env("AVOPS_AGENT_DATA_DIR", "data/agent"), id, rules, collector)
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: os.Getenv("AVOPS_TLS_SERVER_NAME")}}
	headers := http.Header{}
	headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":"+secret)))
	backoff := time.Second
	for ctx.Err() == nil {
		conn, resp, err := dialer.DialContext(ctx, endpoint, headers)
		if err != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			log.Print("center connection unavailable; retrying")
		} else {
			log.Print("Agent connected; Docker collection active")
			started := time.Now()
			run(ctx, conn, collector, runner, time.Duration(seconds)*time.Second, resources)
			conn.Close()
			if time.Since(started) > time.Minute {
				backoff = time.Second
			}
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func run(ctx context.Context, conn *websocket.Conn, collector *docker.Collector, runner *agent.Runner, period time.Duration, resourceClients ...*cadvisor.Client) {
	collectionCtx, stopCollection := context.WithCancel(ctx)
	defer stopCollection()
	var mu sync.RWMutex
	plan := model.Plan{Nodes: []model.PlanNode{}}
	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done); stopCollection(); conn.Close() }) }
	defer finish()
	outbound := make(chan any, 64)
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})
	go func() {
		defer finish()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var header struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &header) != nil {
				return
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			switch header.Type {
			case "checks":
				var next model.Plan
				if dec.Decode(&next) != nil || dec.Decode(&struct{}{}) != io.EOF || len(next.Nodes) > 2048 {
					return
				}
				mu.Lock()
				plan = next
				mu.Unlock()
			case "task":
				var task model.Task
				if dec.Decode(&task) != nil || dec.Decode(&struct{}{}) != io.EOF {
					return
				}
				if task.Action == "QUERY" {
					result := runner.Query(task)
					select {
					case outbound <- result:
					default:
					}
				} else {
					runner.Handle(ctx, task)
				}
			case "result_ack":
				var ack model.ResultAck
				if dec.Decode(&ack) != nil || dec.Decode(&struct{}{}) != io.EOF {
					return
				}
				runner.Ack(ack.OperationID)
			default:
				return
			}
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		}
	}()
	// Give the center's initial probe plan a chance to arrive without delaying shutdown.
	initial := time.NewTimer(time.Second)
	select {
	case <-done:
		initial.Stop()
		return
	case <-ctx.Done():
		initial.Stop()
		return
	case <-initial.C:
	}
	var collectors sync.WaitGroup
	collectors.Add(1)
	go func() {
		defer collectors.Done()
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		var sequence uint64
		for {
			mu.RLock()
			current := plan
			mu.RUnlock()
			passCtx, cancel := context.WithTimeout(collectionCtx, 25*time.Second)
			report := collector.Collect(passCtx, current)
			cancel()
			if len(resourceClients) > 0 && resourceClients[0] != nil {
				resourceClients[0].Enrich(collectionCtx, &report)
			}
			sequence++
			report.Sequence = sequence
			data, err := json.Marshal(report)
			if err != nil || len(data) > 16<<20 {
				report = model.Report{Type: "snapshot", Sequence: sequence, CollectedAt: time.Now().UTC(), Complete: false, Error: "snapshot exceeds message budget", Containers: []model.Observation{}}
				data, _ = json.Marshal(report)
			}
			if !report.Complete {
				log.Print("collection incomplete; last snapshot will age instead of reporting missing containers")
			}
			select {
			case outbound <- json.RawMessage(data):
			case <-collectionCtx.Done():
				return
			}
			select {
			case <-collectionCtx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { stopCollection(); collectors.Wait() }()
	results := time.NewTicker(time.Second)
	defer results.Stop()
	write := func(value any) bool {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(value) == nil
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case message := <-outbound:
			if !write(message) {
				return
			}
		case <-results.C:
			items := runner.Results()
			if len(items) > 64 {
				items = items[:64]
			}
			for _, result := range items {
				if !write(result) {
					return
				}
			}
		}
	}
}
