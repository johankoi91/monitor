import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  MinusCircleOutlined,
  QuestionCircleOutlined,
} from "@ant-design/icons";
import Tag from "antd/es/tag";
import type { RTCStorageSettings } from "./RTCAdministration";
import type { NativeState } from "./RTCNativeCoverage";
import type { RTCParameters } from "./RTCSettings";
export interface Target {
  rtc?: RTCParameters;
  credentials?: { app_certificate?: string };
  has_certificate?: boolean;
  target_id: string;
  name: string;
  cluster_code?: string;
  driver: string;
}
export interface Task {
  name?: string;
  client_type: string;
  sweep_interval_seconds?: number;
  cross_interval_seconds?: number;
  task_id: string;
  target_id: string;
  host_id: string;
  service_type: string;
  enabled: boolean;
  failure_threshold: number;
  recovery_threshold: number;
  unknown_notify_seconds: number;
  description?: string;
  interval_seconds: number;
  timeout_seconds: number;
}
export interface ProbeRuntime {
  host_id: string;
  host_name: string;
  service_type: string;
  client_type: string;
  capabilities?: string[];
  resource_error?: string;
  storage_bytes?: number;
  name?: string;
  enabled?: boolean;
  runtime_id: string;
  driver?: string;
  location: string;
  host_address?: string;
  online: boolean;
  last_heartbeat?: string;
  platform?: string;
  sdk_version?: string;
  runtime_version?: string;
  capacity?: number;
}
export interface ProbeHost {
  host_id: string;
  host_name: string;
  host_address: string;
  location: string;
  online: boolean;
  services: { service_type: string; clients: ProbeRuntime[] }[];
}
export const isInteropClient = (client?: string) =>
  client === "interop" || client === "interop_aut";

export const taskRuntime = (task: Task, snapshot: Snapshot) => {
  const native = snapshot.runtimes.find(
    (runtime) =>
      runtime.host_id === task.host_id &&
      runtime.service_type === task.service_type &&
      runtime.client_type ===
        (isInteropClient(task.client_type) ? "native" : task.client_type),
  );
  if (!isInteropClient(task.client_type) || !native) return native;
  const web = snapshot.runtimes.find(
    (runtime) =>
      runtime.host_id === task.host_id &&
      runtime.service_type === task.service_type &&
      runtime.client_type === "web",
  );
  if (!web) return undefined;
  return {
    ...native,
    capacity: Math.min(native.capacity || 0, web.capacity || 0),
    online: native.online && web.online,
    enabled: native.enabled !== false && web.enabled !== false,
    sdk_version: "Native / Web",
    resource_error: native.resource_error || web.resource_error,
  };
};
export const rtcTargetSupports = (target: Target, client: string) =>
  target.driver === "agora" &&
  !!target.rtc &&
  (isInteropClient(client)
    ? !!target.rtc.native_coverage && !!target.rtc.web_access?.coverage
    : client === "web"
      ? !!target.rtc.web_access
      : client === "native" &&
        (!!target.rtc.native_coverage ||
          !!target.rtc.media_edges?.enabled ||
          !target.rtc.web_access));

export const rtcServerOptions = (
  hosts: ProbeHost[],
  client: string,
  currentHost?: string,
) =>
  hosts.flatMap((host) => {
    const clients =
      host.services.find((service) => service.service_type === "rtc")
        ?.clients || [];
    const required = isInteropClient(client) ? ["native", "web"] : [client];
    const matches = required.map((kind) =>
      clients.find((runtime) => runtime.client_type === kind),
    );
    if (matches.some((runtime) => !runtime)) return [];
    const needsUpgrade =
      isInteropClient(client) &&
      matches.some(
        (runtime) =>
          !runtime!.capabilities?.includes("native_web_duplex_v1") ||
          !runtime!.capabilities?.includes("native_aut_web_interop_v1") ||
          !runtime!.capabilities?.includes("native_web_interop_v1"),
      );
    const unavailable = matches.some(
      (runtime) => !runtime!.online || runtime!.enabled === false,
    );
    return [
      {
        value: host.host_id,
        label: `${host.host_name} · ${host.host_address} · ${clientLabel({ client_type: client })}${needsUpgrade ? "（客户端需升级）" : matches.some((runtime) => runtime!.enabled === false) ? "（已停用）" : unavailable ? "（离线）" : ""}`,
        disabled: needsUpgrade || (unavailable && host.host_id !== currentHost),
      },
    ];
  });

