package cadvisor

import (
	"context"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func observation(id, network string) model.Observation {
	return model.Observation{Container: model.Container{ContainerID: id, RuntimeStatus: "running"}, StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano), Startup: &model.Startup{Config: model.StartupConfig{NetworkMode: network}}}
}
func sample(at time.Time, cpu, rx, tx uint64) map[string]any {
	return map[string]any{"timestamp": at, "cpu": map[string]any{"usage": map[string]any{"total": cpu}}, "memory": map[string]any{"usage": 2048, "working_set": 1024}, "network": map[string]any{"interfaces": []map[string]any{{"name": "eth0", "rx_bytes": rx, "tx_bytes": tx}}}, "filesystem": []map[string]any{{"device": "disk", "usage": 4096}}, "labels": map[string]string{"secret": "must-not-be-forwarded"}}
}
func TestExactIdentityRatesAndHostNetwork(t *testing.T) {
	id := strings.Repeat("a", 64)
	now := time.Now().UTC().Add(-time.Second)
	data := map[string]any{"/docker/" + id: []any{sample(now.Add(-2*time.Second), 1e9, 100, 200), sample(now, 2e9, 300, 600)}, "/docker/" + strings.Repeat("b", 64): []any{sample(now, 999, 999, 999)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.Write([]byte(`container_oom_events_total{id="/docker/` + id + `"} 0`))
			return
		}
		if r.URL.Path != "/api/v2.0/stats/" || r.URL.Query().Get("count") != "2" || r.URL.Query().Get("recursive") != "true" || r.Header.Get("Authorization") != "" {
			t.Error("wrong/read-credential-bearing request")
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()
	c, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := model.Report{Complete: true, Containers: []model.Observation{observation(id, "bridge"), observation(strings.Repeat("c", 64), "bridge")}}
	c.Enrich(context.Background(), &r)
	m := r.Containers[0].Container.Resources
	if m.Status != "AVAILABLE" || m.Stale || m.CPUUsageCores == nil || *m.CPUUsageCores != 0.5 || *m.NetworkReceiveBPS != 100 || *m.NetworkTransmitBPS != 200 || *m.MemoryWorkingSetBytes != 1024 || *m.FilesystemUsageBytes != 4096 {
		t.Fatalf("wrong rates: %+v", m)
	}
	if r.Containers[1].Container.Resources.ReasonCode != "CADVISOR_CONTAINER_NOT_FOUND" {
		t.Fatal("name/partial ID matched another container")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "must-not-be-forwarded") {
		t.Fatal("raw cAdvisor labels forwarded")
	}
	r.Containers[0].Startup.Config.NetworkMode = "host"
	c.Enrich(context.Background(), &r)
	m = r.Containers[0].Container.Resources
	if m.NetworkScope != "HOST_SHARED" || m.NetworkReceiveBPS != nil || m.NetworkTransmitBPS != nil {
		t.Fatal("host traffic attributed to container")
	}
}

func TestExtendedRatesLimitsAndFilesystem(t *testing.T) {
	id := strings.Repeat("a", 64)
	now := time.Now().UTC().Add(-time.Second)
	before, after := sample(now.Add(-2*time.Second), 1e9, 100, 200), sample(now, 2e9, 300, 600)
	for i, s := range []map[string]any{before, after} {
		s["cpu"] = map[string]any{"usage": map[string]any{"total": uint64(1e9 + i*1e9), "user": uint64(1e8 + i*2e8), "system": uint64(2e8 + i*4e8)}, "cfs": map[string]any{"periods": 100 + i*100, "throttled_periods": 20 + i*25, "throttled_time": uint64(i * 2e8)}}
		s["network"] = map[string]any{"interfaces": []map[string]any{{"name": "eth0", "rx_bytes": 100 + i*200, "tx_bytes": 200 + i*400, "rx_packets": 100 + i*20, "tx_packets": 200 + i*40, "rx_dropped": 3 + i*4, "tx_dropped": 2 + i*2, "rx_errors": 5, "tx_errors": 1 + i*2}}}
		s["has_diskio"] = true
		s["diskio"] = map[string]any{"io_service_bytes": []map[string]any{{"major": 8, "minor": 0, "stats": map[string]any{"Read": 100 + i*1024, "Write": 200 + i*2048, "Total": 300 + i*3072}}}, "io_serviced": []map[string]any{{"major": 8, "minor": 0, "stats": map[string]any{"Read": 10 + i*2, "Write": 20 + i*4}}}}
		s["memory"] = map[string]any{"usage": 2048, "working_set": 1024, "rss": 512, "cache": 128, "swap": 0, "max_usage": 4096, "failcnt": 7}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.Write([]byte(`container_oom_events_total{id="/docker/` + id + `"} 2`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"/docker/" + id: []any{before, after}})
	}))
	defer server.Close()
	c, _ := New(server.URL)
	c.FilesystemRoot = t.TempDir()
	limit, quota := uint64(2048), 0.5
	o := observation(id, "bridge")
	o.Container.Limits = &model.ResourceLimits{MemoryLimitBytes: &limit, CPUQuotaCores: &quota}
	r := model.Report{Complete: true, Containers: []model.Observation{o}}
	c.Enrich(context.Background(), &r)
	m := r.Containers[0].Container.Resources
	if m.CPUUserUsageCores == nil || *m.CPUUserUsageCores != 0.1 || *m.CPUSystemUsageCores != 0.2 || *m.CPUThrottledRatio != 0.25 || *m.MemoryWorkingSetRatio != 0.5 || *m.MemoryFailuresTotal != 7 || *m.OOMEventsTotal != 2 || m.OOMObservedAt == nil {
		t.Fatalf("extended resource values incorrect: %+v", m)
	}
	if m.DiskIOStatus != "AVAILABLE" || *m.DiskReadBPS != 512 || *m.DiskWriteBPS != 1024 || *m.DiskReadOpsPerSecond != 1 || *m.DiskWriteOpsPerSecond != 2 || *m.NetworkReceiveDroppedPerSecond != 2 || *m.NetworkReceiveErrorsPerSecond != 0 {
		t.Fatalf("I/O or network rates incorrect: %+v", m)
	}
	if m.HostFilesystem == nil || m.HostFilesystem.Source != "agent_statfs" || m.HostFilesystem.CapacityBytes == 0 || !Valid(m) {
		t.Fatal("real filesystem counters invalid")
	}
	r.Containers[0].Container.Limits = &model.ResourceLimits{}
	r.Containers[0].Startup.Config.NetworkMode = "host"
	c.Enrich(context.Background(), &r)
	m = r.Containers[0].Container.Resources
	if m.CPUThrottledRatio != nil || m.MemoryWorkingSetRatio != nil || m.NetworkReceiveDroppedPerSecond != nil || m.NetworkTransmitPacketsPerSecond != nil {
		t.Fatal("unlimited quota or host shared network invented ratios")
	}
}

