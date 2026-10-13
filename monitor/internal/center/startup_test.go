package center

import (
	"context"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"testing"
	"time"
)

func TestOnDemandStartupIsSessionBoundAndNotRetained(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap", "unmanaged")
	if _, err := s.Save(selection("", "rtc-a"), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NodeStartup(context.Background(), "rtc-a/unmanaged"); err == nil {
		t.Fatal("unmanaged startup exposed")
	}
	done := make(chan error, 1)
	go func() { _, err := s.NodeStartup(context.Background(), "rtc-a/ap"); done <- err }()
	var request model.StartupRequest
	deadline := time.Now().Add(time.Second)
	for request.RequestID == "" && time.Now().Before(deadline) {
		cmds := s.StartupCommands("rtc-a", session)
		if len(cmds) > 0 {
			request = cmds[0]
		}
		time.Sleep(time.Millisecond)
	}
	if request.RequestID == "" {
		t.Fatal("no WSS request queued")
	}
	result := model.StartupResult{Type: "startup_result", RequestID: request.RequestID, ContainerID: request.ContainerID, ContainerName: request.ContainerName, Startup: &model.Startup{CollectedAt: time.Now(), Config: model.StartupConfig{Cmd: []string{"private-runtime-value"}}}}
	s.ReceiveStartup("rtc-a", "old-session", result)
	select {
	case <-done:
		t.Fatal("old session supplied startup")
	default:
	}
	s.ReceiveStartup("rtc-a", session, result)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(s.startupRequests) != 0 {
		t.Fatal("transient request retained")
	}
	s.mu.RLock()
	for _, o := range s.agents["rtc-a"].Report.Containers {
		if o.Startup != nil {
			t.Fatal("parameters retained in state")
		}
	}
	s.mu.RUnlock()
	s.Disconnect("rtc-a", session)
	if _, err := s.NodeStartup(context.Background(), "rtc-a/ap"); err == nil {
		t.Fatal("offline request used stale cache")
	}
}

func TestCancelledStartupDoesNotLeaveQueue(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.RequestStartup(ctx, "rtc-a", "id-ap"); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	if len(s.startupRequests) != 0 {
		t.Fatal("cancelled request retained")
	}
}