export interface Event {
  event_id: string;
  source_seq: number;
  occurred_at: string;
  monotonic_elapsed_ms: number;
  kind: string;
  stage: string;
  name?: string;
  return_code?: number;
  return_kind?: string;
  duration_ms?: number;
  fields?: Record<string, unknown>;
  call_id?: string;
  related_call_id?: string;
  connection_id?: string;
  service_id?: string;
  duration_us?: number;
}
export interface Evidence {
  first_frame_elapsed_ms?: number;
  first_video_elapsed_ms?: number;
  connected: boolean;
  published: boolean;
  subscribed: boolean;
  sent_frames: number;
  sent_bytes: number;
  received_frames: number;
  valid_samples: number;
  sent_video_frames?: number;
  received_video_frames?: number;
  video_width?: number;
  video_height?: number;
  media_duration_ms: number;
}
export interface Endpoint {
  job: {
    runtime_location?: string;
    runtime_version?: string;
    sdk_version?: string;
    platform?: string;
    native_case?: import("./RTCNativeCoverage").NativeCase;
    channel?: string;
    uid?: string;
    runtime_id: string;
    endpoint_role: string;
  };
  ready: boolean;
  events?: Event[] | null;
  result?: {
    stages?: {
      stage: string;
      started_elapsed_ms: number;
      duration_ms: number;
    }[];
    trace_complete?: boolean;
    trace_dropped?: number;
    process_exit?: {
      exit_code?: number;
      signal?: string;
      deadline_exceeded: boolean;
    };
    outcome: string;
    reason: string;
    cleanup: string;
    evidence: Evidence;
    route?: import("./RTCNativeCoverage").Route;
    browser?: {
      sdk_version: string;
      browser_version: string;
      device_capture_calls: number;
      signaling_fallback: boolean;
      signaling_fallback_attempted?: boolean;
      ice_peers: Array<{
        ip: string;
        port: number;
        protocol: string;
        type: string;
      }>;
    };
  };
}
export interface LogFile {
  partial?: boolean;
  error?: string;
  request_id: string;
  endpoint_role: string;
  status: string;
  bytes?: number;
  sha256?: string;
}
export interface EdgeResult {
  endpoint: string;
  run_id?: string;
  status: string;
  reason?: string;
  receiver_evidence?: Evidence;
  cleanup?: string;
  started_at?: string;
  finished_at?: string;
}
export interface Run {
  client_type?: string;
  native_case?: import("./RTCNativeCoverage").NativeCase;
  native_plan?: { phase: string; coverage_version: string; issues: string[] };
  cases?: import("./RTCNativeCoverage").NativeRun["cases"];
  kind?: string;
  parent_run_id?: string;
  edge_endpoint?: string;
  edges?: EdgeResult[];
  run_id: string;
  task_id: string;
  target_id: string;
  config_revision: string;
  driver: string;
  status: string;
  reason: string;
  created_at: string;
  finished_at?: string;
  endpoints: Record<string, Endpoint>;
  logs: Record<string, LogFile>;
}
export interface Snapshot {
  task_activity?: Record<
    string,
    { running: boolean; active_run_id?: string; latest_run_id?: string }
  >;
  native_coverage?: Record<string, NativeState>;
  mode: string;
  config_revision: string;
  rtc_service_status: string;
  storage_available: boolean;
  runtimes: ProbeRuntime[];
  hosts: ProbeHost[];
  targets: Target[];
  tasks: Task[];
  runs: Run[];
  settings: RTCStorageSettings;
  task_health: Record<
    string,
    {
      state: string;
      reason: string;
      infrastructure_reason?: string;
      failure_count: number;
      success_count: number;
      failure_threshold: number;
      recovery_threshold: number;
      last_success_at?: string;
      last_run_id?: string;
      last_result_at?: string;
    }
  >;
}
export type Edit =
  { kind: "target"; value?: Target } | { kind: "task"; value?: Task };
