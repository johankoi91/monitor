package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

func TestDiscoveryReportsChildProcessesAndRechecksPID(t *testing.T) {
	var changed atomic.Bool
	var raw inspect
	json.Unmarshal([]byte(`{"Id":"ap-id","Name":"/agora_local_ap","Path":"/usr/bin/start.sh","Config":{"Image":"fixture"},"HostConfig":{},"Mounts":[{"Source":"/logs","Destination":"/var/log/agora","Type":"bind"}],"State":{"Status":"running","Pid":100}}`), &raw)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"ApiVersion":"1.39","MinAPIVersion":"1.12"}`))
		case "/v1.39/containers/json":
			w.Write([]byte(`[{"Id":"ap-id"}]`))
		case "/v1.39/containers/ap-id/json":
			json.NewEncoder(w).Encode(raw)
		case "/v1.39/containers/ap-id/top":
			if r.URL.Query().Get("ps_args") != "-eo pid,comm" {
				t.Error("process arguments requested")
			}
			pid := "123"
			if changed.Load() {
				pid = "456"
			}
			json.NewEncoder(w).Encode(map[string]any{"Titles": []string{"PID", "COMMAND"}, "Processes": [][]string{{"100", "start.sh"}, {pid, "local_ap.exe"}, {"124", "tds.exe"}}})
		default:
			t.Errorf("unexpected Docker path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := versionTestCollector(server)
	r := c.Collect(context.Background(), model.Plan{})
	if !r.Complete || len(r.Containers) != 1 {
		t.Fatal(r)
	}
	container := r.Containers[0].Container
	if !container.ProcessesAvailable || len(container.Processes) != 3 || container.Processes[1].Name != "local_ap.exe" || container.Processes[1].PID != 123 {
		t.Fatal(container.Processes)
	}
	task := model.LogTask{ContainerID: raw.ID, SnapshotID: startup(raw, time.Now(), c.policy).SnapshotID, Source: "/logs", Services: []model.LogService{{ContainerID: raw.ID, ContainerName: "agora_local_ap", SnapshotID: container.SnapshotID, Processes: container.Processes}}}
	if err := c.VerifyLogSource(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	changed.Store(true)
	if err := c.VerifyLogSource(context.Background(), task); err == nil || !strings.Contains(err.Error(), "PID 已改变") {
		t.Fatalf("changed PID accepted: %v", err)
	}
	// A changed process must not change the startup configuration fingerprint.
	if got := c.Collect(context.Background(), model.Plan{}); got.Containers[0].Container.SnapshotID != container.SnapshotID || got.Containers[0].Container.Processes[1].PID != 124 {
		t.Fatal("process update corrupted configuration identity")
	}
}
