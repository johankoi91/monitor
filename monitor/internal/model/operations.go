package model

import "time"

type StartupRequest struct {
	Type          string `json:"type"`
	RequestID     string `json:"request_id"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
}
type StartupResult struct {
	Type          string   `json:"type"`
	RequestID     string   `json:"request_id"`
	ContainerID   string   `json:"container_id"`
	ContainerName string   `json:"container_name"`
	Startup       *Startup `json:"startup,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type RestartRule struct {
	AgentID       string `json:"agent_id" yaml:"agent_id"`
	ContainerName string `json:"container_name" yaml:"container_name"`
	ServiceCode   string `json:"service_code" yaml:"service_code"`
	Image         string `json:"image" yaml:"image"`
	Approved      bool   `json:"approved" yaml:"approved"`
	ApprovalRef   string `json:"approval_ref" yaml:"approval_ref"`
}
type RestartRequest struct {
	NodeID     string `json:"node_id"`
	Operator   string `json:"operator"`
	Reason     string `json:"reason"`
	RequestKey string `json:"request_key"`
}
type Task struct {
	Type          string    `json:"type"`
	Action        string    `json:"action"`
	OperationID   string    `json:"operation_id"`
	AgentID       string    `json:"agent_id"`
	NodeID        string    `json:"node_id"`
	ServiceCode   string    `json:"service_code"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image"`
	SnapshotID    string    `json:"snapshot_id"`
	Deadline      time.Time `json:"deadline"`
}
type TaskResult struct {
	Type              string     `json:"type"`
	OperationID       string     `json:"operation_id"`
	LedgerID          string     `json:"ledger_id"`
	Phase             string     `json:"phase"`
	ExecutionPossible bool       `json:"execution_possible"`
	StartedAt         *time.Time `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	ContainerID       string     `json:"container_id"`
	SnapshotID        string     `json:"snapshot_id"`
	BeforeStartedAt   string     `json:"before_started_at"`
	AfterStartedAt    string     `json:"after_started_at"`
	Message           string     `json:"message"`
}
type ResultAck struct {
	Type        string `json:"type"`
	OperationID string `json:"operation_id"`
}
type Operation struct {
	OperationID         string      `json:"operation_id"`
	RequestKey          string      `json:"request_key"`
	Type                string      `json:"type"`
	Status              string      `json:"status"`
	Phase               string      `json:"phase"`
	NodeID              string      `json:"node_id"`
	Reason              string      `json:"reason"`
	AuthenticatedSource string      `json:"authenticated_source"`
	SourceIP            string      `json:"source_ip"`
	RequestedAt         time.Time   `json:"requested_at"`
	StartedAt           *time.Time  `json:"started_at"`
	FinishedAt          *time.Time  `json:"finished_at"`
	BeforeStatus        string      `json:"before_status"`
	AfterStatus         *string     `json:"after_status"`
	Message             string      `json:"message"`
	NodeLocked          bool        `json:"node_locked"`
	Task                Task        `json:"task"`
	LedgerID            string      `json:"ledger_id,omitempty"`
	Evidence            *TaskResult `json:"evidence,omitempty"`
	VerifyDeadline      *time.Time  `json:"verify_deadline,omitempty"`
}

func (o Operation) ResultKnown() bool { return !o.NodeLocked && o.FinishedAt != nil }
func (o Operation) DurationMS() int64 {
	if o.FinishedAt == nil {
		return 0
	}
	return o.FinishedAt.Sub(o.RequestedAt).Milliseconds()
}

type AuditRecord struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Operation Operation `json:"operation"`
}

type OperationStep struct {
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	OperationID string    `json:"operation_id"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	Source      string    `json:"source,omitempty"`
}

func (r AuditRecord) Step() OperationStep {
	state := r.Operation.Phase
	if !r.Operation.NodeLocked {
		state = r.Operation.Status
	}
	return OperationStep{At: r.At, Kind: r.Kind, OperationID: r.Operation.OperationID, Status: state, Message: r.Operation.Message, Source: r.Operation.AuthenticatedSource}
}

type ResolveRequest struct {
	Operator string `json:"operator"`
	Reason   string `json:"reason"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence"`
}
type Notification struct {
	ID             string     `json:"event_id"`
	Type           string     `json:"event_type"`
	OccurredAt     time.Time  `json:"occurred_at"`
	Product        string     `json:"product"`
	Cluster        string     `json:"cluster"`
	Service        string     `json:"service_code"`
	Node           string     `json:"node_id,omitempty"`
	PreviousStatus string     `json:"previous_status"`
	CurrentStatus  string     `json:"current_status"`
	ReasonCode     string     `json:"reason_code"`
	Reason         string     `json:"reason"`
	CollectedAt    *time.Time `json:"collected_at"`
	Stale          bool       `json:"stale"`
}
