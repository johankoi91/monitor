package testkit

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync/atomic"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type recorder struct {
	job     protocol.Job
	id      string
	started time.Time
	seq     uint64
	out     chan protocol.Message
	dropped atomic.Uint64
}

func (r *recorder) msg(kind string) protocol.Message {
	m := protocol.Msg(kind)
	m.RunID = r.job.RunID
	m.Revision = r.job.Revision
	m.Role = r.job.Role
	return m
}
func (r *recorder) event(kind, stage, name, call string, code *int, fields map[string]any, duration int64) {
	r.seq++
	ev := protocol.Event{ID: protocol.ID(), WorkerID: r.id, Seq: r.seq, At: time.Now().UTC(), ElapsedMS: time.Since(r.started).Milliseconds(), Kind: kind, Stage: stage, Name: name, CallID: call, Code: code, Fields: fields, DurationMS: duration}
	if kind == "api_call_end" {
		ev.ReturnKind = "int"
		if name == "NewRtcConnection" {
			ev.ReturnKind = "object"
			ev.Code = nil
		}
	}
	m := r.msg("telemetry_batch")
	m.Events = []protocol.Event{ev}
	select {
	case r.out <- m:
	default:
		r.dropped.Add(1)
	}
}
func (r *recorder) call(stage, name string, fn func() error) error {
	id := protocol.ID()
	at := time.Now()
	r.event("api_call_begin", stage, name, id, nil, nil, 0)
	err := fn()
	code := 0
	if err != nil {
		code = -1
	}
	fields := map[string]any{"driver": "mock", "return_origin": "mock"}
	if err != nil {
		fields["reason"] = err.Error()
	}
	r.event("api_call_end", stage, name, id, &code, fields, time.Since(at).Milliseconds())
	return err
}

