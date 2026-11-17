package hub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

func diagnosticName(name string) bool {
	switch name {
	case "manifest.json", "events.jsonl", "sdk-api.jsonl", "route-capture.jsonl", "result.json", "process.stdout.log", "process.stderr.log", "sdk.log":
		return true
	}
	return strings.HasPrefix(name, "sdk/agora") && filepath.Base(name) == strings.TrimPrefix(name, "sdk/") && strings.Contains(name, ".log") && !strings.Contains(name, "..") && !strings.ContainsAny(name, "\\\x00") && len(name) < 128
}
func validateDiagnosticBundle(data []byte, request protocol.LogRequest, expected protocol.Job) (bool, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) == 0 || len(z.File) > 16 {
		return false, errors.New("invalid diagnostic bundle")
	}
	type descriptor struct {
		Name    string `json:"name"`
		Bytes   int64  `json:"bytes"`
		SHA256  string `json:"sha256"`
		Missing bool   `json:"missing"`
	}
	var manifest struct {
		Scope     string                                 `json:"scope"`
		Endpoints map[string]protocol.DiagnosticEndpoint `json:"endpoints"`
		RequestID string                                 `json:"request_id"`
		Job       protocol.Job                           `json:"job"`
		Partial   bool                                   `json:"partial"`
		Files     []descriptor                           `json:"files"`
	}
	actual := map[string]descriptor{}
	var total uint64
	found := false
	for _, f := range z.File {
		if !diagnosticName(f.Name) || !f.Mode().IsRegular() || f.UncompressedSize64 > (32<<20)-total {
			return false, errors.New("invalid diagnostic file")
		}
		if _, exists := actual[f.Name]; exists {
			return false, errors.New("duplicate diagnostic file")
		}
		total += f.UncompressedSize64
		if f.Name == "manifest.json" && f.UncompressedSize64 > 64<<10 {
			return false, errors.New("manifest size limit exceeded")
		}
		reader, err := f.Open()
		if err != nil {
			return false, errors.New("invalid diagnostic stream")
		}
		hash := sha256.New()
		var document bytes.Buffer
		var writer io.Writer = hash
		if f.Name == "manifest.json" {
			writer = io.MultiWriter(hash, &document)
		}
		n, copyErr := io.Copy(writer, io.LimitReader(reader, int64(f.UncompressedSize64)+1))
		reader.Close()
		if copyErr != nil || n != int64(f.UncompressedSize64) {
			return false, errors.New("diagnostic CRC or length mismatch")
		}
		actual[f.Name] = descriptor{Name: f.Name, Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
		if f.Name == "manifest.json" {
			if json.Unmarshal(document.Bytes(), &manifest) != nil {
				return false, errors.New("invalid manifest")
			}
			found = true
		}
	}
	if !found {
		return false, errors.New("manifest required")
	}
	job := manifest.Job
	if manifest.RequestID != request.ID || job.RunID != request.RunID || job.ProbeID != request.ProbeID || job.Role != request.Role || job.Target.ID != expected.Target.ID || job.Revision != expected.Revision || job.Channel != expected.Channel || job.UID != expected.UID || job.PublisherUID != expected.PublisherUID || job.ParentRunID != expected.ParentRunID || job.EdgeEndpoint != expected.EdgeEndpoint {
		return false, errors.New("diagnostic attribution mismatch")
	}
	if request.Role == "pair" {
		if request.Scope != protocol.ServiceLogScope || manifest.Scope != protocol.ServiceLogScope || job.LogScope != protocol.ServiceLogScope || expected.LogScope != protocol.ServiceLogScope || len(manifest.Endpoints) != 2 || !reflect.DeepEqual(job.NativeCase, expected.NativeCase) || !reflect.DeepEqual(job.RTC, expected.RTC) {
			return false, errors.New("invalid shared Service context")
		}
		for _, role := range []string{"sender", "receiver"} {
			endpoint, ok := manifest.Endpoints[role]
			uid := strconv.FormatUint(uint64(protocol.SenderUID), 10)
			if role == "receiver" {
				uid = strconv.FormatUint(uint64(protocol.ReceiverUID), 10)
			}
			if !ok || endpoint.Role != role || endpoint.Channel != expected.Channel || endpoint.UID != uid || endpoint.PublisherUID != expected.PublisherUID {
				return false, errors.New("shared endpoint attribution mismatch")
			}
		}
	}
	if len(manifest.Files) > 32 {
		return false, errors.New("too many file descriptors")
	}
	if len(manifest.Files) > 0 {
		seen := map[string]bool{}
		for _, d := range manifest.Files {
			if !diagnosticName(d.Name) || d.Name == "manifest.json" || seen[d.Name] {
				return false, errors.New("invalid manifest file descriptor")
			}
			seen[d.Name] = true
			a, exists := actual[d.Name]
			if d.Missing {
				if exists {
					return false, errors.New("missing file descriptor conflicts with archive")
				}
				continue
			}
			if !exists || d.Bytes != a.Bytes || d.SHA256 != a.SHA256 {
				return false, errors.New("diagnostic file digest mismatch")
			}
		}
		for name := range actual {
			if name != "manifest.json" && !seen[name] {
				return false, errors.New("unlisted diagnostic file")
			}
		}
	}
	return manifest.Partial, nil
}
