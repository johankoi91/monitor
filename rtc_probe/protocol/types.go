// Package protocol defines the RTC probe control and evidence contract.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"
)

const ProductVersion = "V1.0"
const Version = 2
const MaxMessage = 256 << 10
const MaxBundle = 8 << 20
const LogChunkBytes = 256 << 10
const CleanupGrace = 10 * time.Second
const ResultReportGrace = 5 * time.Second

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$`)

func ValidID(s string) bool { return identifier.MatchString(s) }
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Target struct {
	RTC               *RTCParameters  `json:"rtc,omitempty" yaml:"rtc"`
	Credentials       *AppCredentials `json:"credentials,omitempty" yaml:"-"`
	CredentialVersion string          `json:"credential_version,omitempty" yaml:"-"`
	HasCertificate    bool            `json:"has_certificate,omitempty" yaml:"-"`
	ID                string          `json:"target_id" yaml:"target_id"`
	Name              string          `json:"name" yaml:"name"`
	ClusterCode       string          `json:"cluster_code,omitempty" yaml:"cluster_code"`
	Driver            string          `json:"driver" yaml:"driver"`
}
type Task struct {
	Name                 string          `json:"name,omitempty" yaml:"name,omitempty"`
	ClientType           string          `json:"client_type" yaml:"client_type"`
	ID                   string          `json:"task_id" yaml:"task_id"`
	TargetID             string          `json:"target_id" yaml:"target_id"`
	HostID               string          `json:"host_id" yaml:"host_id"`
	ServiceType          string          `json:"service_type" yaml:"service_type"`
	Enabled              bool            `json:"enabled" yaml:"enabled"`
	SweepIntervalSeconds int             `json:"sweep_interval_seconds,omitempty" yaml:"sweep_interval_seconds"`
	CrossIntervalSeconds int             `json:"cross_interval_seconds,omitempty" yaml:"cross_interval_seconds"`
	IntervalSeconds      int             `json:"interval_seconds" yaml:"interval_seconds"`
	TimeoutSeconds       int             `json:"timeout_seconds" yaml:"timeout_seconds"`
	Scenario             string          `json:"-" yaml:"-"`
	RTC                  *RTCParameters  `json:"-" yaml:"-"`
	Credentials          *AppCredentials `json:"-" yaml:"-"`
	CredentialVersion    string          `json:"-" yaml:"-"`
	HasCertificate       bool            `json:"-" yaml:"-"`
	FailureThreshold     int             `json:"failure_threshold" yaml:"failure_threshold"`
	RecoveryThreshold    int             `json:"recovery_threshold" yaml:"recovery_threshold"`
	UnknownNotifySeconds int             `json:"unknown_notify_seconds" yaml:"unknown_notify_seconds"`
	Description          string          `json:"description,omitempty" yaml:"description"`
}

const SharedServiceLogsCapability = "shared_service_logs_v1"
const ServiceLogScope = "service"

type Job struct {
	HostID          string         `json:"host_id"`
	ServiceType     string         `json:"service_type"`
	ClientType      string         `json:"client_type"`
	EdgeFormat      string         `json:"edge_format,omitempty"`
	LogScope        string         `json:"log_scope,omitempty"`
	NativeCase      *NativeCase    `json:"native_case,omitempty"`
	ParentRunID     string         `json:"parent_run_id,omitempty"`
	EdgeEndpoint    string         `json:"edge_endpoint,omitempty"`
	RuntimeLocation string         `json:"runtime_location,omitempty"`
	RuntimeVersion  string         `json:"runtime_version,omitempty"`
	SDKVersion      string         `json:"sdk_version,omitempty"`
	Platform        string         `json:"platform,omitempty"`
	RunID           string         `json:"run_id"`
	TaskID          string         `json:"task_id"`
	Target          Target         `json:"target"`
	Revision        string         `json:"config_revision"`
	RuntimeID       string         `json:"runtime_id"`
	Role            string         `json:"endpoint_role"`
	Channel         string         `json:"channel"`
	UID             string         `json:"uid"`
	PublisherUID    string         `json:"publisher_uid"`
	Scenario        string         `json:"mock_scenario"`
	Deadline        time.Time      `json:"deadline"`
	MediaDurationMS int            `json:"media_duration_ms"`
	RTC             *RTCParameters `json:"rtc,omitempty"`
	Credentials     *Credentials   `json:"credentials,omitempty"`
	WorkDir         string         `json:"work_dir,omitempty"`
	MediaPath       string         `json:"media_path,omitempty"`
}

func (j Job) Duplex() bool { return j.NativeCase != nil && j.NativeCase.Duplex }

type Event struct {
	ParentRunID   string         `json:"parent_run_id,omitempty"`
	EdgeEndpoint  string         `json:"edge_endpoint,omitempty"`
	RunID         string         `json:"run_id,omitempty"`
	TaskID        string         `json:"task_id,omitempty"`
	TargetID      string         `json:"target_id,omitempty"`
	RuntimeID     string         `json:"runtime_id,omitempty"`
	Revision      string         `json:"config_revision,omitempty"`
	Role          string         `json:"endpoint_role,omitempty"`
	ID            string         `json:"event_id"`
	WorkerID      string         `json:"worker_id"`
	Seq           uint64         `json:"source_seq"`
	At            time.Time      `json:"occurred_at"`
	ElapsedMS     int64          `json:"monotonic_elapsed_ms"`
	Kind          string         `json:"kind"`
	Stage         string         `json:"stage"`
	Name          string         `json:"name,omitempty"`
	CallID        string         `json:"call_id,omitempty"`
	RelatedCallID string         `json:"related_call_id,omitempty"`
	ConnectionID  string         `json:"connection_id,omitempty"`
	ServiceID     string         `json:"service_id,omitempty"`
	ReturnKind    string         `json:"return_kind,omitempty"`
	Code          *int           `json:"return_code,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	DurationUS    int64          `json:"duration_us,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
}
type Evidence struct {
	BrowserMediaValidated bool  `json:"browser_media_validated,omitempty"`
	Connected             bool  `json:"connected"`
	Published             bool  `json:"published"`
	Subscribed            bool  `json:"subscribed"`
	SentFrames            int   `json:"sent_frames"`
	SentBytes             int   `json:"sent_bytes"`
	ReceivedFrames        int   `json:"received_frames"`
	ValidSamples          int   `json:"valid_samples"`
	MediaDurationMS       int   `json:"media_duration_ms"`
	FirstFrameMS          int64 `json:"first_frame_elapsed_ms,omitempty"`
	VideoPublished        bool  `json:"video_published"`
	VideoSubscribed       bool  `json:"video_subscribed"`
	SentVideoFrames       int   `json:"sent_video_frames"`
	ReceivedVideoFrames   int   `json:"received_video_frames"`
	VideoWidth            int   `json:"video_width"`
	VideoHeight           int   `json:"video_height"`
	FirstVideoMS          int64 `json:"first_video_elapsed_ms,omitempty"`
}
type BrowserEvidence struct {
	SignalingInventoryKnown    bool         `json:"signaling_inventory_known"`
	SignalingFallbackAttempted bool         `json:"signaling_fallback_attempted"`
	SDKVersion                 string       `json:"sdk_version"`
	BrowserVersion             string       `json:"browser_version"`
	DeviceCaptureCalls         int          `json:"device_capture_calls"`
	ICEPeers                   []WebICEPeer `json:"ice_peers"`
	SignalingFallback          bool         `json:"signaling_fallback"`
}
type WebICEPeer struct {
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	Type          string `json:"type"`
	SentBytes     int64  `json:"sent_bytes"`
	ReceivedBytes int64  `json:"received_bytes"`
}

