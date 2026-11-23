package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

type logInspectorStub struct{ err error }

func (s logInspectorStub) VerifyLogSource(context.Context, model.LogTask) error { return s.err }

func TestLogsUseMountedHostDirectoryAndSkipLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom/agora")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"ap.DEBUG.20260521-21.1": "before midnight", "media.DEBUG.20260522-00.2": "after midnight", "ap.DEBUG.20260522-03.3": "outside window", "ap.INFO.20260522-00.1": "wrong level"}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "ap.DEBUG.20260522-03.3"), filepath.Join(dir, "link.DEBUG.20260522-00.1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.DEBUG.20260522-00.1"), 0700); err != nil {
		t.Fatal(err)
	}
	task := model.LogTask{ID: "test", Source: "/custom/agora", Mode: "point", Time: "20260522001500", Deadline: time.Now().Add(time.Minute)}
	downloads := (&Runner{dataDir: t.TempDir()}).NewLogDownloads()
	defer downloads.Close()
	result := downloads.Collect(context.Background(), logInspectorStub{}, root, task)
	if result.Error != "" || result.FileCount != 2 || result.Bytes == 0 {
		t.Fatalf("%+v", result)
	}
	var packed bytes.Buffer
	var downloadResult model.LogChunk
	sequence := 0
	downloads.Download(context.Background(), model.LogDownloadTask{ID: task.ID, TransferID: "transfer", Deadline: time.Now().Add(time.Minute)}, func(chunk model.LogChunk) error {
		if chunk.Sequence != sequence || len(chunk.Data) > model.LogChunkBytes {
			t.Fatal("invalid chunk")
		}
		sequence++
		packed.Write(chunk.Data)
		downloadResult = chunk
		return nil
	})
	if !downloadResult.Done || downloadResult.Error != "" {
		t.Fatal(downloadResult)
	}
	gz, err := gzip.NewReader(&packed)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(header.Name, "agora_logs_20260522T001500Z/")
		body, err := io.ReadAll(tr)
		if err != nil || string(body) != files[name] {
			t.Fatalf("bad archive entry: %s %q %v", name, body, err)
		}
		seen[name] = true
	}
	if len(seen) != 2 || !seen["ap.DEBUG.20260521-21.1"] || !seen["media.DEBUG.20260522-00.2"] {
		t.Fatal(seen)
	}
	for _, tc := range []struct {
		name      string
		inspector logInspectorStub
		task      model.LogTask
	}{
		{"identity changed", logInspectorStub{errors.New("changed")}, task},
		{"empty window", logInspectorStub{}, func() model.LogTask { copy := task; copy.Time = "20260101000000"; return copy }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result = downloads.Collect(context.Background(), tc.inspector, root, tc.task)
			if result.Error == "" {
				t.Fatal("failure was not reported")
			}
		})
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	task.Source = "/escape"
	result = downloads.Collect(context.Background(), logInspectorStub{}, root, task)
	if result.Error == "" {
		t.Fatalf("allowed escaped host root: %+v", result)
	}
}

func TestLogsResolveAbsoluteHostLinksWithinMountedRoot(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "data/log/agora")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var/log"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"var/log/agora": "/data/log/agora",
		"relative":      "var/log/agora",
		"chain":         "/relative",
		"escape":        "../outside",
		"loop":          "/loop",
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	filename := "media_edge_1.log.DEBUG.20261122-06.3657"
	if err := os.WriteFile(filepath.Join(directory, filename), []byte("host log through absolute symlink"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"/var/log/agora", "/relative", "/chain"} {
		t.Run(source, func(t *testing.T) {
			task := model.LogTask{ID: "host-links", Source: source, Mode: "point", Time: "2026-11-22T07:30:00Z", Deadline: time.Now().Add(time.Minute)}
			var packed bytes.Buffer
			count, err := collectLogs(context.Background(), logInspectorStub{}, root, task, func(data []byte) error { _, err := packed.Write(data); return err })
			if err != nil || count != 1 {
				t.Fatalf("absolute host link did not collect logs: count=%d err=%v", count, err)
			}
		})
	}
	for _, source := range []string{"/escape", "/loop", "/missing"} {
		if _, err := resolveHostLogDirectory(root, source); err == nil {
			t.Fatal("invalid host path accepted", source)
		}
	}
}

func TestServiceArchiveExcludesOtherPIDAndOtherProcess(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"media_edge_1.log.DEBUG.20261117-09.30543", "media_edge_1.log.DEBUG.20261117-09.30544", "media_edge_2.log.DEBUG.20261117-09.30543"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	services := []model.LogService{{Processes: []model.ContainerProcess{{Name: "media_edge_1", PID: 30543}}}}
	task := model.LogTask{ID: "filtered", Source: "/logs", Mode: "point", Time: "20261117103000", Deadline: time.Now().Add(time.Minute), Services: services}
	var archive bytes.Buffer
	count, err := collectLogs(context.Background(), logInspectorStub{}, root, task, func(data []byte) error { _, err := archive.Write(data); return err })
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	gz, err := gzip.NewReader(&archive)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil || !strings.HasSuffix(header.Name, "media_edge_1.log.DEBUG.20261117-09.30543") {
		t.Fatalf("%+v %v", header, err)
	}
	if _, err = tr.Next(); err != io.EOF {
		t.Fatalf("included another container: %v", err)
	}
}
