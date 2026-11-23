package probe

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func diagnosticEndpoint(job protocol.Job) protocol.DiagnosticEndpoint {
	return protocol.DiagnosticEndpoint{Role: job.Role, UID: job.UID, PublisherUID: job.PublisherUID, Channel: job.Channel}
}
func sameBundleExecution(a, b protocol.Job) bool {
	return a.RunID == b.RunID && a.Revision == b.Revision && a.RuntimeID == b.RuntimeID && a.Channel == b.Channel && a.Target.ID == b.Target.ID && a.TaskID == b.TaskID && a.ParentRunID == b.ParentRunID && a.EdgeEndpoint == b.EdgeEndpoint && reflect.DeepEqual(a.NativeCase, b.NativeCase) && reflect.DeepEqual(a.RTC, b.RTC)
}

// Merge both roles' trace lines into one file, retaining their identities and
// source order. Native SDK binary files are appended separately, once per Service.
func mergeDiagnosticJSONL(parts map[string][]byte) []byte {
	type line struct {
		data  []byte
		at    int64
		seq   uint64
		order int
	}
	var lines []line
	for _, role := range []string{"sender", "receiver"} {
		for _, raw := range bytes.Split(parts[role], []byte{'\n'}) {
			if len(bytes.TrimSpace(raw)) == 0 {
				continue
			}
			var header struct {
				At   time.Time `json:"occurred_at"`
				Seq  uint64    `json:"source_seq"`
				Role string    `json:"endpoint_role"`
			}
			if json.Unmarshal(raw, &header) == nil && header.Role == "" {
				var object map[string]json.RawMessage
				if json.Unmarshal(raw, &object) == nil {
					object["endpoint_role"], _ = json.Marshal(role)
					raw, _ = json.Marshal(object)
				}
			}
			lines = append(lines, line{data: raw, at: func() int64 {
				if header.At.IsZero() {
					return 0
				}
				return header.At.UnixNano()
			}(), seq: header.Seq, order: len(lines)})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].at != lines[j].at {
			return lines[i].at < lines[j].at
		}
		if lines[i].seq != lines[j].seq {
			return lines[i].seq < lines[j].seq
		}
		return lines[i].order < lines[j].order
	})
	var out bytes.Buffer
	for _, line := range lines {
		out.Write(line.data)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func (c *Client) appendDiagnosticFiles(writer *zip.Writer, run, role string) ([]map[string]any, error) {
	shared := role == "pair"
	files := []map[string]any{}
	for _, name := range []string{"events.jsonl", "sdk-api.jsonl", "route-capture.jsonl", "result.json", "process.stdout.log", "process.stderr.log"} {
		perRole := name == "events.jsonl" || name == "sdk-api.jsonl" || name == "result.json"
		roles := []string{role}
		if shared && perRole {
			roles = []string{"sender", "receiver"}
		}
		limit := 1 << 20
		if name == "sdk-api.jsonl" {
			limit = 8 << 20
		}
		parts := map[string][]byte{}
		truncated := false
		for _, sourceRole := range roles {
			source := filepath.Join(c.runDir(run), name)
			if perRole {
				source = filepath.Join(c.endpointDir(run, sourceRole), name)
			}
			f, err := os.Open(source)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
			f.Close()
			if err != nil {
				return nil, err
			}
			if len(data) > limit {
				data = data[:limit]
				truncated = true
			}
			parts[sourceRole] = data
		}
		if len(parts) == 0 {
			files = append(files, map[string]any{"name": name, "missing": true})
			continue
		}
		var data []byte
		if shared && perRole {
			if name == "result.json" {
				results := map[string]json.RawMessage{}
				for sourceRole, raw := range parts {
					if json.Valid(raw) {
						results[sourceRole] = json.RawMessage(raw)
					}
				}
				var err error
				data, err = json.Marshal(results)
				if err != nil {
					return nil, err
				}
				data = append(data, '\n')
			} else {
				data = mergeDiagnosticJSONL(parts)
			}
		} else {
			for _, raw := range parts {
				data = raw
			}
		}
		data = c.safeText(data)
		hash := sha256.Sum256(data)
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(data); err != nil {
			return nil, err
		}
		files = append(files, map[string]any{"name": name, "bytes": len(data), "sha256": hex.EncodeToString(hash[:]), "truncated": truncated})
	}
	return files, nil
}
