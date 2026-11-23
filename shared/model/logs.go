package model

import "time"

const LogChunkBytes = 128 << 10
const MaxLogArchiveBytes int64 = 256 << 20
const MaxLogSourceBytes int64 = 1 << 30

type LogRequest struct {
	ServiceContainerIDs []string `json:"service_container_ids,omitempty"`
	NodeID              string   `json:"node_id,omitempty"`
	AgentID             string   `json:"agent_id"`
	Mode                string   `json:"mode"`
	Time                string   `json:"time"`
}

// LogTask binds collection to the AP configuration reported by this Agent.
// Neither a browser nor a command can supply an arbitrary filesystem path.
type LogTask struct {
	Services    []LogService `json:"services,omitempty"`
	Type        string       `json:"type"`
	ID          string       `json:"id"`
	ContainerID string       `json:"container_id"`
	SnapshotID  string       `json:"snapshot_id"`
	Source      string       `json:"source"`
	Mode        string       `json:"mode"`
	Time        string       `json:"time"`
	Deadline    time.Time    `json:"deadline"`
}

// Docker's process list uses host PIDs. Report this with each discovery
// snapshot, independently of the less frequent startup configuration payload.
type ContainerProcess struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

type LogService struct {
	ContainerID   string             `json:"container_id"`
	ContainerName string             `json:"container_name"`
	SnapshotID    string             `json:"snapshot_id"`
	Processes     []ContainerProcess `json:"processes"`
	Available     bool               `json:"available"`
	Reason        string             `json:"reason,omitempty"`
}

func ValidProcesses(processes []ContainerProcess) bool {
	if len(processes) == 0 || len(processes) > 128 {
		return false
	}
	seen := map[int]bool{}
	for _, process := range processes {
		if process.PID <= 0 || process.PID > 2147483647 || process.Name == "" || len(process.Name) > 255 || seen[process.PID] {
			return false
		}
		seen[process.PID] = true
	}
	return true
}

type LogChunk struct {
	TransferID string `json:"transfer_id,omitempty"`
	Type       string `json:"type"`
	ID         string `json:"id"`
	Sequence   int    `json:"sequence"`
	Data       []byte `json:"data,omitempty"`
	Done       bool   `json:"done,omitempty"`
	Error      string `json:"error,omitempty"`
}

type LogResult struct {
	Label     string `json:"label"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	Error     string `json:"error,omitempty"`
	Bytes     int64  `json:"bytes"`
	FileCount int    `json:"file_count"`
}

type LogDownloadTask struct {
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	TransferID string    `json:"transfer_id"`
	Deadline   time.Time `json:"deadline"`
}
