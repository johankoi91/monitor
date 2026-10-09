package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

const fixture = `{"Id":"container-a","Name":"/agora_local_ap","Image":"sha256:image","Path":"/opt/ap","Args":["--password","-secret-cli","--port=443","https://alice:url-secret@example.com/a?q=query-secret"],"Config":{"Image":"rtc/ap:1","Entrypoint":["sh","-c","echo shell-secret"],"Cmd":["--api-key=api-secret"],"Env":["PASSWORD=env-secret","TZ=Asia/Shanghai","ACCESS_TOKEN=token-secret"],"WorkingDir":"/opt","User":"root","ExposedPorts":{"443/tcp":{}}},"HostConfig":{"Binds":["/config:/etc/ap:ro"],"NetworkMode":"host","RestartPolicy":{"Name":"always","MaximumRetryCount":0},"PortBindings":{"443/tcp":[{"HostIp":"127.0.0.1","HostPort":"443"}]}},"State":{"Status":"running","StartedAt":"2026-01-01T00:00:00Z","ExitCode":0},"RestartCount":0}`

func TestRedactionAndStableFingerprint(t *testing.T) {
	var raw inspect
	if err := json.Unmarshal([]byte(fixture), &raw); err != nil {
		t.Fatal(err)
	}
	policy := Policy{EnvAllow: map[string]bool{"TZ": true, "PASSWORD": true}, ArgAllow: map[string]bool{"port": true, "password": true}}
	s := startup(raw, time.Now(), policy)
	encoded, _ := json.Marshal(s)
	for _, secret := range []string{"secret-cli", "url-secret", "query-secret", "shell-secret", "api-secret", "env-secret", "token-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	if !strings.Contains(string(encoded), "443") || !strings.Contains(string(encoded), "Asia/Shanghai") || s.Incomplete || len(s.Config.Mounts) != 1 || !s.Config.Mounts[0].ReadOnly {
		t.Fatalf("nonsecret configuration lost: %s", encoded)
	}
	later := startup(raw, time.Now().Add(time.Hour), policy)
	if later.SnapshotID != s.SnapshotID {
		t.Fatal("unchanged configuration has unstable identity")
	}
	raw.Config.Image = "rtc/ap:2"
	if startup(raw, time.Now(), policy).SnapshotID == s.SnapshotID {
		t.Fatal("configuration change wasn't detected")
	}
	raw.ID = "recreated"
	if startup(raw, time.Now(), policy).SnapshotID == later.SnapshotID {
		t.Fatal("recreated container kept snapshot identity")
	}
}

func TestLifecycleAndDeclaredLimitsKeepUnknownDistinct(t *testing.T) {
	var raw inspect
	json.Unmarshal([]byte(fixture), &raw)
	if lifecycle(raw, time.Now()).CreatedAt != nil || lifecycle(raw, time.Now()).ExitCode != nil || lifecycle(raw, time.Now()).OOMKilled != nil {
		t.Fatal("missing creation/OOM or running exit code invented")
	}
	if limits(raw).CPUQuotaCores != nil || limits(raw).MemoryLimitBytes != nil {
		t.Fatal("unlimited container received a quota")
	}
	raw.Created = "2025-01-01T00:00:00Z"
	raw.HostConfig.NanoCpus = 100000000
	raw.HostConfig.Memory = 32 << 20
	oom := true
	raw.State.OOMKilled = &oom
	raw.State.Status = "exited"
	raw.State.ExitCode = 137
	raw.State.FinishedAt = "2026-01-02T00:00:00Z"
	raw.RestartCount = 3
	life := lifecycle(raw, time.Now())
	l := limits(raw)
	if life.CreatedAt == nil || life.FinishedAt == nil || life.StartedAt == nil || life.ExitCode == nil || *life.ExitCode != 137 || !*life.OOMKilled || life.RestartCount != 3 || *l.CPUQuotaCores != 0.1 || *l.MemoryLimitBytes != 32<<20 {
		t.Fatal("Docker lifecycle/limits lost")
	}
	raw.HostConfig.NanoCpus = 0
	raw.HostConfig.CpuQuota = 50000
	raw.HostConfig.CpuPeriod = 100000
	if *limits(raw).CPUQuotaCores != 0.5 {
		t.Fatal("CFS quota converted incorrectly")
	}
	if parsedTime("0001-01-01T00:00:00Z") != nil {
		t.Fatal("Docker zero timestamp shown as real time")
	}
}
func TestSafeTextURL(t *testing.T) {
	value := SafeText("https://alice:private@example.com/path?token=sensitive&public=hidden#fragment")
	for _, secret := range []string{"alice", "private", "sensitive", "hidden", "fragment"} {
		if strings.Contains(value, secret) {
			t.Fatal(value)
		}
	}
}
func TestOldDockerAndOversizedConfig(t *testing.T) {
	s := startup(inspect{ID: "a"}, time.Now(), Policy{})
	if !s.Incomplete {
		t.Fatal("missing fields claimed complete")
	}
	var raw inspect
	json.Unmarshal([]byte(fixture), &raw)
	raw.Config.Env = []string{"HUGE=" + strings.Repeat("x", 70<<10)}
	s = startup(raw, time.Now(), Policy{EnvAllow: map[string]bool{"HUGE": true}})
	if !s.Truncated || !s.Incomplete {
		t.Fatal("oversize not marked")
	}
	b, _ := json.Marshal(s)
	if len(b) > 65<<10 {
		t.Fatal("oversize snapshot retained")
	}
}
func TestDockerSocketDiscoveryIncludesStoppedAndFailsClosed(t *testing.T) {
	// Unix socket paths have a short OS limit; macOS testing directories exceed it.
	dir, err := os.MkdirTemp("/tmp", "avops-docker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("collector attempted mutation")
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/v1.39/containers/json":
			if r.URL.Query().Get("all") != "true" {
				t.Error("didn't include stopped containers")
			}
			w.Write([]byte(`[{"Id":"container-a"},{"Id":"container-b"}]`))
		case "/v1.39/containers/container-a/json":
			if fail.Load() {
				w.WriteHeader(404)
				return
			}
			w.Write([]byte(fixture))
		case "/v1.39/containers/container-b/json":
			w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(fixture, "container-a", "container-b"), "agora_local_ap", "stopped"), `"Status":"running"`, `"Status":"exited"`)))
		default:
			w.WriteHeader(404)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	collector := New(socket, Policy{})
	report := collector.Collect(context.Background(), model.Plan{})
	if !report.Complete || len(report.Containers) != 2 || report.Containers[1].Container.RuntimeStatus != "exited" {
		t.Fatalf("discovery: %+v", report)
	}
	fail.Store(true)
	report = collector.Collect(context.Background(), model.Plan{})
	if report.Complete {
		t.Fatal("partial inspect silently treated as authoritative full list")
	}
}
