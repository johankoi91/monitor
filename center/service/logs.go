package center

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/agoralogs"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

type LogSource struct {
	AgentID     string `json:"agent_id"`
	ContainerID string `json:"container_id"`
	SnapshotID  string `json:"snapshot_id"`
	Source      string `json:"source"`
}
type LogSourceView struct {
	LogSource
	Services []model.LogService `json:"services"`
}
type LogCollection struct {
	Services []model.LogService `json:"services,omitempty"`
	ID       string             `json:"id"`
	LogSource
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Filename  string    `json:"filename"`
	Mode      string    `json:"mode"`
	Time      string    `json:"time"`
	Bytes     int64     `json:"bytes"`
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	task      model.LogTask
	session   string
	sent      bool
	download  *logDownload
	cancels   []model.LogDownloadTask
}

type logDownload struct {
	model.LogDownloadTask
	ctx      context.Context
	cancel   context.CancelFunc
	chunks   chan model.LogChunk
	sent     bool
	sequence int
	bytes    int64
	finished bool
}

func (s *Store) logSourceLocked(agentID, nodeID string) (LogSource, error) {
	if nodeID != "" {
		node, _, _ := s.findNodeLocked(nodeID)
		if node.AgentID == "" {
			return LogSource{}, problem(404, "NODE_NOT_FOUND", "监控容器已不存在，请刷新")
		}
		if agentID != "" && agentID != node.AgentID {
			return LogSource{}, problem(400, "INVALID_TARGET", "主机和容器不匹配")
		}
		agentID = node.AgentID
	}
	a := s.agents[agentID]
	if a == nil || !fresh(a, time.Now()) {
		return LogSource{}, problem(409, "AGENT_OFFLINE", "Agent 离线或采样已过期，暂不能拉取日志")
	}
	for _, observed := range a.Report.Containers {
		if observed.Container.ContainerName != "agora_local_ap" {
			continue
		}
		r := s.lastStartups[agentID]["agora_local_ap"]
		if r.Startup == nil || r.Startup.Incomplete || r.Startup.Truncated || r.ContainerID != observed.Container.ContainerID || r.Startup.SnapshotID != observed.Container.SnapshotID {
			return LogSource{}, problem(409, "LOG_STARTUP_UNAVAILABLE", "等待 agora_local_ap 上报完整启动参数后重试")
		}
		source, err := agoralogs.Source(r.Startup.Config)
		if err != nil {
			return LogSource{}, problem(409, "LOG_MOUNT_UNAVAILABLE", err.Error())
		}
		return LogSource{AgentID: agentID, ContainerID: r.ContainerID, SnapshotID: r.Startup.SnapshotID, Source: source}, nil
	}
	return LogSource{}, problem(409, "AP_NOT_FOUND", "该 Agent 未发现 agora_local_ap 容器，无法识别日志目录")
}

func (s *Store) logServicesLocked(source LogSource) []model.LogService {
	services := []model.LogService{}
	for _, observed := range s.agents[source.AgentID].Report.Containers {
		c := observed.Container
		service := model.LogService{ContainerID: c.ContainerID, ContainerName: c.ContainerName, SnapshotID: c.SnapshotID, Processes: clone(c.Processes)}
		r := s.lastStartups[source.AgentID][c.ContainerName]
		switch {
		case r.Startup == nil || r.Startup.Incomplete || r.Startup.Truncated || r.ContainerID != c.ContainerID || r.Startup.SnapshotID != c.SnapshotID:
			service.Reason = "等待 Agent 上报启动参数"
		case c.RuntimeStatus != "running" || !c.ProcessesAvailable || !model.ValidProcesses(c.Processes):
			service.Reason = "容器未运行或尚未上报进程 PID"
		default:
			path, err := agoralogs.Source(r.Startup.Config)
			if err != nil || path != source.Source {
				service.Reason = "容器未挂载该机器的日志目录"
			} else {
				service.Available = true
			}
		}
		services = append(services, service)
	}
	return services
}

