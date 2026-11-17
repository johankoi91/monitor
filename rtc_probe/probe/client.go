// Package probe owns the persistent control connection and isolated workers.
package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/rtc-probe/durable"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type Config struct {
	URL, ID, Secret, DataDir string
	Driver                   string
	Capacity                 int
	AllowPlaintextLoopback   bool
	WorkerCommand            func(context.Context) *exec.Cmd
}
type record struct {
	WorkerID string           `json:"worker_id,omitempty"`
	Job      protocol.Job     `json:"job"`
	Ready    bool             `json:"ready"`
	Events   []protocol.Event `json:"pending_events"`
	Result   *protocol.Result `json:"result,omitempty"`
	Acked    bool             `json:"result_acked"`
}
type process struct {
	writeMu sync.Mutex
	input   *json.Encoder
	cancel  context.CancelFunc
	done    chan struct{}
}
type Client struct {
	storageBytes   int64
	collectActive  map[string]string
	nextLogAttempt map[string]time.Time
	logReports     map[string]protocol.LogRequest
	resourceError  string
	retention      protocol.RetentionPolicy
	lastPrune      time.Time
	config         Config
	mu             sync.Mutex
	records        map[string]*record
	workers        map[string]*process
	collecting     map[string]bool
	logsSlot       chan struct{}
	fatal          chan error
	workersWG      sync.WaitGroup
	logsWG         sync.WaitGroup
	stopping       bool
}

