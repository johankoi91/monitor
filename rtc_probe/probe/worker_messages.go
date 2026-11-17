package probe

import (
	"context"
	"encoding/json"
	"io"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

func (c *Client) recordMessage(job protocol.Job, m protocol.Message) error {
	return c.recordMessages(job, []protocol.Message{m})
}

// Commit a decoded burst atomically before publishing readiness or evidence.
// SDK API files retain every original call and callback. This only coalesces
// durable outbox commits and appends to each role's event file.
func (c *Client) recordMessages(job protocol.Job, messages []protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	merged := make([]protocol.Message, 0, len(messages))
	positions := map[string]int{}
	for _, m := range messages {
		if m.Type == "telemetry_batch" {
			key := m.RunID + "/" + m.Revision + "/" + m.Role
			if i, ok := positions[key]; ok {
				merged[i].Events = append(merged[i].Events, m.Events...)
				continue
			}
			positions[key] = len(merged)
			m.Events = append([]protocol.Event(nil), m.Events...)
		}
		merged = append(merged, m)
	}
	for _, m := range merged {
		if err := c.recordWorkerMessage(job, m); err != nil {
			return err
		}
	}
	return c.save()
}

func (c *Client) readWorkerMessages(ctx context.Context, job protocol.Job, input io.Reader) error {
	messages := make(chan protocol.Message, 128)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(messages)
		decoder := json.NewDecoder(input)
		for {
			var m protocol.Message
			if decoder.Decode(&m) != nil {
				return
			}
			select {
			case messages <- m:
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	for first := range messages {
		batch := []protocol.Message{first}
	collect:
		for len(batch) < 128 {
			select {
			case m, ok := <-messages:
				if !ok {
					break collect
				}
				batch = append(batch, m)
			default:
				break collect
			}
		}
		if err := c.recordMessages(job, batch); err != nil {
			return err
		}
	}
	return nil
}