// Run executes an isolated mock worker over a private JSON pipe.
func runRole(ctx context.Context, input io.Reader, output io.Writer) error {
	d := json.NewDecoder(io.LimitReader(input, 1<<20))
	var initial protocol.Message
	if err := d.Decode(&initial); err != nil {
		return err
	}
	if initial.Type != "prepare_run" || initial.Job == nil {
		return errors.New("initial job required")
	}
	job := *initial.Job
	if !protocol.ValidID(job.RunID) || !protocol.ValidID(job.ProbeID) || job.Target.Driver != "mock" || job.Deadline.Before(time.Now()) || job.MediaDurationMS < 1000 || job.MediaDurationMS > 10000 {
		return errors.New("invalid mock job")
	}
	ctx, cancel := context.WithDeadline(ctx, job.Deadline)
	defer cancel()
	start := make(chan struct{}, 1)
	finish := make(chan struct{}, 1)
	go func() {
		for {
			var m protocol.Message
			if d.Decode(&m) != nil {
				cancel()
				return
			}
			if m.RunID != job.RunID || m.Revision != job.Revision {
				continue
			}
			switch m.Type {
			case "start_media":
				select {
				case start <- struct{}{}:
				default:
				}
			case "finish_run":
				select {
				case finish <- struct{}{}:
				default:
				}
			case "cancel_run":
				cancel()
			}
		}
	}()
	r := &recorder{job: job, id: protocol.ID(), started: time.Now(), out: make(chan protocol.Message, 128)}
	written := make(chan error, 1)
	go func() {
		enc := json.NewEncoder(output)
		var firstError error
		for m := range r.out {
			if firstError == nil {
				if e := enc.Encode(m); e != nil {
					firstError = e
					cancel()
				}
			}
		}
		written <- firstError
	}()
	evidence := protocol.Evidence{}
	stage := "INITIALIZE"
	observe := func(name string, fields map[string]any) { r.event("sdk_callback", stage, name, "", nil, fields, 0) }
	driver := newMock(job, observe)
	r.event("worker_event", stage, "worker_started", "", nil, map[string]any{"driver": "mock", "simulation_only": true}, 0)
	err := func() error {
		for _, step := range []struct {
			stage, name string
			fn          func(context.Context) error
		}{{"INITIALIZE", "Initialize", driver.Initialize}, {"CREATE_CONNECTION", "NewRtcConnection", driver.CreateConnection}, {"CONFIGURE_ACCESS", "SetLocalAccessPoint", driver.ConfigureAccess}, {"REGISTER", "RegisterObservers", driver.RegisterObservers}, {"JOIN", "Connect", driver.Connect}} {
			stage = step.stage
			if e := r.call(stage, step.name, func() error { return step.fn(ctx) }); e != nil {
				return e
			}
		}
		evidence.Connected = true
		if job.Role == "receiver" {
			stage = "SUBSCRIBE"
			if e := r.call(stage, "LocalUser.SubscribeAudio", func() error { return driver.Subscribe(ctx, job.PublisherUID) }); e != nil {
				return e
			}
			evidence.Subscribed = true
		}
		r.out <- r.msg("run_ready")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-start:
		}
		stage = "MEDIA"
		started := time.Now()
		summary := time.NewTicker(time.Second)
		defer summary.Stop()
		publishSummary := func() {
			r.event("media_summary", stage, "media_window", "", nil, map[string]any{"driver": "mock", "sent_frames": evidence.SentFrames, "sent_bytes": evidence.SentBytes, "received_frames": evidence.ReceivedFrames, "valid_samples": evidence.ValidSamples}, 0)
		}
		if job.Role == "sender" {
			stage = "PUBLISH"
			if e := r.call("PUBLISH", "PublishAudio", func() error { return driver.Publish(ctx) }); e != nil {
				return e
			}
			evidence.Published = true
			stage = "MEDIA"
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			end := time.NewTimer(time.Duration(job.MediaDurationMS) * time.Millisecond)
			defer end.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-finish:
					publishSummary()
					return nil
				case <-end.C:
					publishSummary()
					return nil
				case <-summary.C:
					publishSummary()
				case <-tick.C:
					var e error
					pcm := tone()
					if evidence.SentFrames == 0 {
						e = r.call(stage, "PushAudioPcmData", func() error { return driver.PushPCM(ctx, pcm) })
					} else {
						e = driver.PushPCM(ctx, pcm)
					}
					if e != nil {
						return e
					}
					evidence.SentFrames++
					evidence.SentBytes += len(pcm)
				}
			}
		}
		frames := driver.StartReceiving(ctx)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-finish:
				if evidence.ValidSamples < 16000 {
					return &failure{Outcome: "FAILED", Reason: "MEDIA_EVIDENCE_MISSING"}
				}
				publishSummary()
				return nil
			case <-summary.C:
				publishSummary()
			case frame, ok := <-frames:
				if !ok {
					if err := ctx.Err(); err != nil {
						return err
					}
					return &failure{Outcome: "ERROR", Reason: "FRAME_SOURCE_CLOSED"}
				}
				if frame.Channel != job.Channel || frame.UID != job.PublisherUID || frame.SampleRate != 16000 || frame.Channels != 1 || frame.Samples <= 0 || len(frame.PCM) != frame.Samples*2 {
					continue
				}
				energy := 0.0
				for i := 0; i < len(frame.PCM); i += 2 {
					v := float64(int16(binary.LittleEndian.Uint16(frame.PCM[i:])))
					energy += v * v
				}
				if math.Sqrt(energy/float64(frame.Samples)) < 100 {
					continue
				}
				if evidence.ReceivedFrames == 0 {
					evidence.FirstFrameMS = time.Since(r.started).Milliseconds()
					observe("OnUserAudioTrackSubscribed", map[string]any{"uid": job.PublisherUID})
					observe("OnPlaybackAudioFrameBeforeMixing", map[string]any{"uid": frame.UID, "samples": frame.Samples, "sample_rate": frame.SampleRate, "first_valid_frame": true})
				}
				evidence.ReceivedFrames++
				evidence.ValidSamples += frame.Samples
				evidence.MediaDurationMS = evidence.ValidSamples / 16
				if evidence.MediaDurationMS >= 1000 && time.Since(started) >= time.Second {
					publishSummary()
					return nil
				}
			}
		}
	}()
	outcome, reason := "SUCCESS", "SIMULATION_ONLY"
	if err != nil {
		outcome, reason = classify(err)
	}
	stage = "CLEANUP"
	cleanupCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	cleanup := "SUCCESS"
	if e := r.call(stage, "Disconnect", func() error { return driver.Disconnect(cleanupCtx) }); e != nil {
		cleanup = "FAILED"
	}
	if e := r.call(stage, "Release", func() error { return driver.Release(cleanupCtx) }); e != nil {
		cleanup = "FAILED"
	}
	if r.dropped.Load() > 0 {
		outcome = "ERROR"
		reason = "LOCAL_TELEMETRY_OVERFLOW"
	}
	result := &protocol.Result{ID: protocol.ID(), WorkerID: r.id, Driver: "mock", Outcome: outcome, Reason: reason, Cleanup: cleanup, Evidence: evidence, FinishedAt: time.Now().UTC()}
	m := r.msg("run_result")
	m.Result = result
	r.out <- m
	close(r.out)
	return <-written
}