export const terminal = (s: string) =>
  ["SUCCESS", "FAILED", "ERROR", "CANCELLED"].includes(s);
export const isParentRun = (run: Run) =>
  run.kind === "NATIVE_COVERAGE" || run.kind === "EDGE_SWEEP";
export const time = (s?: string) =>
  s ? new Date(s).toLocaleString("zh-CN", { hour12: false }) : "—";
export const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "操作未完成";
export const statuses: Record<string, string> = {
  PREPARING: "准备中",
  RUNNING: "执行中",
  SUCCESS: "通过",
  FAILED: "失败",
  ERROR: "探测执行错误",
  CANCELLED: "已取消",
  PENDING: "待探测",
  REQUESTED: "等待回捞",
  AVAILABLE: "可下载",
  EXPIRED: "已过期",
  UNKNOWN: "待验证",
  VERIFIED: "已确认",
  UNVERIFIED: "待验证",
  MISMATCH: "不符",
  HEALTHY: "正常",
  UNHEALTHY: "异常",
};
export const reasons: Record<string, string> = {
  NO_SHARED_AP: "Native 与 Web 没有共享的可用 AP",
  INTEROP_EDGE_CAPABILITY_UNAVAILABLE: "缺少可分配的 Native 或 Web Edge",
  PROBE_INTEROP_CAPABILITY_UNAVAILABLE: "客户端尚不支持 Native/Web 互通",
  AP_NOT_DISCOVERED: "未发现所选 AP 节点",
  AP_DISCOVERY_STALE: "AP 发现数据已过期",
  AP_DISCOVERY_AMBIGUOUS: "AP 分组地址存在歧义",
  PROBE_EDGE_FORMAT_UNAVAILABLE: "探针需要升级以支持简化 Edge 地址",
  NATIVE_SCHEDULE_BUDGET_EXCEEDED: "任务周期不足以覆盖当前实例，请调整执行间隔",
  NATIVE_COVERAGE_PENDING: "等待完整覆盖验证",
  NATIVE_COVERAGE_VERIFIED: "配置范围验证通过",
  NATIVE_AP_FAILED: "AP 接入异常",
  NATIVE_DIRECT_FAILED: "固定 Edge 接入异常",
  NATIVE_CROSS_FAILED: "跨 Edge 互通异常",
  INVENTORY_STALE: "发现数据过期或实例失联",
  INVENTORY_INCOMPLETE: "部分实例参数无法解析",
  COVERAGE_CHANGED: "覆盖清单变化，等待新结果",
  MEDIA_EDGE_SWEEP_RUNNING: "正在逐个探测 UDP edge",
  ALL_MEDIA_EDGES_VERIFIED: "全部 UDP edge 音视频接收通过",
  MEDIA_EDGE_FAILED: "存在探测失败的 UDP edge",
  MEDIA_EDGE_INCOMPLETE: "部分 UDP edge 未完成有效探测",
  SWEEP_DEADLINE_EXCEEDED: "端口范围探测超过整轮期限",
  SDK_CONFIGURE_MEDIA_EDGE_FAILED: "SDK 未能设置指定 UDP edge",

  PROBE_CAPABILITY_MISMATCH: "探针不支持本任务所需的音视频能力",
  LOCAL_MEDIA_UNAVAILABLE: "探针 demo 素材不可读，请检查离线镜像与文件",
  WORKER_START_FAILED: "探测工作进程未能启动",
  WORKER_DEADLINE_EXCEEDED: "工作进程超过硬超时并已终止",
  SDK_PROCESS_CRASHED: "SDK 工作进程异常终止，请查看退出信息与原生日志",
  WORKER_EXITED_WITHOUT_RESULT: "工作进程退出前未产生结果，请查看进程日志",
  LOCAL_RESOURCE_EXHAUSTED: "探针本地空间不足，请调整保留时间或磁盘预算",
  PROBE_STORAGE_EXHAUSTED: "探针诊断空间不足，已暂停新任务",
  CONTROL_START_TIMEOUT: "等待中心开始指令超时，请检查探针连接和中心负载",
  PROBE_OFFLINE: "探针离线，请检查探测服务与中心连接",
  PROBE_DISABLED: "探针已停用",
  RESULT_EXPIRED: "结果已过期，等待新一轮有效探测",
  STORAGE_UNAVAILABLE: "中心存储不可用，请检查数据库与容量",
  WAITING_FIRST_RESULT: "等待首次有效探测",
  AUDIO_RECEIVE_TIMEOUT: "音频接收超时",
  VIDEO_RECEIVE_TIMEOUT: "视频接收超时",
  SIMULATION_ONLY: "历史执行结果",
  MEDIA_VERIFIED: "音视频接收证据通过",
  MEDIA_RECEIVE_TIMEOUT: "音视频接收超时",
  SDK_JOIN_FAILED: "加入频道失败",
  SDK_INITIALIZE_FAILED: "SDK 初始化失败",
  MOCK_EXECUTION: "模拟执行",
  MEDIA_TIMEOUT: "媒体接收超时",
  MOCK_INITIALIZATION_ERROR: "模拟 SDK 初始化错误",
  MOCK_AUTH_REJECTED: "模拟鉴权拒绝",
  MOCK_PUBLISH_FAILED: "模拟发布失败",
  MANUAL_CANCEL: "手动取消",
  TASK_DISABLED: "任务已停用",
  CONFIG_CHANGED: "配置变更，本轮已取消",
  CENTER_RESTARTED: "中心重启，本轮已中断",
  EXECUTION_DEADLINE_EXCEEDED: "工作进程超过执行与清理期限",
};
export function stateIcon(value: string) {
  if (["SUCCESS", "AVAILABLE", "HEALTHY", "VERIFIED"].includes(value))
    return <CheckCircleOutlined aria-hidden />;
  if (["FAILED", "ERROR", "UNHEALTHY", "MISMATCH"].includes(value))
    return <CloseCircleOutlined aria-hidden />;
  if (["RUNNING", "PREPARING", "REQUESTED"].includes(value))
    return <ClockCircleOutlined aria-hidden />;
  if (["DISABLED", "CANCELLED"].includes(value))
    return <MinusCircleOutlined aria-hidden />;
  return <QuestionCircleOutlined aria-hidden />;
}
export function State({ value }: { value: string }) {
  return (
    <Tag
      className="rtc-state-tag"
      icon={stateIcon(value)}
      color={
        value === "SUCCESS" || value === "AVAILABLE"
          ? "success"
          : value === "FAILED" || value === "ERROR"
            ? "error"
            : value === "RUNNING" ||
                value === "PREPARING" ||
                value === "REQUESTED"
              ? "processing"
              : "default"
      }
    >
      {statuses[value] || value}
    </Tag>
  );
}

export const clientLabel = (task?: { client_type?: string }) =>
  isInteropClient(task?.client_type)
    ? "Native ↔ Web"
    : task?.client_type === "web"
      ? "Web"
      : task?.client_type === "native"
        ? "Native"
        : task?.client_type || "未上报";
export const taskLabel = (task: Task) =>
  task.name || `${clientLabel(task)} 音视频探测`;
export const reasonLabel = (reason?: string) =>
  reason ? reasons[reason] || reason : "";