func (s *Store) LogSource(agentID, nodeID string) (LogSourceView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, err := s.logSourceLocked(agentID, nodeID)
	if err != nil {
		return LogSourceView{}, err
	}
	return LogSourceView{LogSource: source, Services: s.logServicesLocked(source)}, nil
}

func (s *Store) CreateLogCollection(request model.LogRequest) (LogCollection, error) {
	// Validate the format here; Agent interprets unzoned input in its own local
	// timezone, matching the script. The final UTC label comes from Agent.
	_, label, err := agoralogs.Buckets(request.Mode, request.Time, time.UTC)
	if err != nil {
		return LogCollection{}, problem(400, "INVALID_LOG_TIME", err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupLogsLocked(now)
	source, err := s.logSourceLocked(request.AgentID, request.NodeID)
	if err != nil {
		return LogCollection{}, err
	}
	selected := []model.LogService{}
	if len(request.ServiceContainerIDs) > 0 {
		available := map[string]model.LogService{}
		for _, service := range s.logServicesLocked(source) {
			available[service.ContainerID] = service
		}
		seen := map[string]bool{}
		for _, id := range request.ServiceContainerIDs {
			service, ok := available[id]
			if !ok {
				return LogCollection{}, problem(404, "LOG_SERVICE_NOT_FOUND", "所选容器不在该机器的最新发现快照中，请刷新")
			}
			if !service.Available {
				return LogCollection{}, problem(409, "LOG_SERVICE_UNAVAILABLE", service.ContainerName+"："+service.Reason)
			}
			if !seen[id] {
				selected = append(selected, service)
				seen[id] = true
			}
		}
	}
	active := 0
	for _, job := range s.logJobs {
		if job.Status == "COLLECTING" {
			active++
			if job.AgentID == source.AgentID {
				return LogCollection{}, problem(409, "LOG_COLLECTION_BUSY", "该主机正在拉取日志，请等待完成后重试")
			}
		}
	}
	if active >= 4 || len(s.logJobs) >= 16 {
		return LogCollection{}, problem(429, "LOG_COLLECTION_LIMIT", "日志任务已达上限，请等待任务过期后重试")
	}
	job := &LogCollection{ID: ID(), LogSource: source, Status: "COLLECTING", Message: "Agent 正在本机收集并打包日志", Filename: label + "_" + source.AgentID + ".tar.gz", CreatedAt: now, ExpiresAt: now.Add(time.Hour), session: s.agents[source.AgentID].Session}
	job.Mode, job.Time = request.Mode, request.Time
	job.Services = selected
	job.task = model.LogTask{Type: "collect_logs", ID: job.ID, ContainerID: source.ContainerID, SnapshotID: source.SnapshotID, Source: source.Source, Mode: request.Mode, Time: request.Time, Deadline: now.Add(5 * time.Minute)}
	job.task.Services = selected
	if s.logJobs == nil {
		s.logJobs = map[string]*LogCollection{}
	}
	s.logJobs[job.ID] = job
	return *job, nil
}

func failLog(job *LogCollection, message string) {
	job.Status, job.Message = "FAILED", message
	if job.download != nil {
		job.download.cancel()
	}
}

func (s *Store) cleanupLogsLocked(now time.Time) {
	for id, job := range s.logJobs {
		a := s.agents[job.AgentID]
		if job.Status != "FAILED" && (a == nil || !a.Connected || a.Session != job.session) {
			failLog(job, "Agent 连接中断，请确认 Agent 已升级后重新拉取")
		} else if job.Status == "COLLECTING" && !now.Before(job.task.Deadline) {
			failLog(job, "日志收集超时，请检查 Agent 版本和网络后重试")
		}
		if !now.Before(job.ExpiresAt) {
			if job.download != nil {
				job.download.cancel()
			}
			delete(s.logJobs, id)
		}
	}
}

func (s *Store) logCommands(agentID, session string) []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLogsLocked(time.Now())
	items := []any{}
	for _, job := range s.logJobs {
		if job.AgentID != agentID || job.session != session {
			continue
		}
		for _, task := range job.cancels {
			items = append(items, task)
		}
		job.cancels = nil
		if job.Status == "COLLECTING" && !job.sent {
			job.sent = true
			items = append(items, job.task)
		}
		if d := job.download; d != nil && !d.sent && d.ctx.Err() == nil {
			d.sent = true
			items = append(items, d.LogDownloadTask)
		}
	}
	return items
}

func (s *Store) receiveLogResult(agentID, session string, result model.LogResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLogsLocked(time.Now())
	job := s.logJobs[result.ID]
	if job == nil || job.AgentID != agentID || job.session != session || !job.sent || job.Status != "COLLECTING" {
		return
	}
	if result.Error != "" {
		message := result.Error
		if len(message) > 1024 {
			message = message[:1024]
		}
		failLog(job, message)
		return
	}
	if result.Bytes <= 0 || result.Bytes > model.MaxLogArchiveBytes || result.FileCount < 1 || result.FileCount > 2048 {
		failLog(job, "Agent 返回的日志压缩包信息不合法")
		return
	}
	if _, err := time.Parse("20060102T150405Z", strings.TrimPrefix(result.Label, "agora_logs_")); err != nil || !strings.HasPrefix(result.Label, "agora_logs_") {
		failLog(job, "Agent 返回的日志时间信息不合法")
		return
	}
	job.Filename = result.Label + "_" + job.AgentID + ".tar.gz"
	if len(job.Services) == 1 {
		job.Filename = result.Label + "_" + job.AgentID + "_" + job.Services[0].ContainerName + ".tar.gz"
	} else if len(job.Services) > 1 {
		job.Filename = fmt.Sprintf("%s_%s_%d-services.tar.gz", result.Label, job.AgentID, len(job.Services))
	}
	job.Bytes, job.FileCount = result.Bytes, result.FileCount
	job.Status, job.Message = "READY", "Agent 本机日志已就绪，可下载（保留 1 小时）"
}

// The center relays a bounded stream to the requesting browser. It never opens
// or writes an archive file, and cannot download anything until Agent is ready.
func (s *Store) receiveLogChunk(agentID, session string, chunk model.LogChunk) {
	s.mu.Lock()
	job := s.logJobs[chunk.ID]
	if job == nil || job.AgentID != agentID || job.session != session || job.Status != "READY" || job.download == nil {
		s.mu.Unlock()
		return
	}
	d := job.download
	if chunk.TransferID != d.TransferID || !d.sent {
		s.mu.Unlock()
		return
	}
	if chunk.Sequence != d.sequence || len(chunk.Data) > model.LogChunkBytes || d.bytes+int64(len(chunk.Data)) > job.Bytes || (chunk.Done && chunk.Error == "" && d.bytes+int64(len(chunk.Data)) != job.Bytes) {
		chunk.Data, chunk.Done, chunk.Error = nil, true, "Agent 日志流顺序或大小校验失败，请重试下载"
	}
	if len(chunk.Error) > 1024 {
		chunk.Error = chunk.Error[:1024]
	}
	d.sequence++
	d.bytes += int64(len(chunk.Data))
	if chunk.Done || chunk.Error != "" {
		d.finished = true
	}
	s.mu.Unlock()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case d.chunks <- chunk:
	case <-d.ctx.Done():
	case <-timer.C:
		d.cancel()
	}
}

