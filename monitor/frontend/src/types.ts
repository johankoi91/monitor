export interface Agent {
  agent_id: string;
  host_name: string;
  host_address: string;
  agent_online: boolean;
  stale: boolean;
}
export interface Container {
  agent_id: string;
  container_id: string;
  container_name: string;
  host_name: string;
  host_address: string;
  image: string;
  runtime_status: string;
  collected_at: string;
  agent_online: boolean;
  stale: boolean;
  managed: boolean;
  node_id?: string;
  snapshot_id: string;
  resources?: ResourceMetrics | null;
  lifecycle?: ContainerLifecycle | null;
  limits?: {
    cpu_quota_cores: number | null;
    memory_limit_bytes: number | null;
  } | null;
}
export interface ContainerLifecycle {
  created_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  last_observed_at: string;
  restart_count: number;
  exit_code: number | null;
  oom_killed: boolean | null;
  docker_health: string;
  stale: boolean;
}
export interface HostFilesystem {
  scope: string;
  source: string;
  collected_at: string;
  capacity_bytes: number;
  free_bytes: number;
  available_bytes: number;
  used_ratio: number;
  inodes_total: number | null;
  inodes_free: number | null;
  inodes_used_ratio: number | null;
}
export interface ResourceMetrics {
  limits_known?: boolean;
  source: string;
  status: string;
  reason_code: string;
  sampled_at: string | null;
  fetched_at: string;
  stale: boolean;
  sample_window_seconds: number | null;
  cpu_usage_cores: number | null;
  memory_usage_bytes: number | null;
  memory_working_set_bytes: number | null;
  filesystem_usage_bytes: number | null;
  network_receive_bytes_per_second: number | null;
  network_transmit_bytes_per_second: number | null;
  network_scope: string;
  cpu_user_usage_cores?: number | null;
  cpu_system_usage_cores?: number | null;
  cpu_quota_cores?: number | null;
  cpu_throttled_ratio?: number | null;
  cpu_throttled_seconds_per_second?: number | null;
  memory_limit_bytes?: number | null;
  memory_working_set_ratio?: number | null;
  memory_rss_bytes?: number | null;
  memory_cache_bytes?: number | null;
  memory_swap_bytes?: number | null;
  memory_peak_bytes?: number | null;
  memory_failures_total?: number | null;
  oom_events_total?: number | null;
  oom_observed_at?: string | null;
  host_filesystem?: HostFilesystem | null;
  disk_io_status?: string;
  disk_io_scope?: string;
  disk_devices?: Array<{
    device: string;
    major: number;
    minor: number;
    read_bytes_per_second: number | null;
    write_bytes_per_second: number | null;
    read_operations_per_second: number | null;
    write_operations_per_second: number | null;
  }> | null;
  disk_read_bytes_per_second?: number | null;
  disk_write_bytes_per_second?: number | null;
  disk_read_operations_per_second?: number | null;
  disk_write_operations_per_second?: number | null;
  network_receive_packets_per_second?: number | null;
  network_transmit_packets_per_second?: number | null;
  network_receive_dropped_per_second?: number | null;
  network_transmit_dropped_per_second?: number | null;
  network_receive_errors_per_second?: number | null;
  network_transmit_errors_per_second?: number | null;
}
export interface Check {
  type: string;
  host: string;
  port: number;
  timeout_ms: number;
}
export interface Node {
  id: string;
  agent_id: string;
  host_name: string;
  host_address: string;
  container_name: string;
  restart_enabled: boolean;
  checks: Check[];
}
export interface Service {
  code: string;
  name: string;
  nodes: Node[];
}
export interface Cluster {
  code: string;
  name: string;
  product: string;
  services: Service[];
}
export interface Baseline {
  revision: string;
  active: boolean;
  saved_at: string | null;
  yaml_url: string | null;
  definition: {
    schema_version: number;
    site: { code: string; name: string };
    clusters: Cluster[];
  } | null;
}
export interface Addition {
  agent_id: string;
  container_id: string;
  snapshot_id: string;
  cluster_code: string;
  checks: Check[];
  container_name: string;
  host_address: string;
}
export interface NotificationSettings {
  auth_scheme?: string;
  response_seconds?: number;
  max_retries?: number;
  revision: string;
  enabled: boolean;
  url: string;
  tls_server_name: string;
  auth_configured: boolean;
  storage_available: boolean;
}
export interface NodeStatus {
  node_id: string;
  host_name: string;
  host_address: string;
  container_name: string;
  image: string;
  status: string;
  runtime_status: string;
  check_level: string;
  reason: string;
  reason_code: string;
  collected_at: string | null;
  stale: boolean;
  restart_enabled: boolean;
  restart_allowed: boolean;
  restart_block_reason: string;
  active_operation_id: string;
  resources?: ResourceMetrics | null;
  lifecycle?: ContainerLifecycle | null;
}
export interface ServiceStatus {
  cluster_code: string;
  service_code: string;
  service_name: string;
  status: string;
  expected_node_count: number;
  discovered_node_count: number;
  healthy_node_count: number;
  unhealthy_node_count: number;
  unknown_node_count: number;
  nodes: NodeStatus[];
}
export interface Operation {
  operation_id: string;
  node_id: string;
  request_key: string;
  type: string;
  status: string;
  phase: string;
  operator: string;
  reason: string;
  authenticated_source: string;
  source_ip: string;
  requested_at: string;
  started_at: string | null;
  finished_at: string | null;
  before_status: string;
  after_status: string | null;
  message: string;
  result_known: boolean;
  node_locked: boolean;
}
export interface Delivery {
  event: {
    event_id: string;
    event_type?: string;
    occurred_at: string;
    node_id?: string;
    service_code: string;
    previous_status: string;
    current_status: string;
  };
  status: string;
  attempts: number;
  last_error: string;
}
export interface Notifications {
  enabled: boolean;
  pending?: number;
  delivered?: number;
  failed?: number;
  dropped?: number;
  storage_available?: boolean;
  recent?: Delivery[];
}
