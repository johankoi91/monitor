//go:build linux && agora

package agoranative

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/tests/testkit"
)

// Explicitly gated real SDK/WSS acceptance for the entire Native plan.
func TestNativeLiveFullCoverage(t *testing.T) {
	if os.Getenv("RTC_NATIVE_COVERAGE_QA") != "1" {
		t.Skip("explicit full Native acceptance required")
	}
	parameters := liveParameters(t)
	parameters.NativeCoverage = &protocol.NativeCoverageConfig{Groups: []protocol.APBinding{{Group: "172.16.80.25", EntryPoints: []string{parameters.AccessIPs[0]}}}, CrossEdge: true}
	root := t.TempDir()
	cfg := hub.Config{MaxConcurrentRuns: 2, Targets: []protocol.Target{{ID: "qa-native", Name: "isolated Native service", Driver: "agora", RTC: parameters}}, Tasks: []protocol.Task{protocol.Task{ID: "qa-full", TargetID: "qa-native", HostID: "host-qa-full", IntervalSeconds: 180, SweepIntervalSeconds: 900, CrossIntervalSeconds: 1800, TimeoutSeconds: 20, ServiceType: "rtc", ClientType: "native"}}}
	center, err := hub.New(filepath.Join(root, "center"), hub.Config{MaxConcurrentRuns: cfg.MaxConcurrentRuns}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inventory := protocol.NativeInventory{Groups: map[string][]protocol.NativeEdge{"172.16.80.25": {}}}
	for _, kind := range []string{"udp", "aut"} {
		for i := 1; i <= 3; i++ {
			port := 4000 + i
			if kind == "aut" {
				port = 4700 + i
			}
			inventory.Groups["172.16.80.25"] = append(inventory.Groups["172.16.80.25"], protocol.NativeEdge{Transport: kind, Endpoint: fmtEndpoint(port), AgentID: "qa-source", ContainerName: kind + string(rune('0'+i)), ContainerID: kind + string(rune('0'+i))})
		}
	}
	inventory.AccessPoints = []protocol.DiscoveredAccessPoint{{AgentID: "qa-source", Address: parameters.AccessIPs[0], Groups: []string{"172.16.80.25"}}}
	center.SetNativeInventoryProvider(func() (protocol.NativeInventory, error) { return inventory, nil })
	clientConfig, err := testkit.ConfigureClient(center, root, cfg, "agora")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); center.Run(ctx) }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if testkit.ServeEnrollment(center, w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/probes/") {
			center.ServeProbe(w, r)
		} else {
			center.ServeAdmin(w, r)
		}
	}))
	defer server.Close()
	clientConfig.URL = "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probes/connect"
	clientConfig.Capacity = 1
	clientConfig.RouteCapturePath = os.Getenv("RTC_NATIVE_CAPTURE_LIBRARY")
	clientConfig.WorkerCommand = func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeAcceptanceWorker$")
		cmd.Env = append(os.Environ(), "RTC_NATIVE_QA_WORKER=1")
		return cmd
	}
	client, err := probe.New(clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-clientDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("native client cleanup timed out")
		}
		<-done
	})
	if err = center.Enable("qa-full", true); err != nil {
		t.Fatal(err)
	}
	_, _ = center.Start("qa-full")
	deadline := time.Now().Add(7 * time.Minute)
	lastProgress := ""
	for time.Now().Before(deadline) {
		var snapshot struct {
			Health map[string]hub.TaskHealth   `json:"task_health"`
			Native map[string]*hub.NativeState `json:"native_coverage"`
			Runs   []hub.Run                   `json:"runs"`
		}
		body, _ := json.Marshal(center.Snapshot())
		if err = json.Unmarshal(body, &snapshot); err != nil {
			t.Fatal(err)
		}
		state := snapshot.Native["qa-full"]
		if state != nil {
			progress := ""
			complete := true
			for _, phase := range []string{"AP", "DIRECT", "CROSS"} {
				value := state.Phases[phase]
				if value == nil {
					complete = false
					continue
				}
				progress += phase + ":" + value.Health.State + ";"
				if value.Round == 0 || value.Health.State != "HEALTHY" {
					complete = false
				}
			}
			if progress != lastProgress {
				t.Log(progress)
				lastProgress = progress
			}
			if complete && snapshot.Health["qa-full"].State == "HEALTHY" {
				for _, phase := range []string{"AP", "DIRECT", "CROSS"} {
					value := state.Phases[phase]
					raw, err := center.Detail(value.Health.LastRunID)
					if err != nil {
						t.Fatal(err)
					}
					var run hub.Run
					if json.Unmarshal(raw, &run) != nil {
						t.Fatal("invalid native summary")
					}
					for _, row := range run.Cases {
						if row.Status != "SUCCESS" || row.SenderRoute == nil || row.ReceiverRoute == nil || !row.SenderRoute.Guarded || !row.ReceiverRoute.Guarded || row.SenderRoute.Status != "VERIFIED" || row.ReceiverRoute.Status != "VERIFIED" {
							t.Fatal("unverified actual route", row)
						}
					}
				}
				report := os.Getenv("RTC_NATIVE_QA_REPORT_DIR")
				if report != "" {
					if err = os.MkdirAll(report, 0700); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(filepath.Join(report, "native-full-coverage.json"), body, 0600); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("full Native coverage did not establish verified health")
}
func fmtEndpoint(port int) string {
	return os.Getenv("RTC_NATIVE_QA_ACCESS_IP") + ":" + strconv.Itoa(port)
}