func TestOOMCounterParsingAndIOResets(t *testing.T) {
	id := strings.Repeat("d", 64)
	counts := parseOOM([]byte("# HELP container_oom_events_total no value\n" + `container_oom_events_total{id="/docker/` + id + `",name="test"} 3` + "\n" + `other_metric{id="/docker/` + id + `"} 99`))
	if counts[id] != 3 {
		t.Fatal("OOM counter not parsed by exact ID")
	}
	for _, value := range []string{"NaN", "+Inf", "-1", "0.5", "1e30"} {
		if len(parseOOM([]byte(`container_oom_events_total{id="/docker/`+id+`"} `+value))) != 0 {
			t.Fatal("invalid OOM counter accepted")
		}
	}
	if len(parseOOM([]byte(`container_oom_events_total{id="/docker/`+id+`"} 1`+"\n"+`container_oom_events_total{id="/docker/`+id+`"} 2`))) != 0 {
		t.Fatal("ambiguous OOM counters accepted")
	}
	a, b := uint64(100), uint64(50)
	before := []deviceStats{{Major: 8, Minor: 0, Stats: map[string]*uint64{"Read": &a, "Write": &a}}}
	after := []deviceStats{{Major: 8, Minor: 0, Stats: map[string]*uint64{"Read": &b, "Write": &a}}}
	read, write := deviceRates(before, after, 2)
	if read != nil || write != nil {
		t.Fatal("I/O reset became a rate")
	}
	if hostFilesystem(t.TempDir()+"/missing", time.Now()) != nil {
		t.Fatal("missing filesystem invented capacity")
	}
	c, d := uint64(200), uint64(100)
	before = []deviceStats{{Device: "/dev/dm-0", Major: 253, Minor: 0, Stats: map[string]*uint64{"Read": &b, "Write": &b}}, {Device: "/dev/vdb", Major: 252, Minor: 16, Stats: map[string]*uint64{"Read": &b, "Write": &b}}}
	after = []deviceStats{{Device: "/dev/dm-0", Major: 253, Minor: 0, Stats: map[string]*uint64{"Read": &c, "Write": &d}}, {Device: "/dev/vdb", Major: 252, Minor: 16, Stats: map[string]*uint64{"Read": &c, "Write": &d}}}
	read, write = deviceRates(before, after, 2)
	if read != nil || write != nil {
		t.Fatal("mapped and physical device I/O added twice")
	}
	devices := diskDevices(before, after, nil, nil, 2)
	if len(devices) != 2 || *devices[0].ReadBPS != 75 || *devices[1].ReadBPS != 75 {
		t.Fatal("device-level I/O missing")
	}
}
func TestFailureStaleResetAndStoppedNeverInventZero(t *testing.T) {
	id := strings.Repeat("a", 64)
	now := time.Now().UTC().Add(-time.Second)
	data := map[string]any{"/docker/" + id: []any{sample(now.Add(-2*time.Second), 2e9, 300, 600), sample(now, 1e9, 100, 200)}}
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); json.NewEncoder(w).Encode(data) }))
	defer server.Close()
	c, _ := New(server.URL)
	r := model.Report{Complete: true, Containers: []model.Observation{observation(id, "bridge")}}
	c.Enrich(context.Background(), &r)
	m := r.Containers[0].Container.Resources
	if m.CPUUsageCores != nil || m.NetworkReceiveBPS != nil || m.Status != "PARTIAL" {
		t.Fatal("reset converted into a rate")
	}
	data["/docker/"+id] = []any{sample(now.Add(-time.Minute), 1, 1, 1)}
	c.Enrich(context.Background(), &r)
	m = r.Containers[0].Container.Resources
	if !m.Stale || m.MemoryUsageBytes != nil || m.ReasonCode != "CADVISOR_SAMPLE_STALE" {
		t.Fatal("old samples passed as current")
	}
	status = 503
	c.Enrich(context.Background(), &r)
	if !r.Complete || r.Containers[0].Container.Resources.ReasonCode != "CADVISOR_HTTP_ERROR" {
		t.Fatal("cAdvisor failure corrupted Docker completeness")
	}
	r.Containers[0].Container.RuntimeStatus = "exited"
	status = 200
	c.Enrich(context.Background(), &r)
	if r.Containers[0].Container.Resources.ReasonCode != "CONTAINER_NOT_RUNNING" {
		t.Fatal("stopped container showed old metrics")
	}
	status = 200
	data["/docker/"+id] = []any{sample(now.Add(-2*time.Second), 1, 1, 1), sample(now, 2, 2, 2)}
	r.Containers[0] = observation(id, "bridge")
	r.Containers[0].StartedAt = now.Add(-time.Second).Format(time.RFC3339Nano)
	c.Enrich(context.Background(), &r)
	if r.Containers[0].Container.Resources.CPUUsageCores != nil {
		t.Fatal("rate crossed a container restart")
	}
}
func TestOnlyLoopbackNoRedirects(t *testing.T) {
	for _, endpoint := range []string{"http://example.com:8080", "http://192.168.1.1:8080", "http://user:pass@127.0.0.1:8080", "https://127.0.0.1:8080", "http://127.0.0.1:8080/metrics", "http://127.0.0.1:8080?x=1"} {
		if _, err := New(endpoint); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:18089", "http://[::1]:18089", "http://localhost:18089", ""} {
		if _, err := New(endpoint); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com", 302) }))
	defer server.Close()
	c, _ := New(server.URL)
	_, reason := c.fetch(context.Background())
	if reason != "CADVISOR_HTTP_ERROR" {
		t.Fatal("redirect followed")
	}
}
