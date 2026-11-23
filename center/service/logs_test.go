package center

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

func logStore(t *testing.T) (*Store, string) {
	t.Helper()
	s := setup(t)
	session := s.Connect("rtc-a")
	r := report("agora_local_ap", "media")
	r.Containers[0].Startup.Config.Mounts = []model.Mount{{Source: "/srv/agora", Target: "/var/log/agora", Type: "bind"}}
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	return s, session
}

func TestLogCollectionIsolationSequenceAndExpiry(t *testing.T) {
	s, session := logStore(t)
	request := model.LogRequest{AgentID: "rtc-a", Mode: "day", Time: "20260522"}
	job, err := s.CreateLogCollection(request)
	if err != nil || job.Source != "/srv/agora" {
		t.Fatalf("%+v %v", job, err)
	}
	if _, err := s.CreateLogCollection(request); err == nil {
		t.Fatal("parallel host collection allowed")
	}
	if len(s.logCommands("rtc-b", session)) != 0 || len(s.logCommands("rtc-a", "wrong")) != 0 {
		t.Fatal("command crossed Agent/session")
	}
	if len(s.logCommands("rtc-a", session)) != 1 || len(s.logCommands("rtc-a", session)) != 0 {
		t.Fatal("task must be sent exactly once")
	}
	result := model.LogResult{Type: "log_result", ID: job.ID, Label: "agora_logs_20260522T000000Z", Bytes: 7, FileCount: 1}
	s.receiveLogResult("rtc-b", session, result)
	s.receiveLogResult("rtc-a", "old-session", result)
	view, _ := s.logCollection(job.ID)
	if view.Bytes != 0 {
		t.Fatal("unbound result accepted")
	}
	s.receiveLogResult("rtc-a", session, result)
	view, err = s.logCollection(job.ID)
	if err != nil || view.Status != "READY" || view.Bytes != 7 {
		t.Fatalf("%+v %v", view, err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "log-downloads" {
			t.Fatal("center stored archive")
		}
	}
	_, transfer, err := s.beginLogDownload(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.logCommands("rtc-a", session)) != 1 {
		t.Fatal("download instruction missing")
	}
	chunk := model.LogChunk{ID: job.ID, TransferID: transfer.TransferID, Data: []byte("archive")}
	s.receiveLogChunk("rtc-a", "wrong", chunk)
	select {
	case <-transfer.chunks:
		t.Fatal("unbound chunk accepted")
	default:
	}
	s.receiveLogChunk("rtc-a", session, chunk)
	if received := <-transfer.chunks; string(received.Data) != "archive" {
		t.Fatal("relay changed data")
	}
	s.receiveLogChunk("rtc-a", session, model.LogChunk{ID: job.ID, TransferID: transfer.TransferID, Sequence: 1, Done: true})
	if received := <-transfer.chunks; !received.Done || received.Error != "" {
		t.Fatal(received)
	}
	s.endLogDownload(job.ID, transfer)
	_, transfer, err = s.beginLogDownload(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.logCommands("rtc-a", session)
	s.receiveLogChunk("rtc-a", session, model.LogChunk{ID: job.ID, TransferID: transfer.TransferID, Sequence: 1})
	if received := <-transfer.chunks; received.Error == "" {
		t.Fatal("sequence violation accepted")
	}
	s.endLogDownload(job.ID, transfer)
	s.mu.Lock()
	s.cleanupLogsLocked(time.Now().Add(2 * time.Hour))
	s.mu.Unlock()
	if _, err := s.logCollection(job.ID); err == nil {
		t.Fatal("expired job retained")
	}
	job, err = s.CreateLogCollection(request)
	if err != nil {
		t.Fatal(err)
	}
	s.Disconnect("rtc-a", session)
	view, _ = s.logCollection(job.ID)
	if view.Status != "FAILED" {
		t.Fatal("disconnected task did not fail")
	}

}

func TestLogSourceRequiresCurrentAPMount(t *testing.T) {
	s, _ := logStore(t)
	s.lastStartups["rtc-a"]["agora_local_ap"].Startup.SnapshotID = "changed"
	_, err := s.LogSource("rtc-a", "")
	code(t, err, "LOG_STARTUP_UNAVAILABLE")
	s.lastStartups["rtc-a"]["agora_local_ap"].Startup.SnapshotID = "snapshot-agora_local_ap"
	s.lastStartups["rtc-a"]["agora_local_ap"].Startup.Config.Mounts = nil
	_, err = s.LogSource("rtc-a", "")
	code(t, err, "LOG_MOUNT_UNAVAILABLE")
	_, err = s.CreateLogCollection(model.LogRequest{AgentID: "rtc-a", Mode: "day", Time: "20260229"})
	code(t, err, "INVALID_LOG_TIME")
}

func TestLogServiceUsesReportedProcessesOnTheSelectedAgent(t *testing.T) {
	s, session := logStore(t)
	r := report("agora_local_ap", "agora_udp_media_edge_1", "agora_udp_media_edge_2", "no-log-mount")
	r.Sequence = 2
	for i := range r.Containers {
		r.Containers[i].Startup.Config.Mounts = []model.Mount{{Source: "/srv/agora", Target: "/var/log/agora", Type: "bind"}}
		r.Containers[i].Container.ProcessesAvailable = true
		r.Containers[i].Container.Processes = []model.ContainerProcess{{PID: 100 + i, Name: "media_edge_1"}}
	}
	r.Containers[3].Startup.Config.Mounts = nil
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	source, err := s.LogSource("rtc-a", "")
	if err != nil || len(source.Services) != 4 || !source.Services[1].Available || source.Services[3].Available {
		t.Fatalf("%+v %v", source, err)
	}
	request := model.LogRequest{AgentID: "rtc-a", Mode: "day", Time: "20261117"}
	// Every selected container must be valid; never silently widen or partially
	// collect a request with an unavailable or cross-host member.
	request.ServiceContainerIDs = []string{"id-agora_udp_media_edge_1", "different-agent-container"}
	_, err = s.CreateLogCollection(request)
	code(t, err, "LOG_SERVICE_NOT_FOUND")
	request.ServiceContainerIDs[1] = "id-no-log-mount"
	_, err = s.CreateLogCollection(request)
	code(t, err, "LOG_SERVICE_UNAVAILABLE")
	if len(s.logJobs) != 0 {
		t.Fatal("invalid selection created a partial job")
	}
	request.ServiceContainerIDs = []string{"id-agora_udp_media_edge_1", "id-agora_udp_media_edge_2", "id-agora_udp_media_edge_1"}
	job, err := s.CreateLogCollection(request)
	if err != nil || len(job.Services) != 2 || len(job.task.Services) != 2 || job.Services[0].Processes[0].PID != 101 || job.task.Services[1].ContainerID != "id-agora_udp_media_edge_2" {
		t.Fatalf("%+v %v", job, err)
	}
	s.mu.Lock()
	s.logJobs[job.ID].Status = "FAILED"
	s.mu.Unlock()
	r.Sequence = 3
	r.Containers[1].Container.Processes[0].PID = 999
	if err = s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	request.ServiceContainerIDs = []string{"id-agora_udp_media_edge_1"}
	job, err = s.CreateLogCollection(request)
	if err != nil || job.Services[0].Processes[0].PID != 999 {
		t.Fatalf("old PID retained: %+v %v", job, err)
	}
}