type Result struct {
	Browser       *BrowserEvidence `json:"browser,omitempty"`
	Route         *RouteEvidence   `json:"route,omitempty"`
	ProcessExit   *ProcessExitInfo `json:"process_exit,omitempty"`
	Stages        []StageTiming    `json:"stages,omitempty"`
	ID            string           `json:"result_id"`
	WorkerID      string           `json:"worker_id"`
	Driver        string           `json:"driver"`
	Outcome       string           `json:"outcome"`
	Reason        string           `json:"reason"`
	Cleanup       string           `json:"cleanup"`
	Evidence      Evidence         `json:"evidence"`
	FinishedAt    time.Time        `json:"finished_at"`
	TraceComplete bool             `json:"trace_complete"`
	TraceDropped  uint64           `json:"trace_dropped"`
}
type ProcessExitInfo struct {
	At                 time.Time `json:"at"`
	PID                int       `json:"pid,omitempty"`
	Started            bool      `json:"started"`
	ExitCode           *int      `json:"exit_code,omitempty"`
	Signal             string    `json:"signal,omitempty"`
	DeadlineExceeded   bool      `json:"deadline_exceeded"`
	ForcedCancellation bool      `json:"forced_cancellation"`
}
type StageTiming struct {
	Name       string `json:"stage"`
	StartedMS  int64  `json:"started_elapsed_ms"`
	DurationMS int64  `json:"duration_ms"`
}
type DiagnosticEndpoint struct {
	Role         string `json:"endpoint_role"`
	UID          string `json:"uid"`
	PublisherUID string `json:"publisher_uid"`
	Channel      string `json:"channel"`
}

type LogRequest struct {
	Scope     string    `json:"scope,omitempty"`
	Partial   bool      `json:"partial,omitempty"`
	Error     string    `json:"error,omitempty"`
	ID        string    `json:"request_id"`
	RunID     string    `json:"run_id"`
	RuntimeID string    `json:"runtime_id"`
	Role      string    `json:"endpoint_role"`
	Status    string    `json:"status"`
	SHA256    string    `json:"sha256,omitempty"`
	Bytes     int64     `json:"bytes,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Message struct {
	HostID         string           `json:"host_id,omitempty"`
	ServiceType    string           `json:"service_type,omitempty"`
	ClientType     string           `json:"client_type,omitempty"`
	StorageBytes   int64            `json:"storage_bytes,omitempty"`
	ResourceError  string           `json:"resource_error,omitempty"`
	Capabilities   []string         `json:"capabilities,omitempty"`
	Retention      *RetentionPolicy `json:"retention,omitempty"`
	Version        int              `json:"protocol_version"`
	Type           string           `json:"type"`
	ID             string           `json:"message_id,omitempty"`
	RunID          string           `json:"run_id,omitempty"`
	Revision       string           `json:"config_revision,omitempty"`
	Role           string           `json:"endpoint_role,omitempty"`
	Driver         string           `json:"driver,omitempty"`
	RuntimeVersion string           `json:"runtime_version,omitempty"`
	SDKVersion     string           `json:"sdk_version,omitempty"`
	Platform       string           `json:"platform,omitempty"`
	Capacity       int              `json:"capacity,omitempty"`
	Job            *Job             `json:"job,omitempty"`
	Events         []Event          `json:"events,omitempty"`
	EventIDs       []string         `json:"event_ids,omitempty"`
	Result         *Result          `json:"result,omitempty"`
	Logs           *LogRequest      `json:"logs,omitempty"`
}

func Msg(kind string) Message { return Message{Version: Version, Type: kind, ID: ID()} }
func Decode(data []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}
