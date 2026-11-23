package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/agoralogs"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

type localLogArchive struct {
	path    string
	expires time.Time
}

// LogDownloads owns archives on the Agent machine. Only metadata is reported
// to center; bytes leave this machine when a user actually downloads a file.
type LogDownloads struct {
	mu       sync.Mutex
	dir      string
	err      error
	archives map[string]localLogArchive
	stop     chan struct{}
	done     chan struct{}
}

func (r *Runner) NewLogDownloads() *LogDownloads {
	l := &LogDownloads{dir: filepath.Join(r.dataDir, "log-downloads"), archives: map[string]localLogArchive{}, stop: make(chan struct{}), done: make(chan struct{})}
	// A new Agent connection invalidates center's old session-bound jobs.
	l.err = os.RemoveAll(l.dir)
	if l.err == nil {
		l.err = os.MkdirAll(l.dir, 0700)
	}
	go func() {
		defer close(l.done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-l.stop:
				return
			case <-ticker.C:
				l.mu.Lock()
				l.cleanupLocked()
				l.mu.Unlock()
			}
		}
	}()
	return l
}

func (l *LogDownloads) cleanupLocked() {
	for id, archive := range l.archives {
		if !time.Now().Before(archive.expires) {
			_ = os.Remove(archive.path)
			delete(l.archives, id)
		}
	}
}
func (l *LogDownloads) Close() {
	close(l.stop)
	<-l.done
	_ = os.RemoveAll(l.dir)
}

func (l *LogDownloads) Collect(ctx context.Context, inspector logInspector, hostRoot string, task model.LogTask) model.LogResult {
	result := model.LogResult{Type: "log_result", ID: task.ID}
	_, label, parseErr := agoralogs.Buckets(task.Mode, task.Time, time.Local)
	if parseErr != nil {
		result.Error = parseErr.Error()
		return result
	}
	result.Label = label
	l.mu.Lock()
	l.cleanupLocked()
	available := l.err == nil && len(l.archives) < 16
	l.mu.Unlock()
	if !available {
		result.Error = "Agent 日志临时目录不可写或已达 16 个任务上限"
		return result
	}
	file, err := os.CreateTemp(l.dir, "archive-*.tar.gz")
	if err != nil {
		result.Error = "Agent 无法创建日志压缩包，请检查本机数据目录"
		return result
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			_ = os.Remove(file.Name())
		}
	}()
	count, err := collectLogs(ctx, inspector, hostRoot, task, func(data []byte) error {
		_, writeErr := file.Write(data)
		return writeErr
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	info, err := file.Stat()
	if err != nil {
		result.Error = "Agent 无法读取压缩包信息"
		return result
	}
	if err = file.Close(); err != nil {
		result.Error = "Agent 保存日志压缩包失败"
		return result
	}
	l.mu.Lock()
	l.archives[task.ID] = localLogArchive{path: file.Name(), expires: time.Now().Add(time.Hour)}
	l.mu.Unlock()
	success = true
	result.Bytes, result.FileCount = info.Size(), count
	return result
}

func (l *LogDownloads) Download(ctx context.Context, task model.LogDownloadTask, emit func(model.LogChunk) error) {
	sequence := 0
	err := func() error {
		if task.TransferID == "" || !time.Now().Before(task.Deadline) {
			return errors.New("下载指令已过期")
		}
		l.mu.Lock()
		l.cleanupLocked()
		archive, found := l.archives[task.ID]
		var file *os.File
		var openErr error
		if found {
			file, openErr = os.Open(archive.path)
		}
		l.mu.Unlock()
		if !found || openErr != nil {
			return errors.New("Agent 上的日志压缩包已过期或不可读，请重新拉取")
		}
		defer file.Close()
		buffer := make([]byte, model.LogChunkBytes)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := file.Read(buffer)
			if n > 0 {
				if sendErr := emit(model.LogChunk{Type: "log_chunk", ID: task.ID, TransferID: task.TransferID, Sequence: sequence, Data: append([]byte(nil), buffer[:n]...)}); sendErr != nil {
					return sendErr
				}
				sequence++
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return errors.New("Agent 读取日志压缩包失败")
			}
		}
	}()
	result := model.LogChunk{Type: "log_chunk", ID: task.ID, TransferID: task.TransferID, Sequence: sequence, Done: true}
	if err != nil {
		result.Error = err.Error()
	}
	_ = emit(result)
}
