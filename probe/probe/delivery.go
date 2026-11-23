package probe

import (
	"encoding/json"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

// WebSocket delivery is ordered and reliable while a connection stays open.
// Keep one event batch in flight per endpoint; reconnect creates a new window
// and replays every unacknowledged durable message.
type deliveryWindow map[string]string

// ACK state can be coalesced: after a crash, replaying an already acknowledged
// immutable event/result is safe. New worker evidence is still saved before send.
func (c *Client) flushAcknowledgements() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ackDirty {
		return nil
	}
	return c.save()
}

func deliveryKey(message protocol.Message) string {
	return message.Type + "/" + message.RunID + "/" + message.Role
}

func deliverySignature(message protocol.Message) string {
	switch message.Type {
	case "run_ready":
		return "ready"
	case "run_result":
		if message.Result != nil {
			return message.Result.ID
		}
	case "telemetry_batch":
		if len(message.Events) > 0 {
			return message.Events[0].ID
		}
	case "log_status":
		data, _ := json.Marshal(message.Logs)
		return string(data)
	}
	return message.ID
}

func (window deliveryWindow) shouldSend(message protocol.Message) bool {
	previous, sent := window[deliveryKey(message)]
	return !sent || previous != deliverySignature(message)
}

func (window deliveryWindow) sent(message protocol.Message) {
	window[deliveryKey(message)] = deliverySignature(message)
}

// Bound the entire connection's telemetry backlog, not only each endpoint's.
// Otherwise many restored executions can fill the socket and center ACK queue,
// delaying readiness/results for current workers until their deadlines expire.
const telemetryFlightLimit = 4

func (window deliveryWindow) batch(pending []protocol.Message) []protocol.Message {
	inFlight := 0
	for _, message := range pending {
		if message.Type == "telemetry_batch" && !window.shouldSend(message) {
			inFlight++
		}
	}
	out := make([]protocol.Message, 0, len(pending))
	for _, message := range pending {
		if !window.shouldSend(message) {
			continue
		}
		if message.Type == "telemetry_batch" {
			if inFlight >= telemetryFlightLimit {
				continue
			}
			inFlight++
		}
		out = append(out, message)
	}
	return out
}