func (s *Store) logCollection(id string) (LogCollection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLogsLocked(time.Now())
	job := s.logJobs[id]
	if job == nil {
		return LogCollection{}, problem(404, "LOG_COLLECTION_NOT_FOUND", "日志任务不存在或已过期，请重新拉取")
	}
	return *job, nil
}

func (s *Store) beginLogDownload(ctx context.Context, id string) (LogCollection, *logDownload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLogsLocked(time.Now())
	job := s.logJobs[id]
	if job == nil {
		return LogCollection{}, nil, problem(404, "LOG_COLLECTION_NOT_FOUND", "日志任务不存在或已过期，请重新拉取")
	}
	if job.Status != "READY" {
		return LogCollection{}, nil, problem(409, "LOG_NOT_READY", job.Message)
	}
	for _, other := range s.logJobs {
		if other.AgentID == job.AgentID && other.download != nil {
			return LogCollection{}, nil, problem(409, "LOG_DOWNLOAD_BUSY", "该 Agent 正在下载日志，请稍后重试")
		}
	}
	deadline := time.Now().Add(5 * time.Minute)
	downloadCtx, cancel := context.WithDeadline(ctx, deadline)
	d := &logDownload{LogDownloadTask: model.LogDownloadTask{Type: "download_logs", ID: job.ID, TransferID: ID(), Deadline: deadline}, ctx: downloadCtx, cancel: cancel, chunks: make(chan model.LogChunk, 8)}
	job.download = d
	return *job, d, nil
}
func (s *Store) endLogDownload(id string, d *logDownload) {
	d.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.logJobs[id]; job != nil && job.download == d {
		if d.sent && !d.finished {
			task := d.LogDownloadTask
			task.Type = "cancel_log_download"
			job.cancels = append(job.cancels, task)
		}
		job.download = nil
	}
}

