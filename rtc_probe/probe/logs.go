package probe

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func (c *Client) collect(ctx context.Context, request protocol.LogRequest) {
	if !protocol.ValidID(request.ID) || request.ProbeID != c.config.ID || time.Now().After(request.ExpiresAt) {
		return
	}
	c.mu.Lock()
	if time.Now().Before(c.nextLogAttempt[request.ID]) {
		c.mu.Unlock()
		return
	}
	r := c.records[recordKey(request.RunID, request.Role)]
	if r == nil {
		if c.logReports == nil {
			c.logReports = map[string]protocol.LogRequest{}
		}
		request.Status = "EXPIRED"
		request.Error = "LOCAL_RETENTION_EXPIRED"
		c.logReports[request.ID] = request
		c.mu.Unlock()
		return
	}
	if r.Job.Role != request.Role || c.collecting[request.ID] || c.stopping || ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	job := r.Job
	partial := r.Result == nil
	notStarted := r.Result != nil && r.Result.Cleanup == "NOT_STARTED"
	c.collecting[request.ID] = true
	c.logsWG.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.logsWG.Done()
		requestCtx, cancel := context.WithDeadline(ctx, request.ExpiresAt)
		defer cancel()
		defer func() {
			c.mu.Lock()
			delete(c.collecting, request.ID)
			delete(c.collectActive, request.ID)
			if c.nextLogAttempt == nil {
				c.nextLogAttempt = map[string]time.Time{}
			}
			c.nextLogAttempt[request.ID] = time.Now().Add(2 * time.Second)
			c.mu.Unlock()
		}()
		select {
		case c.logsSlot <- struct{}{}:
			defer func() { <-c.logsSlot }()
		case <-requestCtx.Done():
			return
		}
		c.mu.Lock()
		if c.collectActive == nil {
			c.collectActive = map[string]string{}
		}
		c.collectActive[request.ID] = request.RunID
		if _, err := os.Stat(c.runDir(request.RunID)); os.IsNotExist(err) {
			if c.logReports == nil {
				c.logReports = map[string]protocol.LogRequest{}
			}
			request.Status = "EXPIRED"
			request.Error = "LOCAL_RETENTION_EXPIRED"
			if notStarted {
				request.Status = "FAILED"
				request.Error = "SDK_NOT_STARTED"
			}
			c.logReports[request.ID] = request
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		path := filepath.Join(c.runDir(request.RunID), "bundles", request.ID+".zip")
		data, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			var buf bytes.Buffer
			writer := zip.NewWriter(&buf)
			files := []map[string]any{}
			for _, name := range []string{"events.jsonl", "sdk-api.jsonl", "result.json", "process.stdout.log", "process.stderr.log"} {
				source := filepath.Join(c.runDir(request.RunID), name)
				if name == "events.jsonl" || name == "result.json" || name == "sdk-api.jsonl" {
					source = filepath.Join(c.endpointDir(request.RunID, request.Role), name)
				}
				f, err := os.Open(source)
				if err != nil {
					files = append(files, map[string]any{"name": name, "missing": true})
					continue
				}
				limit := 1 << 20
				if name == "sdk-api.jsonl" {
					limit = 8 << 20
				}
				b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
				f.Close()
				if err != nil {
					writer.Close()
					return
				}
				truncated := len(b) > limit
				if truncated {
					b = b[:limit]
				}
				b = c.safeText(b)
				sum := sha256.Sum256(b)
				entry, err := writer.Create(name)
				if err != nil {
					writer.Close()
					return
				}
				if _, err = entry.Write(b); err != nil {
					writer.Close()
					return
				}
				files = append(files, map[string]any{"name": name, "bytes": len(b), "sha256": hex.EncodeToString(sum[:]), "truncated": truncated})
			}
			sdkFiles, err := c.appendSDKLogs(writer, request.RunID)
			if err != nil {
				writer.Close()
				return
			}
			files = append(files, sdkFiles...)

			manifest := map[string]any{"request_id": request.ID, "job": job, "driver": job.Target.Driver, "simulation_only": job.Target.Driver == "mock", "partial": partial, "collected_at": time.Now().UTC(), "files": files, "native_sdk_logs": map[bool]string{true: "collected", false: "unavailable"}[len(sdkFiles) > 0]}
			var exit protocol.ProcessExitInfo
			if durable.Read(filepath.Join(c.runDir(request.RunID), "process.exit.json"), 4096, &exit) == nil {
				manifest["process_exit"] = exit
			}
			entry, err := writer.Create("manifest.json")
			if err != nil {
				return
			}
			if err = json.NewEncoder(entry).Encode(manifest); err != nil {
				return
			}
			if err = writer.Close(); err != nil {
				return
			}
			data = buf.Bytes()
			if len(data) > protocol.MaxBundle {
				c.mu.Lock()
				if c.logReports == nil {
					c.logReports = map[string]protocol.LogRequest{}
				}
				request.Status = "FAILED"
				request.Error = "LOG_BUNDLE_LIMIT_EXCEEDED"
				c.logReports[request.ID] = request
				c.mu.Unlock()
				return
			}
			if err = durable.Bytes(path, data); err != nil {
				return
			}
		} else if e != nil {
			return
		}
		client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		_ = c.uploadBundle(requestCtx, client, request, data)

		client.CloseIdleConnections()
	}()
}

// Keep native binary log files separate so vendor tools can decode them.
func (c *Client) appendSDKLogs(writer *zip.Writer, run string) ([]map[string]any, error) {
	dir := filepath.Join(c.runDir(run), "sdk")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []map[string]any
	remaining := 4 << 20
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.Contains(entry.Name(), ".log") {
			continue
		}
		name := "sdk/" + entry.Name()
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			files = append(files, map[string]any{"name": name, "missing": true})
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(remaining)+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		truncated := len(b) > remaining
		if truncated {
			b = b[:remaining]
		}
		remaining -= len(b)
		b = c.safeText(b)
		sum := sha256.Sum256(b)
		target, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = target.Write(b); err != nil {
			return nil, err
		}
		files = append(files, map[string]any{"name": name, "bytes": len(b), "sha256": hex.EncodeToString(sum[:]), "truncated": truncated})
	}
	return files, nil
}
