import {
  ApiOutlined,
  DeploymentUnitOutlined,
  ShareAltOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Empty from "antd/es/empty";
import Tag from "antd/es/tag";
import type { Run } from "./RTCModel";
import useRTCRunDetail from "./useRTCRunDetail";
import { rtcRunHref } from "../../app/routes";
import type { API } from "../../shared/api";
import RTCLink from "./RTCLink";
import {
  clientLabel,
  isInteropClient,
  reasonLabel,
  stateIcon,
} from "./RTCModel";
import { RTCSectionHeading } from "./RTCVisual";
export interface NativeEdge {
  transport: string;
  endpoint: string;
  agent_id?: string;
  container_name?: string;
  container_id?: string;
  stale?: boolean;
}
export interface NativeMember {
  group: string;
  edge: NativeEdge;
}
interface PhaseHealth {
  state: string;
  reason: string;
  last_run_id?: string;
  last_result_at?: string;
  failure_count: number;
  success_count: number;
  failure_threshold: number;
  recovery_threshold: number;
}
export interface NativeState {
  client_type?: string;
  worker_limit?: number;
  cross_topology?: string;
  scheduling_error?: string;
  coverage_version: string;
  expected: NativeMember[];
  issues: string[];
  phases: Record<
    string,
    {
      health: PhaseHealth;
      active_run_id?: string;
      case_count: number;
      next_at: string;
      round: number;
    }
  >;
}
export interface Route {
  status: string;
  reason?: string;
  expected?: string;
  observed: Array<{
    endpoint: string;
    sent_bytes: number;
    received_bytes: number;
  }>;
  ap_observed: boolean;
}
interface Media {
  media_duration_ms: number;
  received_video_frames?: number;
}
export interface NativeCase {
  duplex?: boolean;
  client_type?: string;
  sender_client?: string;
  receiver_client?: string;
  case_key: string;
  case_kind: string;
  ap_group: string;
  ap_entrypoint?: string;
  sender: NativeEdge;
  receiver: NativeEdge;
}
interface BrowserInfo {
  ice_peers: Array<{ ip: string; port: number; protocol: string }>;
  signaling_inventory_known?: boolean;
  signaling_fallback: boolean;
  signaling_fallback_attempted: boolean;
}
export interface CaseRow {
  sender_browser?: BrowserInfo;
  receiver_browser?: BrowserInfo;
  case: NativeCase;
  run_id?: string;
  status: string;
  reason?: string;
  sender_evidence?: Media;
  receiver_evidence?: Media;
  sender_route?: Route;
  receiver_route?: Route;
}
export interface NativeRun {
  run_id: string;
  status: string;
  reason: string;
  cases?: CaseRow[];
  native_plan?: { phase: string; coverage_version: string; issues: string[] };
}
export const phaseLabels: Record<string, string> = {
  AP: "AP 接入",
  DIRECT: "Edge 直连",
  CROSS: "跨 Edge 互通",
};
export function CoverageBadge({ value }: { value: string }) {
  return (
    <Tag
      className="rtc-state-tag"
      icon={stateIcon(value)}
      color={
        ["SUCCESS", "HEALTHY", "VERIFIED"].includes(value)
          ? "success"
          : ["FAILED", "UNHEALTHY", "MISMATCH"].includes(value)
            ? "error"
            : ["RUNNING", "PREPARING"].includes(value)
              ? "processing"
              : "default"
      }
    >
      {(
        {
          SUCCESS: "通过",
          HEALTHY: "正常",
          VERIFIED: "已确认",
          FAILED: "失败",
          UNHEALTHY: "异常",
          MISMATCH: "不符",
          DISABLED: "已停用",
          UNKNOWN: "待验证",
          UNVERIFIED: "待验证",
          ERROR: "执行错误",
          CANCELLED: "已取消",
          RUNNING: "执行中",
          PREPARING: "准备中",
          PENDING: "待执行",
        } as Record<string, string>
      )[value] || value}
    </Tag>
  );
}
export const casePath = (c: NativeCase) =>
  c.duplex
    ? `Native ${c.sender.transport.toUpperCase()} ↔ Web · AP ${c.ap_entrypoint || "—"}`
    : isInteropClient(c.client_type)
      ? `${c.sender_client === "native" ? (c.sender.transport === "aut" ? "Native AUT" : "Native UDP") : "Web"} → ${c.receiver_client === "native" ? (c.receiver.transport === "aut" ? "Native AUT" : "Native UDP") : "Web"} · AP ${c.ap_entrypoint || "—"}`
      : c.case_kind === "AP_JOIN"
        ? `${c.sender.transport.toUpperCase()} · AP ${c.ap_entrypoint || "—"}`
        : `${c.sender.endpoint || "—"} → ${c.receiver.endpoint || "—"}`;
export function NativeCases({ run }: { run: NativeRun }) {
  const rows = [...(run.cases || [])].sort(
    (a, b) =>
      Number(["FAILED", "ERROR"].includes(b.status)) -
      Number(["FAILED", "ERROR"].includes(a.status)),
  );
  return rows.length ? (
    <div className="rtc-table-wrap">
      <table className="rtc-table" aria-label="验证项">
        <thead>
          <tr>
            <th scope="col">验证路径</th>
            <th scope="col">本次结果</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.case.case_key}>
              <td data-label="验证路径">
                <span className="rtc-mono">{casePath(row.case)}</span>
                {row.case.duplex &&
                  (row.sender_evidence || row.receiver_evidence) && (
                    <div className="rtc-secondary">
                      Native 收：
                      {row.sender_evidence
                        ? `${(row.sender_evidence.media_duration_ms / 1000).toFixed(2)} 秒 / ${row.sender_evidence.received_video_frames || 0} 帧`
                        : "待验证"}
                      {" · "}Web 收：
                      {row.receiver_evidence
                        ? `${(row.receiver_evidence.media_duration_ms / 1000).toFixed(2)} 秒 / ${row.receiver_evidence.received_video_frames || 0} 帧`
                        : "待验证"}
                    </div>
                  )}
              </td>
              <td data-label="本次结果">
                <CoverageBadge value={row.status} />
                {["FAILED", "ERROR", "CANCELLED"].includes(row.status) && (
                  <div className="rtc-secondary">{reasonLabel(row.reason)}</div>
                )}
              </td>
              <td data-label="操作">
                <RTCLink
                  href={
                    row.run_id ? rtcRunHref(row.run_id, run.run_id) : undefined
                  }
                  disabled={!row.run_id}
                >
                  查看
                </RTCLink>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <Empty description="等待该类执行记录" />
  );
}
export default function NativeCoverageDetails({
  api,
  state,
  runs,
  phase,
  onPhase,
}: {
  api: API;
  state?: NativeState;
  runs: Run[];
  phase: string;
  onPhase: (phase: string) => void;
}) {
  const keys = Object.keys(phaseLabels).filter((key) => state?.phases[key]);
  const current = keys.includes(phase) ? phase : keys[0];
  const currentState = state?.phases[current];
  const currentID =
    currentState?.active_run_id || currentState?.health.last_run_id || null;
  const { run: record, error } = useRTCRunDetail(
    api,
    currentID,
    0,
    runs.find((run) => run.run_id === currentID),
  );
  if (!state) return <Empty description="等待首次执行计划" />;
  return (
    <>
      {error && <Alert type="error" showIcon message={error} />}
      {state.scheduling_error && (
        <Alert
          type="error"
          showIcon
          message={reasonLabel(state.scheduling_error)}
        />
      )}
      {!!state.issues?.length && (
        <Alert
          type="warning"
          showIcon
          message={state.issues.map(reasonLabel).join("；")}
        />
      )}
      <div className="rtc-phase-grid">
        {keys.map((key) => {
          const value = state.phases[key],
            phaseRecord =
              key === current
                ? record
                : runs.find(
                    (run) =>
                      run.run_id ===
                      (value.active_run_id || value.health.last_run_id),
                  ),
            unknown = value.health.state === "UNKNOWN";
          const PhaseIcon =
            key === "AP"
              ? ApiOutlined
              : key === "DIRECT"
                ? DeploymentUnitOutlined
                : ShareAltOutlined;
          return (
            <button
              key={key}
              type="button"
              className={`rtc-phase ${current === key ? "active" : ""}`}
              aria-pressed={current === key}
              onClick={() => onPhase(key)}
            >
              <div className="rtc-phase-heading">
                <span className="rtc-phase-label">
                  <span className="rtc-phase-icon">
                    <PhaseIcon aria-hidden />
                  </span>
                  <strong>
                    {isInteropClient(state.client_type)
                      ? `${clientLabel({ client_type: state.client_type })} 互通`
                      : phaseLabels[key]}
                  </strong>
                </span>
                <CoverageBadge
                  value={value.active_run_id ? "RUNNING" : value.health.state}
                />
              </div>
              <div className="rtc-phase-count">
                {phaseRecord
                  ? (phaseRecord.cases || []).filter(
                      (row) => row.status === "SUCCESS",
                    ).length
                  : !unknown &&
                      !value.active_run_id &&
                      value.health.state === "HEALTHY" &&
                      value.health.failure_count === 0
                    ? value.case_count
                    : "—"}
                <small> / {value.case_count} 项</small>
              </div>
            </button>
          );
        })}
      </div>
      <div className="rtc-panel">
        <div className="rtc-panel-heading">
          <RTCSectionHeading
            title="验证路径"
            description={
              isInteropClient(state.client_type)
                ? "每条独立频道 · Native / Web 双端收发 · 共两条路径"
                : phaseLabels[current]
            }
          />
        </div>
        {record ? (
          <NativeCases run={record} />
        ) : (
          <Empty description="等待该类执行记录" />
        )}
      </div>
    </>
  );
}
