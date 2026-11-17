package hub

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func TestNativeCredentialsStayAtCenterAndPairHasOneAssignment(t *testing.T) {
	c := testConfig()
	c.Targets = nil
	c.Tasks = nil
	h, err := New(t.TempDir(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := h.Session("probe-a")
	hello := protocol.Msg("hello")
	hello.Driver = "agora"
	hello.Capacity = 1
	if _, err = h.Receive("probe-a", s, hello); err != nil {
		t.Fatal(err)
	}
	cert := strings.Repeat("c", 32)
	plan := Plan{Targets: []protocol.Target{{ID: "native", Name: "真实 RTC", Driver: "agora"}}, Tasks: []protocol.Task{{ID: "native-av", TargetID: "native", Sender: "probe-a", Receiver: "probe-a", Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 30, RTC: &protocol.RTCParameters{AppID: strings.Repeat("a", 32), AuthMode: "certificate"}, Credentials: &protocol.Credentials{AppCertificate: cert}}}}
	if err = h.UpdatePlan(planRevision(h.planLocked()), plan); err != nil {
		t.Fatal(err)
	}
	run, err := h.Start("native-av")
	if err != nil {
		t.Fatal(err)
	}
	commands := h.Commands("probe-a", s)
	if len(commands) != 1 || commands[0].Role != "pair" || commands[0].Job == nil {
		t.Fatal("native job was not a single pair assignment")
	}
	creds := commands[0].Job.Credentials
	if creds == nil || creds.SenderToken == "" || creds.ReceiverToken == "" || creds.AppCertificate != "" {
		t.Fatal("probe did not receive only short-lived tokens")
	}
	public, _ := json.Marshal(h.Snapshot())
	detail, _ := h.Detail(run.ID)
	for _, value := range []string{cert, creds.SenderToken, creds.ReceiverToken} {
		if strings.Contains(string(public), value) || strings.Contains(string(detail), value) {
			t.Fatal("credential leaked through management response")
		}
	}
	if _, err = h.Start("native-av"); err == nil {
		t.Fatal("same task reentered")
	}
}
func TestAudioSuccessDoesNotMaskMissingVideo(t *testing.T) {
	r := &Run{Driver: "agora", Endpoints: map[string]*Endpoint{"sender": {Job: protocol.Job{RTC: &protocol.RTCParameters{Template: "audio_video", MinAudioMS: 1000, MinVideoFrames: 5}}}}}
	a := &protocol.Result{Evidence: protocol.Evidence{Connected: true, Published: true, SentFrames: 10, VideoPublished: true, SentVideoFrames: 10}}
	b := &protocol.Result{Evidence: protocol.Evidence{Connected: true, Subscribed: true, MediaDurationMS: 1000, VideoSubscribed: true}}
	if verifiedEvidence(r, a, b) {
		t.Fatal("audio alone passed combined template")
	}
	b.Evidence.ReceivedVideoFrames = 5
	if !verifiedEvidence(r, a, b) {
		t.Fatal("complete audio/video evidence did not pass")
	}
	b.Evidence.MediaDurationMS = 0
	if verifiedEvidence(r, a, b) {
		t.Fatal("video alone passed combined template")
	}
}

func TestNativeCapabilityMismatchPausesTask(t *testing.T) {
	h, task := healthFixture(t)
	session := h.sessions[task.Sender]
	session.Capabilities = []string{"rtc_audio_v1", "retention_v1"}
	if _, err := h.Start(task.ID); err == nil {
		t.Fatal("audio-only probe received combined RTC task")
	}
	if current := h.effectiveHealth(task, time.Now().UTC()); current.State != "UNKNOWN" || current.Reason != "PROBE_CAPABILITY_MISMATCH" {
		t.Fatal("unsupported media capabilities appeared healthy", current)
	}
	session.Capabilities = append(session.Capabilities, "rtc_video_v1")
	if _, err := h.Start(task.ID); err != nil {
		t.Fatal("capable probe rejected", err)
	}
}
