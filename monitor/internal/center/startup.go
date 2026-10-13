package center

import (
	"context"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

type pendingStartup struct {
	agent, session string
	request        model.StartupRequest
	sent           bool
	result         chan model.StartupResult
}

func (s *Store) RequestStartup(ctx context.Context, agent, cid string) (any, error) {
	s.mu.Lock()
	a := s.agents[agent]
	if a == nil || !a.Connected {
		s.mu.Unlock()
		return nil, problem(503, "AGENT_OFFLINE", "Agent不在线，无法读取启动参数")
	}
	name := ""
	for _, o := range a.Report.Containers {
		if o.Container.ContainerID == cid {
			name = o.Container.ContainerName
			break
		}
	}
	if name == "" {
		s.mu.Unlock()
		return nil, problem(404, "CONTAINER_NOT_FOUND", "容器不在最新发现清单")
	}
	if s.startupRequests == nil {
		s.startupRequests = map[string]*pendingStartup{}
	}
	if len(s.startupRequests) >= 32 {
		s.mu.Unlock()
		return nil, problem(429, "STARTUP_BUSY", "参数读取请求过多，请稍后重试")
	}
	id := ID()
	p := &pendingStartup{agent: agent, session: a.Session, request: model.StartupRequest{Type: "startup_request", RequestID: id, ContainerID: cid, ContainerName: name}, result: make(chan model.StartupResult, 1)}
	s.startupRequests[id] = p
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.startupRequests, id); s.mu.Unlock() }()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, problem(499, "REQUEST_CANCELLED", "请求已取消")
	case <-timer.C:
		return nil, problem(504, "STARTUP_TIMEOUT", "Agent未及时返回启动参数，请重试")
	case result := <-p.result:
		if result.Error != "" {
			return nil, problem(503, "STARTUP_READ_FAILED", "Agent读取失败："+result.Error)
		}
		return map[string]any{"startup": result.Startup, "stale": false}, nil
	}
}

func (s *Store) StartupCommands(agent, session string) []model.StartupRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := []model.StartupRequest{}
	for _, p := range s.startupRequests {
		if p.agent == agent && p.session == session && !p.sent {
			p.sent = true
			requests = append(requests, p.request)
		}
	}
	return requests
}
func (s *Store) ReceiveStartup(agent, session string, result model.StartupResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.startupRequests[result.RequestID]
	a := s.agents[agent]
	if p == nil || a == nil || !a.Connected || a.Session != session || p.agent != agent || p.session != session || p.request.ContainerID != result.ContainerID || p.request.ContainerName != result.ContainerName {
		return
	}
	if result.Error == "" && (result.Startup == nil || result.Startup.CollectedAt.Before(time.Now().Add(-10*time.Second)) || result.Startup.CollectedAt.After(time.Now().Add(5*time.Second))) {
		return
	}
	select {
	case p.result <- result:
	default:
	}
}
