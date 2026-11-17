package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func TestWorkerBurstsPersistEveryEventAndPrioritizeControl(t *testing.T) {
	c := retentionFixture(t)
	job := protocol.Job{RunID: protocol.ID(), Revision: "revision", Role: "pair"}
	for _, role := range []string{"sender", "receiver"} {
		copy := job
		copy.Role = role
		c.records[recordKey(job.RunID, role)] = &record{Job: copy}
	}
	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	for i := 0; i < 80; i++ {
		for _, role := range []string{"sender", "receiver"} {
			m := protocol.Msg("telemetry_batch")
			m.RunID = job.RunID
			m.Revision = job.Revision
			m.Role = role
			m.Events = []protocol.Event{{ID: protocol.ID(), WorkerID: "worker", Seq: uint64(i + 1), Name: "SetParameters", Fields: map[string]any{"args": map[string]any{"parameters": "fixed-route"}}}}
			if err := enc.Encode(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, role := range []string{"sender", "receiver"} {
		m := protocol.Msg("run_ready")
		m.RunID = job.RunID
		m.Revision = job.Revision
		m.Role = role
		enc.Encode(m)
	}
	if err := c.readWorkerMessages(context.Background(), job, &input); err != nil {
		t.Fatal(err)
	}
	var saved map[string]*record
	raw, err := os.ReadFile(filepath.Join(c.config.DataDir, "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"sender", "receiver"} {
		r := saved[recordKey(job.RunID, role)]
		if !r.Ready || len(r.Events) != 80 {
			t.Fatal("uncommitted readiness or missing events", r)
		}
		f, err := os.Open(filepath.Join(c.endpointDir(job.RunID, role), "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		d := json.NewDecoder(f)
		n := 0
		for {
			var event protocol.Event
			err := d.Decode(&event)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			n++
			if event.Seq != uint64(n) || event.Fields["args"] == nil {
				t.Fatal("event order or arguments lost", event)
			}
		}
		f.Close()
		if n != 80 {
			t.Fatal(n)
		}
	}
	pending := c.pending()
	if len(pending) != 4 || pending[0].Type != "run_ready" || pending[1].Type != "run_ready" {
		t.Fatal("telemetry delayed readiness", pending)
	}
	result := protocol.Msg("run_result")
	result.RunID = job.RunID
	result.Revision = job.Revision
	result.Role = "sender"
	result.Result = &protocol.Result{ID: protocol.ID(), FinishedAt: time.Now()}
	if err := c.recordMessage(job, result); err != nil {
		t.Fatal(err)
	}
	pending = c.pending()
	if pending[0].Type != "run_ready" || pending[1].Type != "run_result" {
		t.Fatal("telemetry delayed result")
	}
}
