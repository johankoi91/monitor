package probe

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeLogsRemainSeparateBinaryFiles(t *testing.T) {
	c := &Client{config: Config{DataDir: t.TempDir()}, identity: RuntimeIdentity{Secret: "probe-secret-not-in-log"}}
	dir := filepath.Join(c.runDir("run"), "sdk")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, second := []byte{0, 255, 254, 1, 2, 3}, []byte("separate API log")
	for name, data := range map[string][]byte{"agorasdk.log": first, "agoraapi.log": second, "agoradns.dat": []byte("not a diagnostic log")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	files, err := c.appendSDKLogs(w, "run")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || len(z.File) != 2 {
		t.Fatal("native logs or auxiliary data merged", files)
	}
	expected := map[string][]byte{"sdk/agorasdk.log": first, "sdk/agoraapi.log": second}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, expected[f.Name]) {
			t.Fatal("native binary log changed", f.Name)
		}
	}
}

func TestCachedBundleRejectsLegacyZeroUIDWithoutAcceptingForeignIdentity(t *testing.T) {
	job := protocol.Job{RunID: "run", Revision: "revision", Role: "receiver", RuntimeID: "probe", Channel: "channel", UID: "10002", PublisherUID: "10001"}
	bundle := func(value protocol.Job) []byte {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		entry, err := writer.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.NewEncoder(entry).Encode(map[string]any{"job": value}); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	if !cachedBundleMatchesJob(bundle(job), job) {
		t.Fatal("matching artifact rejected")
	}
	legacy := job
	legacy.UID = "0"
	if cachedBundleMatchesJob(bundle(legacy), job) {
		t.Fatal("legacy artifact accepted with wrong UID")
	}
	foreign := job
	foreign.RunID = "other"
	if cachedBundleMatchesJob(bundle(foreign), job) {
		t.Fatal("foreign artifact accepted")
	}
}

func TestSharedServiceBundleMergesBothRolesAndKeepsNativeLogsOnce(t *testing.T) {
	c := retentionFixture(t)
	run := "shared-run"
	for _, role := range []string{"sender", "receiver"} {
		dir := c.endpointDir(run, role)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		seq := 1
		if role == "receiver" {
			seq = 2
		}
		event := map[string]any{"event_id": role, "worker_id": "shared-worker", "source_seq": seq, "occurred_at": "2026-11-18T00:00:00Z", "endpoint_role": role, "kind": "sdk_callback", "fields": map[string]any{"parameters": map[string]any{"role": role}}}
		data, _ := json.Marshal(event)
		for _, name := range []string{"sdk-api.jsonl", "events.jsonl"} {
			if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}
		data, _ = json.Marshal(map[string]any{"worker_id": "shared-worker", "endpoint_role": role, "outcome": "SUCCESS"})
		if err := os.WriteFile(filepath.Join(dir, "result.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sdk := filepath.Join(c.runDir(run), "sdk")
	os.MkdirAll(sdk, 0700)
	raw := []byte{0, 255, 2, 3}
	os.WriteFile(filepath.Join(sdk, "agorasdk.log"), raw, 0600)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if _, err := c.appendDiagnosticFiles(writer, run, "pair"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.appendSDKLogs(writer, run); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	nativeCount := 0
	for _, file := range archive.File {
		f, _ := file.Open()
		data, _ := io.ReadAll(f)
		f.Close()
		switch file.Name {
		case "sdk/agorasdk.log":
			nativeCount++
			if !bytes.Equal(data, raw) {
				t.Fatal("shared SDK log changed")
			}
		case "sdk-api.jsonl", "events.jsonl":
			decoder := json.NewDecoder(bytes.NewReader(data))
			roles := map[string]bool{}
			count := 0
			for {
				var event protocol.Event
				err := decoder.Decode(&event)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				roles[event.Role] = true
				count++
				if event.Seq != uint64(count) || event.Fields["parameters"] == nil {
					t.Fatal("trace data or ordering lost", event)
				}
			}
			if count != 2 || !roles["sender"] || !roles["receiver"] {
				t.Fatal("one role lost", roles)
			}
		case "result.json":
			var results map[string]json.RawMessage
			if json.Unmarshal(data, &results) != nil || len(results) != 2 {
				t.Fatal("missing endpoint result")
			}
		}
	}
	if nativeCount != 1 {
		t.Fatal("native SDK logs duplicated", nativeCount)
	}
}
