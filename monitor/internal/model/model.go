package model

import "time"

type Check struct {
	Type      string `json:"type" yaml:"type"`
	Host      string `json:"host" yaml:"host"`
	Port      int    `json:"port" yaml:"port"`
	TimeoutMS int    `json:"timeout_ms" yaml:"timeout_ms"`
}
type Node struct {
	ID             string  `json:"id" yaml:"id"`
	AgentID        string  `json:"agent_id" yaml:"agent_id"`
	HostName       string  `json:"host_name" yaml:"host_name"`
	HostAddress    string  `json:"host_address" yaml:"host_address"`
	ContainerName  string  `json:"container_name" yaml:"container_name"`
	RestartEnabled bool    `json:"restart_enabled" yaml:"restart_enabled"`
	Checks         []Check `json:"checks" yaml:"checks"`
}
type Service struct {
	Code  string `json:"code" yaml:"code"`
	Name  string `json:"name" yaml:"name"`
	Nodes []Node `json:"nodes" yaml:"nodes"`
}
type Cluster struct {
	Code     string    `json:"code" yaml:"code"`
	Name     string    `json:"name" yaml:"name"`
	Product  string    `json:"product" yaml:"product"`
	Services []Service `json:"services" yaml:"services"`
}
type Site struct {
	Code string `json:"code" yaml:"code"`
	Name string `json:"name" yaml:"name"`
}
type Definition struct {
	SchemaVersion int       `json:"schema_version" yaml:"schema_version"`
	Site          Site      `json:"site" yaml:"site"`
	Clusters      []Cluster `json:"clusters" yaml:"clusters"`
}
type Baseline struct {
	Revision   string      `json:"revision"`
	Active     bool        `json:"active"`
	SavedAt    *time.Time  `json:"saved_at"`
	Definition *Definition `json:"definition"`
	YAMLURL    *string     `json:"yaml_url"`
}
type Addition struct {
	AgentID     string  `json:"agent_id"`
	ContainerID string  `json:"container_id"`
	SnapshotID  string  `json:"snapshot_id"`
	ClusterCode string  `json:"cluster_code"`
	ClusterName string  `json:"cluster_name"`
	ServiceCode string  `json:"service_code"`
	ServiceName string  `json:"service_name"`
	Checks      []Check `json:"checks"`
}
type Selection struct {
	ExpectedRevision string     `json:"expected_revision"`
	Additions        []Addition `json:"additions"`
	Removals         []string   `json:"removals"`
}
type Env struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Redacted bool   `json:"redacted"`
}
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Type     string `json:"type"`
	ReadOnly bool   `json:"read_only"`
}
type PortBinding struct {
	ContainerPort string `json:"container_port"`
	HostIP        string `json:"host_ip"`
	HostPort      string `json:"host_port"`
}
type RestartPolicy struct {
	Name              string `json:"name"`
	MaximumRetryCount int    `json:"maximum_retry_count"`
}
type StartupConfig struct {
	Image         string        `json:"image"`
	ImageID       string        `json:"image_id"`
	Entrypoint    []string      `json:"entrypoint"`
	Cmd           []string      `json:"cmd"`
	Path          string        `json:"path"`
	Args          []string      `json:"args"`
	Env           []Env         `json:"env"`
	WorkingDir    string        `json:"working_dir"`
	User          string        `json:"user"`
	Mounts        []Mount       `json:"mounts"`
	ExposedPorts  []string      `json:"exposed_ports"`
	PortBindings  []PortBinding `json:"port_bindings"`
	NetworkMode   string        `json:"network_mode"`
	RestartPolicy RestartPolicy `json:"restart_policy"`
}
type Startup struct {
	SnapshotID     string        `json:"snapshot_id"`
	Fingerprint    string        `json:"configuration_fingerprint"`
	CollectedAt    time.Time     `json:"collected_at"`
	RedactedFields []string      `json:"redacted_fields"`
	Truncated      bool          `json:"truncated"`
	Incomplete     bool          `json:"incomplete"`
	Config         StartupConfig `json:"config"`
}
type Container struct {
	AgentID       string              `json:"agent_id"`
	HostName      string              `json:"host_name"`
	HostAddress   string              `json:"host_address"`
	ContainerID   string              `json:"container_id"`
	ContainerName string              `json:"container_name"`
	RuntimeStatus string              `json:"runtime_status"`
	Image         string              `json:"image"`
	CollectedAt   time.Time           `json:"collected_at"`
	ReceivedAt    time.Time           `json:"received_at"`
	AgentOnline   bool                `json:"agent_online"`
	Stale         bool                `json:"stale"`
	Managed       bool                `json:"managed"`
	NodeID        string              `json:"node_id,omitempty"`
	SnapshotID    string              `json:"snapshot_id"`
	Resources     *ResourceMetrics    `json:"resources,omitempty"`
	Limits        *ResourceLimits     `json:"limits,omitempty"`
	Lifecycle     *ContainerLifecycle `json:"lifecycle,omitempty"`
}

