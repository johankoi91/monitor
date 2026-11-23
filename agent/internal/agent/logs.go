package agent

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/agoralogs"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

type logInspector interface {
	VerifyLogSource(context.Context, model.LogTask) error
}

type logContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r logContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type logChunkWriter struct {
	ctx  context.Context
	emit func([]byte) error
	size int64
}

func (w *logChunkWriter) Write(data []byte) (int, error) {
	if w.size+int64(len(data)) > model.MaxLogArchiveBytes {
		return 0, errors.New("日志压缩包超过 256 MiB，请缩小时间范围")
	}
	n := 0
	for len(data) > 0 {
		if err := w.ctx.Err(); err != nil {
			return n, errors.New("日志收集已取消或超时")
		}
		size := len(data)
		if size > model.LogChunkBytes {
			size = model.LogChunkBytes
		}
		if err := w.emit(append([]byte(nil), data[:size]...)); err != nil {
			return n, err
		}
		n += size
		w.size += int64(size)
		data = data[size:]
	}
	return n, nil
}

func collectLogs(ctx context.Context, inspector logInspector, hostRoot string, task model.LogTask, emit func([]byte) error) (int, error) {
	if task.ID == "" || task.Deadline.IsZero() || !time.Now().Before(task.Deadline) {
		return 0, errors.New("日志任务已过期")
	}
	if err := inspector.VerifyLogSource(ctx, task); err != nil {
		return 0, err
	}
	tags, label, err := agoralogs.Buckets(task.Mode, task.Time, time.Local)
	if err != nil {
		return 0, err
	}
	if hostRoot == "" {
		hostRoot = "/"
	}
	root, err := filepath.EvalSymlinks(hostRoot)
	if err != nil || !filepath.IsAbs(root) {
		return 0, errors.New("Agent 的宿主机根目录不可用")
	}
	if !filepath.IsAbs(task.Source) || filepath.Clean(task.Source) != task.Source {
		return 0, errors.New("日志挂载目录无效")
	}
	directory, err := resolveHostLogDirectory(root, task.Source)
	if err != nil {
		return 0, errors.New("宿主机日志目录不可读，请检查 Agent 的 AVOPS_HOST_ROOT 只读挂载")
	}
	rel, err := filepath.Rel(root, directory)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return 0, errors.New("日志目录超出 Agent 的宿主机根目录")
	}
	folder, err := os.Open(directory)
	if err != nil {
		return 0, errors.New("宿主机日志目录不可读")
	}
	defer folder.Close()
	files := []os.FileInfo{}
	var total int64
	// Scan incrementally; do not load an unbounded directory into memory.
	for {
		entries, readErr := folder.Readdir(256)
		for _, info := range entries {
			if !info.Mode().IsRegular() || !agoralogs.Matches(info.Name(), tags) || !agoralogs.MatchesServices(info.Name(), task.Services) {
				continue
			}
			total += info.Size()
			files = append(files, info)
			if total > model.MaxLogSourceBytes || len(files) > 2048 {
				return 0, errors.New("匹配日志超过 1 GiB 或 2048 个文件，请缩小时间范围")
			}
		}
		if ctx.Err() != nil {
			return 0, errors.New("日志收集已取消或超时")
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, errors.New("读取日志目录失败")
		}
	}
	if len(files) == 0 {
		return 0, errors.New("未找到时间窗口内对应的日志文件")
	}
	w := &logChunkWriter{ctx: ctx, emit: emit}
	buffer := bufio.NewWriterSize(w, model.LogChunkBytes)
	gz := gzip.NewWriter(buffer)
	tw := tar.NewWriter(gz)
	for _, info := range files {
		// Never follow a symlink substituted after the directory scan.
		fd, openErr := syscall.Open(filepath.Join(directory, info.Name()), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if openErr != nil {
			return 0, fmt.Errorf("读取日志 %s 失败", info.Name())
		}
		file := os.NewFile(uintptr(fd), info.Name())
		current, statErr := file.Stat()
		if statErr != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
			file.Close()
			return 0, errors.New("收集期间日志文件发生替换，请重试")
		}
		header := &tar.Header{Name: label + "/" + info.Name(), Mode: 0600, Size: info.Size(), ModTime: info.ModTime()}
		err = tw.WriteHeader(header)
		if err == nil {
			_, err = io.CopyN(tw, logContextReader{ctx: ctx, reader: file}, info.Size())
		}
		file.Close()
		if err != nil {
			return 0, errors.New("日志文件读取或传输失败：" + err.Error())
		}
	}
	if err = tw.Close(); err != nil {
		return 0, err
	}
	if err = gz.Close(); err != nil {
		return 0, err
	}
	if err = buffer.Flush(); err != nil {
		return 0, err
	}
	return len(files), nil
}

// Resolve absolute links in the host's filesystem namespace. A host link such
// as /var/log/agora -> /data/log/agora must stay under /host inside the Agent,
// rather than resolving against the container's own /data directory.
func resolveHostLogDirectory(root, source string) (string, error) {
	pending := strings.Split(source, string(filepath.Separator))
	var parts []string
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) == 0 {
				return "", errors.New("日志目录超出 Agent 的宿主机根目录")
			}
			parts = parts[:len(parts)-1]
			continue
		}
		candidate := filepath.Join(append(append([]string{root}, parts...), part)...)
		info, err := os.Lstat(candidate)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 255 {
				return "", errors.New("日志目录符号链接过多或存在循环")
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(target) {
				parts = nil
			}
			pending = append(strings.Split(target, string(filepath.Separator)), pending...)
			continue
		}
		if !info.IsDir() {
			return "", errors.New("日志目录路径包含非目录项")
		}
		parts = append(parts, part)
	}
	return filepath.Join(append([]string{root}, parts...)...), nil
}