func New(c Config) (*Client, error) {
	u, e := url.Parse(c.URL)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v1/probe/connect" || u.Hostname() == "" {
		return nil, errors.New("credential-free probe websocket URL required")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "wss" && !(u.Scheme == "ws" && local && c.AllowPlaintextLoopback) {
		return nil, errors.New("WSS required; plaintext only allowed for explicit loopback development")
	}
	if !protocol.ValidID(c.ID) || len(c.Secret) < 24 || c.DataDir == "" {
		return nil, errors.New("independent probe ID, 24+ character secret and data directory required")
	}
	if c.Driver == "" {
		c.Driver = "mock"
	}
	if c.Capacity == 0 {
		c.Capacity = 2
	}
	if c.Capacity < 1 || c.Capacity > 8 {
		return nil, errors.New("invalid worker capacity")
	}
	if c.WorkerCommand == nil {
		executable, e := os.Executable()
		if e != nil {
			return nil, e
		}
		c.WorkerCommand = func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, executable, "worker")
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "RTC_PROBE_SECRET=") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			return cmd
		}
	}
	return &Client{config: c, records: map[string]*record{}, workers: map[string]*process{}, collecting: map[string]bool{}, logsSlot: make(chan struct{}, 1), fatal: make(chan error, 1)}, nil
}
func (c *Client) save() error {
	b, err := json.Marshal(c.records)
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return errors.New("probe outbox capacity exhausted")
	}
	return durable.Bytes(filepath.Join(c.config.DataDir, "outbox.json"), b)
}
func (c *Client) fail(err error) {
	select {
	case c.fatal <- err:
	default:
	}
}
func (c *Client) Run(parent context.Context) error {
	if e := os.MkdirAll(c.config.DataDir, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(c.config.DataDir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("another probe owns data directory")
	}
	ctx, cancel := context.WithCancel(parent)
	defer func() {
		cancel()
		c.shutdownWorkers()
		c.logsWG.Wait()
	}()
	if e = durable.Read(filepath.Join(c.config.DataDir, "outbox.json"), 16<<20, &c.records); e != nil && !os.IsNotExist(e) {
		return e
	}
	if c.records == nil {
		return errors.New("corrupt probe outbox")
	}
	c.retention = protocol.DefaultRetention()
	if err := durable.Read(filepath.Join(c.config.DataDir, "retention.json"), 4096, &c.retention); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := c.retention.Validate(); err != nil {
		return err
	}
	migrated := map[string]*record{}
	for id, r := range c.records {
		if r == nil || !protocol.ValidID(id) || (id != r.Job.RunID && id != recordKey(r.Job.RunID, r.Job.Role)) || r.Job.ProbeID != c.config.ID || (r.Job.Target.Driver != "mock" && r.Job.Target.Driver != "agora") {
			return errors.New("corrupt or foreign probe execution; preserve for repair")
		}
		if r.Result == nil {
			r.Result = &protocol.Result{ID: protocol.ID(), WorkerID: protocol.ID(), Driver: "mock", Outcome: "ERROR", Reason: "PROBE_PROCESS_RESTARTED", Cleanup: "UNKNOWN", FinishedAt: time.Now().UTC()}
			r.Result.Driver = r.Job.Target.Driver
		}
		migrated[recordKey(r.Job.RunID, r.Job.Role)] = r
	}
	c.records = migrated
	if e = c.save(); e != nil {
		return e
	}
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-timer.C:
				c.mu.Lock()
				err := c.pruneLocked(now.UTC(), 0)
				if err != nil {
					c.resourceError = "LOCAL_RESOURCE_EXHAUSTED"
					for _, p := range c.workers {
						p.cancel()
					}
				} else {
					c.resourceError = ""
				}
				c.mu.Unlock()
			}
		}
	}()
	defer func() { cancel(); <-maintenanceDone }()
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		header := http.Header{}
		req := &http.Request{Header: header}
		req.SetBasicAuth(c.config.ID, c.config.Secret)
		conn, resp, err := dialer.DialContext(ctx, c.config.URL, header)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err == nil {
			_ = c.session(ctx, conn)
			conn.Close()
			backoff = 250 * time.Millisecond
		} else {
			log.Print("RTC probe center unavailable; reconnecting")
		}
		select {
		case err := <-c.fatal:
			return err
		default:
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case err := <-c.fatal:
			t.Stop()
			return err
		case <-t.C:
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
	return nil
}
func (c *Client) session(ctx context.Context, conn *websocket.Conn) error {
	defer conn.Close()
	conn.SetReadLimit(protocol.MaxMessage)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})
	readErr := make(chan error, 1)
	go func() {
		for {
			kind, b, e := conn.ReadMessage()
			if e != nil {
				readErr <- e
				return
			}
			if kind != websocket.TextMessage {
				readErr <- errors.New("invalid websocket message")
				return
			}
			m, e := protocol.Decode(b)
			if e != nil || m.Version != protocol.Version {
				readErr <- errors.New("invalid probe protocol")
				return
			}
			if e = c.handle(ctx, m); e != nil {
				readErr <- e
				return
			}
		}
	}()
	write := func(m protocol.Message) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(m)
	}
	hello := protocol.Msg("hello")
	hello.Driver = c.config.Driver
	hello.ProbeVersion, hello.SDKVersion, hello.Platform = "0.4.0", sdkVersion(c.config.Driver), runtime.GOOS+"/"+runtime.GOARCH
	hello.Capacity = c.config.Capacity
	hello.ResourceError = c.resourceStatus()
	hello.StorageBytes = c.storageSize()
	hello.Capabilities = []string{"rtc_audio_v1", "rtc_video_v1", "retention_v1", "log_chunks_v1", "rtc_vos_list_v1"}
	if e := write(hello); e != nil {
		return e
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	type sentMessage struct {
		signature string
		at        time.Time
	}
	sent := map[string]sentMessage{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-readErr:
			return e
		case e := <-c.fatal:
			c.fail(e)
			return e
		case <-heartbeat.C:
			m := protocol.Msg("heartbeat")
			m.Driver = c.config.Driver
			m.ProbeVersion, m.SDKVersion, m.Platform = "0.4.0", sdkVersion(c.config.Driver), runtime.GOOS+"/"+runtime.GOARCH
			m.Capacity = c.config.Capacity
			m.ResourceError = c.resourceStatus()
			m.StorageBytes = c.storageSize()
			m.Capabilities = []string{"rtc_audio_v1", "rtc_video_v1", "retention_v1", "log_chunks_v1", "rtc_vos_list_v1"}
			if e := write(m); e != nil {
				return e
			}
		case <-tick.C:
			for _, m := range c.pending() {
				key := m.Type + "/" + m.RunID + "/" + m.Role
				signature := "ready"
				if m.Result != nil {
					signature = m.Result.ID
				} else if len(m.Events) > 0 {
					signature = m.Events[0].ID + "/" + m.Events[len(m.Events)-1].ID
				}
				previous := sent[key]
				if previous.signature == signature && time.Since(previous.at) < 2*time.Second {
					continue
				}
				if e := write(m); e != nil {
					return e
				}
				sent[key] = sentMessage{signature, time.Now()}
			}
		}
	}
}
func (c *Client) pending() []protocol.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []protocol.Message{}
	for _, report := range c.logReports {
		m := protocol.Msg("log_status")
		copy := report
		m.Logs = &copy
		m.RunID = report.RunID
		m.Role = report.Role
		m.ID = report.ID
		out = append(out, m)
	}
	for _, r := range c.records {
		base := func(kind string) protocol.Message {
			m := protocol.Msg(kind)
			m.RunID = r.Job.RunID
			m.Revision = r.Job.Revision
			m.Role = r.Job.Role
			return m
		}
		if r.Ready && r.Result == nil {
			out = append(out, base("run_ready"))
		}
		if r.Result != nil && !r.Acked {
			m := base("run_result")
			v := *r.Result
			m.Result = &v
			out = append(out, m)
		}
		if len(r.Events) > 0 {
			m := base("telemetry_batch")
			n := len(r.Events)
			if n > 64 {
				n = 64
			}
			m.Events = append([]protocol.Event(nil), r.Events[:n]...)
			out = append(out, m)
		}
	}
	return out
}
func (c *Client) handle(ctx context.Context, m protocol.Message) error {
	if m.Type == "log_status_ack" {
		c.mu.Lock()
		delete(c.logReports, m.ID)
		c.mu.Unlock()
		return nil
	}
	if m.Type == "configure_probe" {
		if m.Retention == nil {
			return errors.New("retention policy required")
		}
		if err := m.Retention.Validate(); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.retention.Version == m.Retention.Version {
			return nil
		}
		if err := durable.Write(filepath.Join(c.config.DataDir, "retention.json"), m.Retention); err != nil {
			return err
		}
		c.retention = *m.Retention
		return nil
	}
	if m.Type == "prepare_run" {
		if m.Job == nil {
			return errors.New("job required")
		}
		return c.prepare(ctx, *m.Job)
	}
	if m.Type == "collect_logs" {
		if m.Logs == nil {
			return errors.New("log request required")
		}
		c.collect(ctx, *m.Logs)
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	role := m.Role
	if role == "pair" {
		role = "sender"
	}
	r := c.records[recordKey(m.RunID, role)]
	if r == nil {
		return nil
	}
	if (m.Role != r.Job.Role && m.Role != "pair") || m.Revision != r.Job.Revision {
		return errors.New("execution revision or role mismatch")
	}
	switch m.Type {
	case "telemetry_ack":
		acked := map[string]bool{}
		for _, id := range m.EventIDs {
			acked[id] = true
		}
		left := make([]protocol.Event, 0, len(r.Events))
		for _, e := range r.Events {
			if !acked[e.ID] {
				left = append(left, e)
			}
		}
		r.Events = left
		return c.save()
	case "result_ack":
		if r.Result == nil || r.Result.ID != m.ID {
			return errors.New("result acknowledgement mismatch")
		}
		r.Acked = true
		return c.save()
	case "start_media", "finish_run", "cancel_run":
		if p := c.workers[m.RunID]; p != nil {
			p.writeMu.Lock()
			e := p.input.Encode(m)
			p.writeMu.Unlock()
			if e != nil {
				return e
			}
			if m.Type == "cancel_run" {
				go func() {
					t := time.NewTimer(protocol.CleanupGrace)
					defer t.Stop()
					select {
					case <-p.done:
					case <-t.C:
						p.cancel()
					}
				}()
			}
		}
	default:
		return errors.New("unknown control message")
	}
	return nil
}
func (c *Client) prepare(ctx context.Context, job protocol.Job) error {
	if job.ProbeID != c.config.ID {
		return errors.New("assignment owner mismatch")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopping || ctx.Err() != nil {
		return context.Canceled
	}
	if !protocol.ValidID(job.RunID) || job.ProbeID != c.config.ID || job.Target.Driver != c.config.Driver || (job.Role != "sender" && job.Role != "receiver" && job.Role != "pair") || job.Revision == "" || job.Deadline.Before(time.Now()) {
		return errors.New("invalid mock assignment")
	}
	roles := []string{job.Role}
	if job.Role == "pair" {
		roles = []string{"sender", "receiver"}
	}
	clean := job
	clean.Credentials = nil
	clean.Role = roles[0]
	if old := c.records[recordKey(job.RunID, roles[0])]; old != nil {
		a, _ := json.Marshal(old.Job)
		b, _ := json.Marshal(clean)
		if string(a) != string(b) {
			return errors.New("conflicting assignment")
		}
		return nil
	}
	if len(c.workers) >= c.config.Capacity {
		return errors.New("worker capacity exhausted")
	}
	if err := c.pruneLocked(time.Now().UTC(), reserveFor(job)); err != nil {
		c.resourceError = "LOCAL_RESOURCE_EXHAUSTED"
		for _, role := range roles {
			v := job
			v.Credentials = nil
			v.Role = role
			c.records[recordKey(job.RunID, role)] = &record{Job: v, Events: []protocol.Event{}, Result: &protocol.Result{ID: protocol.ID(), WorkerID: protocol.ID(), Driver: job.Target.Driver, Outcome: "ERROR", Reason: "LOCAL_RESOURCE_EXHAUSTED", Cleanup: "NOT_STARTED", FinishedAt: time.Now().UTC()}}
		}
		if err := c.save(); err != nil {
			c.fail(err)
			return err
		}
		return nil
	}
	c.resourceError = ""

	for _, role := range roles {
		v := job
		v.Credentials = nil
		v.Role = role
		if job.RTC != nil && role == "receiver" {
			v.UID = strconv.FormatUint(uint64(job.RTC.ReceiverUID), 10)
		}
		c.records[recordKey(job.RunID, role)] = &record{Job: v, Events: []protocol.Event{}}
	}
	if e := c.save(); e != nil {
		for _, role := range roles {
			delete(c.records, recordKey(job.RunID, role))
		}
		c.fail(e)
		return e
	}
	dir := c.runDir(job.RunID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		c.fail(e)
		return e
	}
	workerCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), job.Deadline.Add(protocol.CleanupGrace))
	cmd := c.config.WorkerCommand(workerCtx)
	cmd.WaitDelay = time.Second
	read, write, e := os.Pipe()
	if e != nil {
		cancel()
		return e
	}
	cmd.ExtraFiles = []*os.File{write}
	stdin, e := cmd.StdinPipe()
	if e != nil {
		read.Close()
		write.Close()
		cancel()
		return e
	}
	stdout, e := os.OpenFile(filepath.Join(dir, "process.stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		read.Close()
		write.Close()
		stdin.Close()
		cancel()
		return e
	}
	stderr, e := os.OpenFile(filepath.Join(dir, "process.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		stdout.Close()
		read.Close()
		write.Close()
		stdin.Close()
		cancel()
		return e
	}
	cmd.Stdout = &limitedWriter{w: stdout, remaining: 1 << 20}
	cmd.Stderr = &limitedWriter{w: stderr, remaining: 1 << 20}
	p := &process{input: json.NewEncoder(stdin), cancel: cancel, done: make(chan struct{})}
	c.workers[job.RunID] = p
	startErr := cmd.Start()
	write.Close()
	if startErr == nil {
		initial := protocol.Msg("prepare_run")
		job.WorkDir = dir
		initial.Job = &job
		startErr = p.input.Encode(initial)
	}
	c.workersWG.Add(1)
	go func() {
		defer c.workersWG.Done()
		defer close(p.done)
		defer cancel()
		defer read.Close()
		defer stdin.Close()
		defer stdout.Close()
		defer stderr.Close()
		if startErr == nil {
			decoder := json.NewDecoder(read)
			for {
				var m protocol.Message
				if e := decoder.Decode(&m); e != nil {
					break
				}
				if e := c.recordMessage(job, m); e != nil {
					c.fail(e)
					cancel()
					break
				}
			}
		}
		if cmd.Process != nil {
			_ = cmd.Wait()
		}
		exit := processExit(cmd, startErr, workerCtx)
		_ = durable.Write(filepath.Join(dir, "process.exit.json"), exit)
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.workers, job.RunID)
		for _, role := range roles {
			rec := c.records[recordKey(job.RunID, role)]
			if rec.Result == nil {
				workerID := rec.WorkerID
				if workerID == "" {
					workerID = protocol.ID()
				}
				rec.Result = &protocol.Result{ID: protocol.ID(), WorkerID: workerID, Driver: job.Target.Driver, Outcome: "ERROR", Reason: "WORKER_EXITED_WITHOUT_RESULT", Cleanup: "UNKNOWN", FinishedAt: time.Now().UTC(), ProcessExit: &exit}
				if !exit.Started {
					rec.Result.Reason = "WORKER_START_FAILED"
				} else if exit.DeadlineExceeded {
					rec.Result.Reason = "WORKER_DEADLINE_EXCEEDED"
				} else if exit.Signal != "" {
					rec.Result.Reason = "SDK_PROCESS_CRASHED"
				}
				if c.resourceError != "" {
					rec.Result.Reason = "LOCAL_RESOURCE_EXHAUSTED"
				}
				_ = durable.Write(filepath.Join(c.endpointDir(job.RunID, role), "result.json"), rec.Result)
			}
		}
		if e := c.save(); e != nil {
			c.fail(e)
		}
	}()
	return nil
}
func (c *Client) runDir(id string) string { return filepath.Join(c.config.DataDir, "runs", id) }
func (c *Client) recordMessage(job protocol.Job, m protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.records[recordKey(job.RunID, m.Role)]
	if r == nil || m.RunID != job.RunID || m.Revision != job.Revision || (m.Role != job.Role && job.Role != "pair") {
		return errors.New("worker evidence identity mismatch")
	}
	if err := os.MkdirAll(c.endpointDir(job.RunID, m.Role), 0700); err != nil {
		return err
	}
	switch m.Type {
	case "run_ready":
		r.Ready = true
	case "telemetry_batch":
		if r.WorkerID == "" && len(m.Events) > 0 {
			r.WorkerID = m.Events[0].WorkerID
		}
		if len(r.Events)+len(m.Events) > 512 {
			return errors.New("local telemetry capacity reached")
		}
		r.Events = append(r.Events, m.Events...)
		f, e := os.OpenFile(filepath.Join(c.endpointDir(job.RunID, m.Role), "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if e != nil {
			return e
		}
		for _, ev := range m.Events {
			if e = json.NewEncoder(f).Encode(ev); e != nil {
				f.Close()
				return e
			}
		}
		e = f.Sync()
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
	case "run_result":
		if m.Result == nil {
			return errors.New("worker result missing")
		}
		r.Result = m.Result
		if e := durable.Write(filepath.Join(c.endpointDir(job.RunID, m.Role), "result.json"), m.Result); e != nil {
			return e
		}
	default:
		return fmt.Errorf("invalid worker message")
	}
	return c.save()
}

type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (w *limitedWriter) Write(b []byte) (int, error) {
	n := len(b)
	if w.remaining > 0 {
		keep := int64(n)
		if keep > w.remaining {
			keep = w.remaining
		}
		written, e := w.w.Write(b[:keep])
		w.remaining -= int64(written)
		if e != nil {
			return written, e
		}
	}
	return n, nil
}

func (c *Client) UploadURL(id string) string {
	u, _ := url.Parse(c.config.URL)
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = "/api/v1/probe/uploads/" + id
	return u.String()
}
func (c *Client) safeText(b []byte) []byte {
	text := strings.ReplaceAll(string(b), c.config.Secret, "[REDACTED]")
	text = regexp.MustCompile(`00[67][A-Za-z0-9+/=]{40,}`).ReplaceAllString(text, "[REDACTED_TOKEN]")
	return []byte(text)
}

func recordKey(run, role string) string               { return run + "--" + role }
func (c *Client) endpointDir(run, role string) string { return filepath.Join(c.runDir(run), role) }
func sdkVersion(driver string) string {
	if driver == "agora" {
		return "Go-2.6.6/native-4.6.31.135"
	}
	return "mock/1"
}
