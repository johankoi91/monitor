package probe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func TestAcknowledgementCoalescingKeepsCrashReplaySafe(t *testing.T) {
	c := retentionFixture(t)
	job := protocol.Job{RunID: protocol.ID(), Role: "sender", Revision: "revision"}
	event := protocol.Event{ID: protocol.ID()}
	result := &protocol.Result{ID: protocol.ID()}
	key := recordKey(job.RunID, job.Role)
	c.records[key] = &record{Job: job, Events: []protocol.Event{event}, Result: result}
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	for _, ack := range []protocol.Message{
		{Type: "telemetry_ack", RunID: job.RunID, Role: job.Role, Revision: job.Revision, EventIDs: []string{event.ID}},
		{Type: "result_ack", RunID: job.RunID, Role: job.Role, Revision: job.Revision, ID: result.ID},
	} {
		if err := c.handle(context.Background(), ack); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]*record {
		raw, err := os.ReadFile(filepath.Join(c.config.DataDir, "outbox-v2.json"))
		if err != nil {
			t.Fatal(err)
		}
		var records map[string]*record
		if err = json.Unmarshal(raw, &records); err != nil {
			t.Fatal(err)
		}
		return records
	}
	// A crash before the batched flush retains evidence for harmless idempotent replay.
	if saved := read()[key]; saved.Acked || len(saved.Events) != 1 || saved.Result.ID != result.ID {
		t.Fatal("unflushed ACK lost durable evidence")
	}
	if err := c.flushAcknowledgements(); err != nil {
		t.Fatal(err)
	}
	if saved := read()[key]; !saved.Acked || len(saved.Events) != 0 {
		t.Fatal("ACK batch was not persisted")
	}
}

func TestDeliveryBackpressureKeepsNewResultsAheadOfUnacknowledgedTelemetry(t *testing.T) {
	c := retentionFixture(t)
	job := protocol.Job{RunID: protocol.ID(), Role: "sender", Revision: "revision"}
	first, second := protocol.Event{ID: protocol.ID()}, protocol.Event{ID: protocol.ID()}
	r := &record{Job: job, Ready: true, Events: []protocol.Event{first}}
	c.records[recordKey(job.RunID, job.Role)] = r
	window := deliveryWindow{}
	for _, message := range c.pending() {
		if !window.shouldSend(message) {
			t.Fatal("initial message blocked")
		}
		window.sent(message)
	}
	// Appending events must not resend an overlapping batch while its head is unacknowledged.
	r.Events = append(r.Events, second)
	for _, message := range c.pending() {
		if window.shouldSend(message) {
			t.Fatalf("duplicate in-flight message: %s", message.Type)
		}
	}
	r.Result = &protocol.Result{ID: protocol.ID()}
	messages := c.pending()
	if messages[0].Type != "run_result" || !window.shouldSend(messages[0]) {
		t.Fatal("result delayed behind telemetry")
	}
	window.sent(messages[0])
	if err := c.handle(context.Background(), protocol.Message{Type: "telemetry_ack", RunID: job.RunID, Role: job.Role, Revision: job.Revision, EventIDs: []string{first.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, message := range c.pending() {
		if message.Type == "telemetry_batch" && (!window.shouldSend(message) || message.Events[0].ID != second.ID) {
			t.Fatal("ACK did not release the next batch")
		}
	}
	// A fresh connection must replay both the still-unacknowledged result and events.
	reconnected := deliveryWindow{}
	for _, message := range c.pending() {
		if !reconnected.shouldSend(message) {
			t.Fatal("reconnection lost durable delivery")
		}
	}
}

func TestDeliveryTracksLogStatusChangesAndDoesNotMarkFailedWrites(t *testing.T) {
	window := deliveryWindow{}
	message := protocol.Message{Type: "log_status", RunID: "run", Role: "pair", Logs: &protocol.LogRequest{ID: "request", Status: "COLLECTING"}}
	if !window.shouldSend(message) || !window.shouldSend(message) {
		t.Fatal("checking eligibility must not mark an unwritten message")
	}
	window.sent(message)
	if window.shouldSend(message) {
		t.Fatal("unchanged log status was resent")
	}
	message.Logs.Status = "UPLOADED"
	if !window.shouldSend(message) {
		t.Fatal("changed log status was suppressed")
	}
}
