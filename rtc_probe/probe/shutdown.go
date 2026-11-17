package probe

import (
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"time"
)

// Give each SDK worker its normal asynchronous leave/cleanup path before kill.
func (c *Client) shutdownWorkers() {
	c.mu.Lock()
	c.stopping = true
	for id, p := range c.workers {
		m := protocol.Msg("cancel_run")
		m.RunID = id
		for _, record := range c.records {
			if record.Job.RunID == id {
				m.Revision = record.Job.Revision
				m.Role = record.Job.Role
				if record.Job.Target.Driver == "agora" {
					m.Role = "pair"
				}
				break
			}
		}
		p.writeMu.Lock()
		_ = p.input.Encode(m)
		p.writeMu.Unlock()
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() { c.workersWG.Wait(); close(done) }()
	timer := time.NewTimer(protocol.CleanupGrace)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}
	c.mu.Lock()
	for _, p := range c.workers {
		p.cancel()
	}
	c.mu.Unlock()
	<-done
}