type ResourceLimits struct {
	CPUQuotaCores    *float64 `json:"cpu_quota_cores"`
	MemoryLimitBytes *uint64  `json:"memory_limit_bytes"`
}
type ContainerLifecycle struct {
	CreatedAt      *time.Time `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	LastObservedAt time.Time  `json:"last_observed_at"`
	RestartCount   int        `json:"restart_count"`
	ExitCode       *int       `json:"exit_code"`
	OOMKilled      *bool      `json:"oom_killed"`
	DockerHealth   string     `json:"docker_health"`
	Stale          bool       `json:"stale"`
}
type HostFilesystem struct {
	Scope           string    `json:"scope"`
	Source          string    `json:"source"`
	CollectedAt     time.Time `json:"collected_at"`
	CapacityBytes   uint64    `json:"capacity_bytes"`
	FreeBytes       uint64    `json:"free_bytes"`
	AvailableBytes  uint64    `json:"available_bytes"`
	UsedRatio       float64   `json:"used_ratio"`
	InodesTotal     *uint64   `json:"inodes_total"`
	InodesFree      *uint64   `json:"inodes_free"`
	InodesUsedRatio *float64  `json:"inodes_used_ratio"`
}
type DiskDeviceMetrics struct {
	Device            string   `json:"device"`
	Major             uint64   `json:"major"`
	Minor             uint64   `json:"minor"`
	ReadBPS           *float64 `json:"read_bytes_per_second"`
	WriteBPS          *float64 `json:"write_bytes_per_second"`
	ReadOpsPerSecond  *float64 `json:"read_operations_per_second"`
	WriteOpsPerSecond *float64 `json:"write_operations_per_second"`
}

// ResourceMetrics is a bounded numeric view of cAdvisor, not a health verdict.
// Nil readings mean unavailable; zero is a valid measured value.
type ResourceMetrics struct {
	LimitsKnown                     bool                `json:"limits_known"`
	Source                          string              `json:"source"`
	Status                          string              `json:"status"`
	ReasonCode                      string              `json:"reason_code"`
	SampledAt                       *time.Time          `json:"sampled_at"`
	FetchedAt                       time.Time           `json:"fetched_at"`
	Stale                           bool                `json:"stale"`
	SampleWindowSeconds             *float64            `json:"sample_window_seconds"`
	CPUUsageCores                   *float64            `json:"cpu_usage_cores"`
	MemoryUsageBytes                *uint64             `json:"memory_usage_bytes"`
	MemoryWorkingSetBytes           *uint64             `json:"memory_working_set_bytes"`
	FilesystemUsageBytes            *uint64             `json:"filesystem_usage_bytes"`
	NetworkReceiveBPS               *float64            `json:"network_receive_bytes_per_second"`
	NetworkTransmitBPS              *float64            `json:"network_transmit_bytes_per_second"`
	NetworkScope                    string              `json:"network_scope"`
	CPUUserUsageCores               *float64            `json:"cpu_user_usage_cores"`
	CPUSystemUsageCores             *float64            `json:"cpu_system_usage_cores"`
	CPUQuotaCores                   *float64            `json:"cpu_quota_cores"`
	CPUThrottledRatio               *float64            `json:"cpu_throttled_ratio"`
	CPUThrottledSecondsPerSecond    *float64            `json:"cpu_throttled_seconds_per_second"`
	MemoryLimitBytes                *uint64             `json:"memory_limit_bytes"`
	MemoryWorkingSetRatio           *float64            `json:"memory_working_set_ratio"`
	MemoryRSSBytes                  *uint64             `json:"memory_rss_bytes"`
	MemoryCacheBytes                *uint64             `json:"memory_cache_bytes"`
	MemorySwapBytes                 *uint64             `json:"memory_swap_bytes"`
	MemoryPeakBytes                 *uint64             `json:"memory_peak_bytes"`
	MemoryFailuresTotal             *uint64             `json:"memory_failures_total"`
	OOMEventsTotal                  *uint64             `json:"oom_events_total"`
	OOMObservedAt                   *time.Time          `json:"oom_observed_at"`
	HostFilesystem                  *HostFilesystem     `json:"host_filesystem"`
	DiskIOStatus                    string              `json:"disk_io_status"`
	DiskIOScope                     string              `json:"disk_io_scope"`
	DiskDevices                     []DiskDeviceMetrics `json:"disk_devices"`
	DiskReadBPS                     *float64            `json:"disk_read_bytes_per_second"`
	DiskWriteBPS                    *float64            `json:"disk_write_bytes_per_second"`
	DiskReadOpsPerSecond            *float64            `json:"disk_read_operations_per_second"`
	DiskWriteOpsPerSecond           *float64            `json:"disk_write_operations_per_second"`
	NetworkReceivePacketsPerSecond  *float64            `json:"network_receive_packets_per_second"`
	NetworkTransmitPacketsPerSecond *float64            `json:"network_transmit_packets_per_second"`
	NetworkReceiveDroppedPerSecond  *float64            `json:"network_receive_dropped_per_second"`
	NetworkTransmitDroppedPerSecond *float64            `json:"network_transmit_dropped_per_second"`
	NetworkReceiveErrorsPerSecond   *float64            `json:"network_receive_errors_per_second"`
	NetworkTransmitErrorsPerSecond  *float64            `json:"network_transmit_errors_per_second"`
}
type CheckResult struct {
	Check       Check     `json:"check"`
	OK          bool      `json:"ok"`
	CollectedAt time.Time `json:"collected_at"`
}
type Observation struct {
	Container    Container     `json:"container"`
	Startup      *Startup      `json:"startup"`
	StartedAt    string        `json:"started_at"`
	ExitCode     int           `json:"exit_code"`
	RestartCount int           `json:"restart_count"`
	DockerHealth string        `json:"docker_health"`
	Checks       []CheckResult `json:"checks"`
}
type Report struct {
	Type             string        `json:"type"`
	Sequence         uint64        `json:"sequence"`
	CollectedAt      time.Time     `json:"collected_at"`
	Complete         bool          `json:"complete"`
	BaselineRevision string        `json:"baseline_revision"`
	Error            string        `json:"error,omitempty"`
	Containers       []Observation `json:"containers"`
}
type PlanNode struct {
	ContainerName string  `json:"container_name"`
	Checks        []Check `json:"checks"`
}
type Plan struct {
	Type     string     `json:"type"`
	Revision string     `json:"revision"`
	Nodes    []PlanNode `json:"nodes"`
}
