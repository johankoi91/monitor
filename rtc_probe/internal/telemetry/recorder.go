// Package telemetry records real SDK calls and callbacks without media payloads.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

const MaxTraceBytes = 8 << 20

type window struct {
	Role, Name, Stage string
	Count, Rejected   uint64
	First, Last       time.Time
	Fields            map[string]any
}
type Recorder struct {
	job       protocol.Job
	mu        sync.Mutex
	worker    string
	started   time.Time
	seq       uint64
	secrets   []string
	queue     chan protocol.Event
	done      chan struct{}
	emit      func(protocol.Event)
	lastCalls map[string]string
	windows   map[string]*window
	err       error
	dropped   uint64
	closed    bool
}
type Span struct {
	ID, Role, Stage, Name string
	Started               time.Time
	Stream                bool
}

func New(dir, worker string, secrets []string, emit func(protocol.Event)) (*Recorder, error) {
	files := map[string]*os.File{}
	for _, role := range []string{"sender", "receiver"} {
		path := filepath.Join(dir, role)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(filepath.Join(path, "sdk-api.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			for _, opened := range files {
				opened.Close()
			}
			return nil, err
		}
		files[role] = f
	}
	r := &Recorder{worker: worker, started: time.Now(), secrets: secrets, queue: make(chan protocol.Event, 4096), done: make(chan struct{}), emit: emit, lastCalls: map[string]string{}, windows: map[string]*window{}}
	go r.write(files)
	return r, nil
}
func (r *Recorder) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}
func (r *Recorder) write(files map[string]*os.File) {
	defer close(r.done)
	defer func() {
		for _, f := range files {
			if err := f.Sync(); err != nil {
				r.fail(err)
			}
			if err := f.Close(); err != nil {
				r.fail(err)
			}
		}
	}()
	bytes := 0
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-r.queue:
			if !ok {
				return
			}
			data, err := json.Marshal(e)
			if err == nil && bytes+len(data)+1 > MaxTraceBytes {
				err = errors.New("SDK API trace capacity reached")
			}
			if err == nil {
				role, _ := e.Fields["endpoint_role"].(string)
				if files[role] == nil {
					err = errors.New("invalid SDK trace role")
				} else {
					_, err = files[role].Write(append(data, '\n'))
					bytes += len(data) + 1
				}
			}
			if err != nil {
				r.fail(err)
			}
		case <-timer.C:
			for _, f := range files {
				if err := f.Sync(); err != nil {
					r.fail(err)
				}
			}
		}
	}
}
func (r *Recorder) sanitize(value any, key string) any {
	switch strings.ToLower(key) {
	case "token", "sender_token", "receiver_token", "app_certificate", "secret", "authorization", "encryption_key":
		return "[REDACTED]"
	}
	switch v := value.(type) {
	case string:
		for _, s := range r.secrets {
			if s != "" {
				v = strings.ReplaceAll(v, s, "[REDACTED]")
			}
		}
		if len(v) > 2048 {
			v = v[:2048] + "…[truncated]"
		}
		return v
	case []byte:
		return map[string]any{"buffer_bytes": len(v), "payload_omitted": true}
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			out[k] = r.sanitize(x, k)
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, x := range v {
			out[i] = r.sanitize(x, "").(string)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = r.sanitize(x, "")
		}
		return out
	default:
		return value
	}
}
func (r *Recorder) record(role, kind, stage, name, call, related string, fields map[string]any, code *int, returnKind string, duration time.Duration, stream bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.seq++
	if fields == nil {
		fields = map[string]any{}
	}
	fields = r.sanitize(fields, "").(map[string]any)
	fields["endpoint_role"] = role
	e := protocol.Event{ID: protocol.ID(), WorkerID: r.worker, Seq: r.seq, At: time.Now().UTC(), ElapsedMS: time.Since(r.started).Milliseconds(), Kind: kind, Stage: stage, Name: name, CallID: call, RelatedCallID: related, ConnectionID: r.worker + "/" + role, ServiceID: r.worker + "/service", Fields: fields, Code: code, ReturnKind: returnKind, DurationMS: duration.Milliseconds(), DurationUS: duration.Microseconds()}
	e.ParentRunID = r.job.ParentRunID
	e.EdgeEndpoint = r.job.EdgeEndpoint
	e.RunID = r.job.RunID
	e.TaskID = r.job.TaskID
	e.TargetID = r.job.Target.ID
	e.RuntimeID = r.job.RuntimeID
	e.Revision = r.job.Revision
	e.Role = role
	select {
	case r.queue <- e:
	default:
		r.dropped++
	}
	forward := true
	if stream {
		key := role + "/" + name + "/" + kind
		w := r.windows[key]
		if w == nil {
			w = &window{Role: role, Name: name, Stage: stage, First: e.At}
			r.windows[key] = w
		}
		forward = w.Count == 0 || (code != nil && *code != 0)
		w.Count++
		w.Last = e.At
		w.Fields = fields
		if parameters, ok := fields["parameters"].(map[string]any); ok {
			if accepted, exists := parameters["accepted"].(bool); exists && !accepted {
				w.Rejected++
			}
			if first, _ := parameters["first_valid_frame"].(bool); first {
				forward = true
			}
		}
	}
	if forward && r.emit != nil {
		r.emit(e)
	}
}
func (r *Recorder) Begin(role, stage, name string, args map[string]any, stream bool) Span {
	s := Span{ID: protocol.ID(), Role: role, Stage: stage, Name: name, Started: time.Now(), Stream: stream}
	r.mu.Lock()
	r.lastCalls[role+"/"+name] = s.ID
	r.mu.Unlock()
	r.record(role, "api_call_begin", stage, name, s.ID, "", map[string]any{"args": args}, nil, "", 0, stream)
	return s
}
func (r *Recorder) End(s Span, kind string, code *int, result map[string]any) {
	r.record(s.Role, "api_call_end", s.Stage, s.Name, s.ID, "", map[string]any{"return_origin": "go_sdk", "result": result}, code, kind, time.Since(s.Started), s.Stream)
}
func (r *Recorder) Callback(role, stage, name, relatedAPI string, parameters map[string]any, stream bool) {
	r.mu.Lock()
	related := r.lastCalls[role+"/"+relatedAPI]
	r.mu.Unlock()
	r.record(role, "sdk_callback", stage, name, "", related, map[string]any{"parameters": parameters, "related_api": relatedAPI}, nil, "", 0, stream)
}
func (r *Recorder) Flush() {
	r.mu.Lock()
	windows := r.windows
	r.windows = map[string]*window{}
	r.mu.Unlock()
	for _, w := range windows {
		r.record(w.Role, "sdk_trace_summary", w.Stage, w.Name, "", "", map[string]any{"count": w.Count, "rejected": w.Rejected, "first_at": w.First, "last_at": w.Last, "last_parameters": w.Fields}, nil, "", 0, false)
	}
}
func (r *Recorder) Close() error {
	r.Flush()
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.dropped != 0 {
		return fmt.Errorf("SDK trace dropped %d events", r.dropped)
	}
	return nil
}
func (r *Recorder) Dropped() uint64 { r.mu.Lock(); defer r.mu.Unlock(); return r.dropped }

func (r *Recorder) Bind(job protocol.Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job.Credentials = nil
	r.job = job
}

func (r *Recorder) Event(role, kind, stage, name, call string, code *int, fields map[string]any, duration time.Duration) {
	r.record(role, kind, stage, name, call, "", fields, code, "", duration, false)
}
