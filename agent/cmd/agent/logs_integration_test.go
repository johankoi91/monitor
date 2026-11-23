package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/agent/internal/agent"
	"github.com/johankoi91/monitor/runtime/agent/internal/docker"
	"github.com/johankoi91/monitor/runtime/center/frontend"
	"github.com/johankoi91/monitor/runtime/center/service"
	"github.com/johankoi91/monitor/runtime/center/testfixture"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

// Uses a fake Docker daemon but real Agent packing, WebSocket control, HTTP
// download relay and tar extraction. There are no business hosts involved.
func TestRealWireLogCollection(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "avops-logs-wire-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	hostRoot := filepath.Join(dir, "host")
	logDir := filepath.Join(hostRoot, "custom/agora")
	if err = os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	filename := "media_edge_1.log.DEBUG.20260522-03.123"
	content := "log content from the Agent host filesystem\n"
	if err = os.WriteFile(filepath.Join(logDir, filename), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AVOPS_HOST_ROOT", hostRoot)
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	dockerServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			io.WriteString(w, `{"ApiVersion":"1.39","MinAPIVersion":"1.12"}`)
		case "/v1.39/containers/json":
			io.WriteString(w, `[{"Id":"ap-id"},{"Id":"media-id"},{"Id":"media2-id"},{"Id":"unavailable-id"}]`)
		case "/v1.39/containers/ap-id/top":
			io.WriteString(w, `{"Titles":["PID","COMMAND"],"Processes":[["321","start.sh"],["333","local_ap.exe"]]}`)
		case "/v1.39/containers/media-id/top":
			io.WriteString(w, `{"Titles":["PID","COMMAND"],"Processes":[["123","media_edge_1"]]}`)
		case "/v1.39/containers/media2-id/top":
			io.WriteString(w, `{"Titles":["PID","COMMAND"],"Processes":[["124","media_edge_1"]]}`)
		case "/v1.39/containers/unavailable-id/top":
			io.WriteString(w, `{"Titles":["PID","COMMAND"],"Processes":[["456","media_edge_1"]]}`)
		case "/v1.39/info":
			io.WriteString(w, `{"Name":"fixture-host-with-a-long-machine-name-123456789"}`)
		case "/v1.39/containers/ap-id/json", "/v1.39/containers/media-id/json", "/v1.39/containers/media2-id/json", "/v1.39/containers/unavailable-id/json":
			id, name := "ap-id", "agora_local_ap"
			pid, executable := 321, "/usr/bin/start.sh"
			mounts := []map[string]any{{"Source": "/custom/agora", "Destination": "/var/log/agora", "Type": "bind", "RW": true}}
			if strings.Contains(r.URL.Path, "media-id") {
				id, name = "media-id", "media"
				pid, executable = 123, "/usr/bin/media_edge_1"
			} else if strings.Contains(r.URL.Path, "media2-id") {
				id, name = "media2-id", "media2"
				pid, executable = 124, "/usr/bin/media_edge_1"
			} else if strings.Contains(r.URL.Path, "unavailable-id") {
				id, name = "unavailable-id", "unavailable-service"
				pid, executable = 456, "/usr/bin/media_edge_1"
				mounts = nil
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": id, "Name": "/" + name, "Image": "sha256:fixture", "Config": map[string]any{"Image": "fixture/image:1"}, "HostConfig": map[string]any{"NetworkMode": "host"}, "Mounts": mounts, "Path": executable, "State": map[string]any{"Status": "running", "Pid": pid}})
		default:
			t.Errorf("unexpected Docker operation: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})}
	go dockerServer.Serve(listener)
	defer dockerServer.Close()
	centerDir, agentDir := filepath.Join(dir, "center"), filepath.Join(dir, "agent")
	store, err := center.NewStore(centerDir, center.Config{Site: model.Site{Code: "fixture", Name: "Fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.EnrollAgent("fixture-offline", strings.Repeat("o", 32), model.HostInfo{HostName: "fixture-offline", HostAddress: "127.0.0.2", IPAddresses: []string{"127.0.0.2"}, HostnameSource: "fixture", AddressSource: "fixture", CollectedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err = store.EnrollAgent("fixture", strings.Repeat("f", 32), model.HostInfo{HostName: "fixture-host", HostAddress: "127.0.0.1", IPAddresses: []string{"127.0.0.1"}, HostnameSource: "fixture", AddressSource: "fixture", CollectedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(frontend.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(testfixture.AccountAuth((&center.Server{Store: store, AdminID: "fixture", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler()))
	defer server.Close()
	collector := docker.New(socket, docker.Policy{RawStartup: true})
	runner, err := agent.New(agentDir, "fixture", nil, collector)
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
	done := make(chan struct{})
	go func() { defer close(done); run(ctx, conn, collector, runner, 100*time.Millisecond) }()
	defer func() { cancel(); conn.Close(); <-done }()
	deadline := time.Now().Add(15 * time.Second)
	for len(store.Containers()) != 4 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(store.Containers()) != 4 {
		t.Fatal("missing discovery")
	}
	for _, container := range store.Containers() {
		if container.ContainerName != "media" {
			continue
		}
		_, err = store.Save(model.Selection{Additions: []model.Addition{{AgentID: "fixture", ContainerID: container.ContainerID, SnapshotID: container.SnapshotID, ClusterCode: "fixture", ClusterName: "Fixture"}}, Removals: []string{}}, "test")
		if err != nil {
			t.Fatal(err)
		}
	}
	call := func(method, path string, body any, expected int) *http.Response {
		t.Helper()
		data, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.SetBasicAuth("fixture", strings.Repeat("z", 32))
		response, err := (&http.Client{Timeout: 20 * time.Second}).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != expected {
			message, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, message)
		}
		return response
	}
	request := model.LogRequest{NodeID: "fixture/media", Mode: "point", Time: "2026-05-22T03:45:00Z"}
	response := call("POST", "/api/v1/log-collections", request, 202)
	var job center.LogCollection
	json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if job.Source != "/custom/agora" || job.ContainerID != "ap-id" {
		t.Fatalf("did not use host's AP mount: %+v", job)
	}
	for time.Now().Before(deadline) {
		response = call("GET", "/api/v1/log-collections/"+job.ID, nil, 200)
		json.NewDecoder(response.Body).Decode(&job)
		response.Body.Close()
		if job.Status != "COLLECTING" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if job.Status != "READY" {
		t.Fatalf("not ready: %+v", job)
	}
	source, err := store.LogSource("fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	verifiedTask := model.LogTask{ContainerID: source.ContainerID, SnapshotID: source.SnapshotID, Source: source.Source}
	if err = collector.VerifyLogSource(context.Background(), verifiedTask); err != nil {
		t.Fatal(err)
	}
	for _, tamper := range []func(*model.LogTask){func(t *model.LogTask) { t.Source = "/etc" }, func(t *model.LogTask) { t.ContainerID = "media-id" }, func(t *model.LogTask) { t.SnapshotID = "changed" }} {
		changed := verifiedTask
		tamper(&changed)
		if collector.VerifyLogSource(context.Background(), changed) == nil {
			t.Fatal("Agent accepted a mismatched AP mount or identity")
		}
	}
	archives, err := filepath.Glob(filepath.Join(agentDir, "log-downloads", "*.tar.gz"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("archive not retained on Agent: %v %v", archives, err)
	}
	if err = filepath.WalkDir(centerDir, func(path string, entry fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".tar.gz") {
			t.Error("archive was stored at center")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	response = call("GET", "/api/v1/log-collections/"+job.ID+"/download", nil, 200)
	gz, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil || !strings.HasSuffix(header.Name, "/"+filename) {
		t.Fatalf("bad archive header: %+v %v", header, err)
	}
	data, err := io.ReadAll(tr)
	if err != nil || string(data) != content {
		t.Fatalf("wrong downloaded content: %q %v", data, err)
	}
	if _, err = tr.Next(); err != io.EOF {
		t.Fatal("unexpected archive entries", err)
	}
	// Read to the gzip trailer and HTTP EOF so the relay validates completion.
	if _, err = io.Copy(io.Discard, gz); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	response.Body.Close()
	if err = os.WriteFile(filepath.Join(logDir, "media_edge_1.log.DEBUG.20260522-03.456"), []byte("LOG_FROM_OTHER_CONTAINER_MUST_BE_EXCLUDED"), 0600); err != nil {
		t.Fatal(err)
	}
	secondFilename := "media_edge_1.log.DEBUG.20260522-03.124"
	if err = os.WriteFile(filepath.Join(logDir, secondFilename), []byte("log from second selected container"), 0600); err != nil {
		t.Fatal(err)
	}
	request.ServiceContainerIDs = []string{"media-id", "media2-id", "media-id"}
	response = call("POST", "/api/v1/log-collections", request, 202)
	job = center.LogCollection{}
	json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if len(job.Services) != 2 {
		t.Fatalf("selection was lost or not deduplicated: %+v", job.Services)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response = call("GET", "/api/v1/log-collections/"+job.ID, nil, 200)
		json.NewDecoder(response.Body).Decode(&job)
		response.Body.Close()
		if job.Status != "COLLECTING" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if job.Status != "READY" || job.FileCount != 2 || !strings.HasSuffix(job.Filename, "_2-services.tar.gz") {
		t.Fatalf("multi-container collection failed: %+v", job)
	}
	response = call("GET", "/api/v1/log-collections/"+job.ID+"/download", nil, 200)
	gz, err = gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr = tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[filepath.Base(header.Name)] = true
	}
	if len(seen) != 2 || !seen[filename] || !seen[secondFilename] {
		t.Fatalf("multi-container archive included the wrong files: %v", seen)
	}
	if _, err = io.Copy(io.Discard, gz); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	response.Body.Close()
	// Verify every selected member, including members after the first.
	verifiedTask.Services = job.Services
	if err = collector.VerifyLogSource(context.Background(), verifiedTask); err != nil {
		t.Fatal(err)
	}
	verifiedTask.Services[1].Processes[0].PID = 999
	if err = collector.VerifyLogSource(context.Background(), verifiedTask); err == nil {
		t.Fatal("Agent accepted changed PID in the second selected container")
	}
	if os.Getenv("AVOPS_BROWSER_TEST") == "1" {
		script, _ := filepath.Abs("../../../center/tests/log-collection.cjs")
		cmd := exec.Command("node", script)
		cmd.Env = append(os.Environ(), "AVOPS_UI_URL="+server.URL, "AVOPS_ISOLATED_UI_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v\n%s", err, output)
		} else {
			t.Log(string(output))
		}
	}
}
