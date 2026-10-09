// Local browser fixture. All containers and execution evidence are simulated.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/access"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"github.com/johankoi91/monitor/runtime/web"
)

func main() {
	statusDemo := os.Getenv("AVOPS_UI_DEMO_BASELINE") == "1"
	dir, err := os.MkdirTemp("/tmp", "avops-react-qa-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	store, err := center.NewStore(dir, center.Config{Site: model.Site{Code: "ui-fixture", Name: "React 界面模拟验证"}, Agents: []center.AgentConfig{{ID: "ui-fixture", Secret: strings.Repeat("f", 32), HostName: "界面模拟主机", HostAddress: "127.0.0.1"}}})
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	notifier, err := notify.New(dir, notify.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer notifier.Close()
	keys, err := access.New(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer keys.Close()
	session := store.Connect("ui-fixture")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		var sequence uint64
		for ctx.Err() == nil {
			sequence++
			at := time.Now().UTC()
			r := model.Report{Type: "snapshot", Complete: true, Sequence: sequence, CollectedAt: at, BaselineRevision: store.Baseline().Revision, Containers: []model.Observation{}}
			for i := 0; i < 55; i++ {
				name := "unused-" + strings.Repeat("x", i+1)
				if i == 0 {
					name = "agora_local_ap"
					if statusDemo {
						name = "agora_cap_sync"
					}
				}
				hash := sha256.Sum256([]byte(name))
				id := hex.EncodeToString(hash[:])
				r.Containers = append(r.Containers, model.Observation{Container: model.Container{ContainerID: id, ContainerName: name, SnapshotID: id, Image: "fixture/image:1", RuntimeStatus: "running", CollectedAt: at}, Startup: &model.Startup{SnapshotID: id, CollectedAt: at, Config: model.StartupConfig{Image: "fixture/image:1", Env: []model.Env{}}}})
				cpu, rx, tx, window := 0.25, 1024.0, 2048.0, 5.0
				memory, disk := uint64(32<<20), uint64(4<<20)
				r.Containers[i].Container.Resources = &model.ResourceMetrics{Source: "cadvisor", Status: "AVAILABLE", ReasonCode: "OK", SampledAt: &at, FetchedAt: at, CPUUsageCores: &cpu, MemoryUsageBytes: &memory, MemoryWorkingSetBytes: &memory, FilesystemUsageBytes: &disk, NetworkScope: "CONTAINER", NetworkReceiveBPS: &rx, NetworkTransmitBPS: &tx, SampleWindowSeconds: &window}
				if i == 0 {
					r.Containers[i].Container.Resources.NetworkScope = "HOST_SHARED"
					r.Containers[i].Container.Resources.NetworkReceiveBPS = nil
					r.Containers[i].Container.Resources.NetworkTransmitBPS = nil
				}
				m := r.Containers[i].Container.Resources
				limit := uint64(64 << 20)
				quota, ratio, throttled := 0.5, 0.5, 0.1
				zero := uint64(0)
				total, free := uint64(1000000), uint64(900000)
				inodeRatio := 0.1
				m.LimitsKnown = true
				m.MemoryLimitBytes = &limit
				m.MemoryWorkingSetRatio = &ratio
				m.CPUQuotaCores = &quota
				m.CPUThrottledRatio = &throttled
				m.MemoryRSSBytes = &memory
				m.MemoryCacheBytes = &zero
				m.MemorySwapBytes = &zero
				m.MemoryPeakBytes = &memory
				m.MemoryFailuresTotal = &zero
				m.OOMEventsTotal = &zero
				m.OOMObservedAt = &at
				m.HostFilesystem = &model.HostFilesystem{Scope: "HOST_DOCKER_STORAGE", Source: "agent_statfs", CollectedAt: at, CapacityBytes: 100 << 30, FreeBytes: 50 << 30, AvailableBytes: 49 << 30, UsedRatio: 0.5, InodesTotal: &total, InodesFree: &free, InodesUsedRatio: &inodeRatio}
				m.DiskIOStatus = "AVAILABLE"
				m.DiskIOScope = "PER_DEVICE"
				m.DiskDevices = []model.DiskDeviceMetrics{{Device: "/dev/dm-0", Major: 253, Minor: 0, ReadBPS: &rx, WriteBPS: &tx, ReadOpsPerSecond: &quota, WriteOpsPerSecond: &quota}, {Device: "/dev/vdb", Major: 252, Minor: 16, ReadBPS: &rx, WriteBPS: &tx}}
				created, started := at.Add(-48*time.Hour), at.Add(-time.Hour)
				oom := false
				r.Containers[i].Container.Lifecycle = &model.ContainerLifecycle{CreatedAt: &created, StartedAt: &started, LastObservedAt: at, RestartCount: 2, OOMKilled: &oom, DockerHealth: "healthy"}
				if statusDemo && i == 0 {
					cpu = 0.0003
					memory = 8 << 20
					disk = 520 << 10
					m.MemoryLimitBytes = nil
					m.MemoryWorkingSetRatio = nil
					m.CPUQuotaCores = nil
					m.CPUThrottledRatio = nil
					m.DiskIOStatus = "NO_VALID_SAMPLES"
					m.DiskDevices = nil
					m.DiskIOScope = ""
					m.MemoryPeakBytes = &memory
					r.Containers[i].Container.Lifecycle.RestartCount = 0
					r.Containers[i].Container.Lifecycle.DockerHealth = ""
					r.Containers[i].Container.Lifecycle.StartedAt = nil
					start := at.Add(-94 * time.Minute)
					r.Containers[i].Container.Lifecycle.StartedAt = &start
				}
			}
			store.Receive("ui-fixture", session, r)
			if statusDemo && !store.Baseline().Active {
				c := r.Containers[0].Container
				if _, err := store.Save(model.Selection{Additions: []model.Addition{{AgentID: "ui-fixture", ContainerID: c.ContainerID, SnapshotID: c.SnapshotID, ClusterCode: "ui-fixture", ClusterName: "UI 演示", ServiceCode: "rtc-ap", ServiceName: "RTC 服务"}}, Removals: []string{}}, "fixture"); err != nil {
					log.Fatal(err)
				}
			}
			store.TickOperations(at)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	assets, _ := fs.Sub(web.Files, "static")
	handler := (&center.Server{Store: store, AdminID: "fixture", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets)), Notifier: notifier, Access: keys}).Handler()
	if os.Getenv("AVOPS_UI_ACCOUNT_FIXTURE") == "1" {
		handler = accountFixture(handler)
	}
	server := &http.Server{Addr: "127.0.0.1:18088", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); server.Close() }()
	log.Print("React fixture on http://127.0.0.1:18088; data simulated, no Docker access")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
