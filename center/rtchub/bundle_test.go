package hub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"testing"
)

func testDiagnostic(t *testing.T, request protocol.LogRequest, job protocol.Job, partial, badHash bool) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	payload := []byte("unique-binary-payload-12345")
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	if badHash {
		digest = "wrong"
	}
	manifest := map[string]any{"request_id": request.ID, "job": job, "partial": partial, "files": []map[string]any{{"name": "process.stdout.log", "bytes": len(payload), "sha256": digest}}}
	f, _ := w.Create("manifest.json")
	json.NewEncoder(f).Encode(manifest)
	f, _ = w.CreateHeader(&zip.FileHeader{Name: "process.stdout.log", Method: zip.Store})
	f.Write(payload)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestDiagnosticChecksCRCFileDigestsAndExecutionAttribution(t *testing.T) {
	job := protocol.Job{RunID: "run", RuntimeID: "probe", Role: "sender", Revision: "v1", Target: protocol.Target{ID: "rtc"}, Channel: "current", UID: "10001", PublisherUID: "10001"}
	req := protocol.LogRequest{ID: "request", RunID: job.RunID, RuntimeID: job.RuntimeID, Role: job.Role}
	valid := testDiagnostic(t, req, job, false, false)
	if partial, err := validateDiagnosticBundle(valid, req, job); err != nil || partial {
		t.Fatal("valid bundle rejected", err)
	}
	if _, err := validateDiagnosticBundle(testDiagnostic(t, req, job, false, true), req, job); err == nil {
		t.Fatal("file digest mismatch accepted")
	}
	damaged := append([]byte(nil), valid...)
	index := bytes.Index(damaged, []byte("unique-binary-payload-12345"))
	if index < 0 {
		t.Fatal("fixture payload not found")
	}
	damaged[index] ^= 0xff
	if _, err := validateDiagnosticBundle(damaged, req, job); err == nil {
		t.Fatal("ZIP CRC corruption accepted")
	}
	old := job
	old.Channel = "previous"
	if _, err := validateDiagnosticBundle(testDiagnostic(t, req, old, false, false), req, job); err == nil {
		t.Fatal("old channel bundle accepted")
	}
	partial, err := validateDiagnosticBundle(testDiagnostic(t, req, job, true, false), req, job)
	if err != nil || !partial {
		t.Fatal("partial snapshot flag lost", err)
	}
}

func TestSharedBundleRequiresBothEndpointIdentities(t *testing.T) {
	job := protocol.Job{RunID: "run", RuntimeID: "probe", Role: "pair", Revision: "revision", Channel: "channel", UID: "10001", PublisherUID: "10001", LogScope: protocol.ServiceLogScope, Target: protocol.Target{ID: "target"}}
	req := protocol.LogRequest{ID: "request", RunID: job.RunID, RuntimeID: job.RuntimeID, Role: "pair", Scope: protocol.ServiceLogScope}
	build := func(receiverChannel string, missing bool) []byte {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		endpoints := map[string]protocol.DiagnosticEndpoint{"sender": {Role: "sender", Channel: job.Channel, UID: "10001", PublisherUID: "10001"}, "receiver": {Role: "receiver", Channel: receiverChannel, UID: "10002", PublisherUID: "10001"}}
		if missing {
			delete(endpoints, "receiver")
		}
		f, _ := writer.Create("manifest.json")
		json.NewEncoder(f).Encode(map[string]any{"request_id": req.ID, "job": job, "scope": protocol.ServiceLogScope, "endpoints": endpoints})
		writer.Close()
		return buffer.Bytes()
	}
	if _, err := validateDiagnosticBundle(build("channel", false), req, job); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDiagnosticBundle(build("foreign-channel", false), req, job); err == nil {
		t.Fatal("foreign receiver accepted")
	}
	if _, err := validateDiagnosticBundle(build("channel", true), req, job); err == nil {
		t.Fatal("receiver identity missing")
	}
}

func TestNativeDiagnosticIgnoresBrowserOnlySettingsWithoutIgnoringNativeIdentity(t *testing.T) {
	expected := &protocol.RTCParameters{AppID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AuthMode: "none", WebAccess: &protocol.WebAccessConfiguration{Port: 443, IgnoreCertificates: true}}
	legacy := *expected
	legacy.WebAccess = nil
	if !sameDiagnosticRTC(&legacy, expected, "agora") {
		t.Fatal("browser-only fields broke old Native diagnostic")
	}
	if sameDiagnosticRTC(&legacy, expected, protocol.WebDriver) {
		t.Fatal("Web access parameters ignored for browser diagnostic")
	}
	legacy.AppID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if sameDiagnosticRTC(&legacy, expected, "agora") {
		t.Fatal("different AppID accepted")
	}
}