func (s *Server) relayLogDownload(w http.ResponseWriter, r *http.Request, id string) {
	job, d, err := s.Store.beginLogDownload(r.Context(), id)
	if err != nil {
		writeProblem(w, err)
		return
	}
	defer s.Store.endLogDownload(id, d)
	_ = http.NewResponseController(w).SetWriteDeadline(d.Deadline)
	started := false
	for {
		select {
		case <-d.ctx.Done():
			if !started {
				failure(w, 504, "LOG_DOWNLOAD_INTERRUPTED", "Agent 下载中断或超时，请重试")
			} else {
				panic(http.ErrAbortHandler)
			}
			return
		case chunk := <-d.chunks:
			if chunk.Error != "" {
				if !started {
					failure(w, 502, "AGENT_LOG_DOWNLOAD_FAILED", chunk.Error)
				} else {
					panic(http.ErrAbortHandler)
				}
				return
			}
			if len(chunk.Data) > 0 {
				if !started {
					w.Header().Set("Content-Type", "application/gzip")
					w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, job.Filename))
					w.Header().Set("Content-Length", strconv.FormatInt(job.Bytes, 10))
					started = true
				}
				if _, err := w.Write(chunk.Data); err != nil {
					return
				}
			}
			if chunk.Done {
				return
			}
		}
	}
}

func (s *Server) logRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/log-collections")
	if path == "" {
		if r.Method != "POST" {
			method(w)
			return
		}
		var request model.LogRequest
		if !decode(w, r, &request) {
			return
		}
		job, err := s.Store.CreateLogCollection(request)
		if err != nil {
			writeProblem(w, err)
			return
		}
		send(w, http.StatusAccepted, job)
		return
	}
	if r.Method != "GET" {
		method(w)
		return
	}
	if path == "/source" {
		source, err := s.Store.LogSource(r.URL.Query().Get("agent_id"), r.URL.Query().Get("node_id"))
		if err != nil {
			writeProblem(w, err)
			return
		}
		send(w, 200, source)
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 2 || !identifier.MatchString(parts[0]) || len(parts) == 2 && parts[1] != "download" {
		failure(w, 404, "NOT_FOUND", "路径不存在")
		return
	}
	if len(parts) == 2 {
		s.relayLogDownload(w, r, parts[0])
		return
	}
	job, err := s.Store.logCollection(parts[0])
	if err != nil {
		writeProblem(w, err)
		return
	}
	send(w, 200, job)
}
