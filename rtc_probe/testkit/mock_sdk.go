// Package testkit supplies mock RTC workers for isolated integration tests.
package testkit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type frame struct {
	Channel, UID                  string
	SampleRate, Channels, Samples int
	PCM                           []byte
}
type observer func(name string, fields map[string]any)
type failure struct{ Outcome, Reason string }

func (f *failure) Error() string { return f.Reason }
func classify(err error) (string, string) {
	var f *failure
	if errors.As(err, &f) {
		return f.Outcome, f.Reason
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "FAILED", "MEDIA_TIMEOUT"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED", "CANCELLED"
	}
	return "ERROR", "SDK_ADAPTER_ERROR"
}

type mockSDK struct {
	Job     protocol.Job
	Observe observer
}

func newMock(job protocol.Job, o observer) *mockSDK { return &mockSDK{Job: job, Observe: o} }
func (m *mockSDK) action(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fmt.Printf("[mock-sdk] %s run=%s role=%s\n", name, m.Job.RunID, m.Job.Role)
	return nil
}
func (m *mockSDK) Initialize(ctx context.Context) error {
	if e := m.action(ctx, "Initialize"); e != nil {
		return e
	}
	if m.Job.Scenario == "sdk_error" {
		return &failure{"ERROR", "MOCK_INITIALIZATION_ERROR"}
	}
	return nil
}
func (m *mockSDK) CreateConnection(ctx context.Context) error {
	return m.action(ctx, "NewRtcConnection")
}
func (m *mockSDK) ConfigureAccess(ctx context.Context) error {
	return m.action(ctx, "SetLocalAccessPoint")
}
func (m *mockSDK) RegisterObservers(ctx context.Context) error {
	return m.action(ctx, "RegisterObservers")
}
func (m *mockSDK) Connect(ctx context.Context) error {
	if e := m.action(ctx, "Connect"); e != nil {
		return e
	}
	m.Observe("OnConnecting", map[string]any{"channel": m.Job.Channel, "uid": m.Job.UID})
	if m.Job.Scenario == "join_failure" {
		m.Observe("OnConnectionFailure", map[string]any{"reason": "MOCK_AUTH_REJECTED"})
		return &failure{"FAILED", "MOCK_AUTH_REJECTED"}
	}
	m.Observe("OnConnected", map[string]any{"channel": m.Job.Channel, "uid": m.Job.UID})
	return nil
}
func (m *mockSDK) Subscribe(ctx context.Context, uid string) error {
	if e := m.action(ctx, "SubscribeAudio"); e != nil {
		return e
	}
	return nil
}
func (m *mockSDK) Publish(ctx context.Context) error {
	if e := m.action(ctx, "PublishAudio"); e != nil {
		return e
	}
	if m.Job.Scenario == "publish_failure" {
		return &failure{"FAILED", "MOCK_PUBLISH_FAILED"}
	}
	m.Observe("OnAudioTrackPublishSuccess", map[string]any{"driver": "mock"})
	return nil
}
func (m *mockSDK) PushPCM(ctx context.Context, pcm []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(pcm) != 320 {
		return &failure{"ERROR", "INVALID_PCM_INPUT"}
	}
	return nil
}
func tone() []byte {
	b := make([]byte, 320)
	for i := 0; i < 160; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*400*float64(i)/16000))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}
func (m *mockSDK) StartReceiving(ctx context.Context) <-chan frame {
	out := make(chan frame, 4)
	go func() {
		defer close(out)
		if m.Job.Scenario == "no_media" || m.Job.Scenario == "publish_failure" {
			<-ctx.Done()
			return
		}
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				select {
				case out <- frame{Channel: m.Job.Channel, UID: m.Job.PublisherUID, SampleRate: 16000, Channels: 1, Samples: 160, PCM: tone()}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
func (m *mockSDK) Disconnect(ctx context.Context) error {
	if e := m.action(ctx, "Disconnect"); e != nil {
		return e
	}
	if m.Job.Scenario == "cleanup_error" {
		return &failure{"ERROR", "MOCK_CLEANUP_ERROR"}
	}
	m.Observe("OnDisconnected", map[string]any{"expected": true})
	return nil
}
func (m *mockSDK) Release(ctx context.Context) error { return m.action(ctx, "Release") }
