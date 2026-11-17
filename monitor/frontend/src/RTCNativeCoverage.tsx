import { useEffect, useRef, useState } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Drawer from "antd/es/drawer";
import Empty from "antd/es/empty";
import Space from "antd/es/space";
import Table from "antd/es/table";
import Tabs from "antd/es/tabs";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import type { API } from "./api";

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
 worker_limit?:number;
 cross_topology?:string;
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
interface Route {
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
interface NativeCase {
  case_key: string;
  case_kind: string;
  ap_group: string;
  ap_entrypoint?: string;
  sender: NativeEdge;
  receiver: NativeEdge;
}
interface CaseRow {
  case: NativeCase;
  run_id?: string;
  status: string;
  reason?: string;
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
const phases = {
  AP: "AP 接入",
  DIRECT: "Edge 固定地址",
  CROSS: "跨 Edge 互通",
};
const outcomes: Record<string, string> = {
  SUCCESS: "通过",
  FAILED: "失败",
  ERROR: "执行错误",
  CANCELLED: "已取消",
  PENDING: "待执行",
  PREPARING: "准备中",
  RUNNING: "执行中",
  HEALTHY: "正常",
  UNHEALTHY: "异常",
  UNKNOWN: "未知",
  VERIFIED: "已确认",
  UNVERIFIED: "未确认",
  MISMATCH: "不符",
};
const reasons: Record<string, string> = {
  INVENTORY_STALE: "发现数据已过期或实例失联",
  INVENTORY_INCOMPLETE: "部分 Native 实例参数无法解析",
  NATIVE_INVENTORY_UNAVAILABLE: "启动参数发现数据不可用",
  NO_udp_EDGE_DISCOVERED: "尚未发现 UDP 实例",
  NO_aut_EDGE_DISCOVERED: "尚未发现 AUT 实例",
  CROSS_EDGE_INVENTORY_INCOMPLETE: "互通所需实例不完整",
  PROBE_NATIVE_CAPABILITY_UNAVAILABLE: "探针尚不支持完整选路验证",
  JOIN_TIMEOUT: "加入频道超时",
  MEDIA_VERIFIED: "音视频通过",
  NATIVE_PHASE_VERIFIED: "本类覆盖通过",
  ROUTE_UNVERIFIED: "实际选路尚未确认",
  ROUTE_MISMATCH: "实际选路与计划不符",
  COVERAGE_CHANGED: "清单变化，等待新覆盖",
  NATIVE_PLAN_TIMEOUT: "本轮超过执行预算",
  WAITING_FIRST_RESULT: "等待首次结果",
  CENTER_RESTARTED: "中心重启，等待新结果",
};
function Badge({ value }: { value: string }) {
  return (
    <Tag
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
      {outcomes[value] || value}
    </Tag>
  );
}
const routeName = (edge: NativeEdge) =>
  `${edge.transport.toUpperCase()} · ${edge.endpoint || "AP 分配"}`;
export function NativeCases({
  run,
  onOpen,
}: {
  run: NativeRun;
  onOpen: (id: string) => void;
}) {
  return (
    <>
      <Space wrap>
        <Badge value={run.status} />
        <Typography.Text>{reasons[run.reason] || run.reason}</Typography.Text>
      </Space>
      <Table<CaseRow>
        size="small"
        rowKey={(row) => row.case.case_key}
        style={{ marginTop: 12 }}
        dataSource={run.cases || []}
        pagination={false}
        scroll={{ x: 920 }}
        expandable={{
          expandedRowRender: (row) => (
            <pre className="rtc-event-fields">
              {JSON.stringify(
                {
                  sender_route: row.sender_route,
                  receiver_route: row.receiver_route,
                },
                null,
                2,
              )}
            </pre>
          ),
        }}
        columns={[
          {
            title: "AP 分组 / 入口",
            key: "ap",
            render: (_, row) => (
              <>
                {row.case.ap_group}
                <div>{row.case.ap_entrypoint}</div>
              </>
            ),
          },
          {
            title: "发送 → 接收",
            key: "route",
            render: (_, row) => (
              <>
                {routeName(row.case.sender)}
                <div>↓ {routeName(row.case.receiver)}</div>
              </>
            ),
          },
          {
            title: "结果",
            key: "status",
            render: (_, row) => (
              <>
                <Badge value={row.status} />
                <div>{reasons[row.reason || ""] || row.reason}</div>
              </>
            ),
          },
          {
            title: "音视频证据",
            key: "media",
            render: (_, row) =>
              row.receiver_evidence
                ? `${row.receiver_evidence.media_duration_ms} ms / ${row.receiver_evidence.received_video_frames || 0} 视频帧`
                : "—",
          },
          {
            title: "实际选路",
            key: "proof",
            render: (_, row) => (
              <>
                {row.sender_route && (
                  <div>
                    发 <Badge value={row.sender_route.status} />
                  </div>
                )}
                {row.receiver_route && (
                  <div>
                    收 <Badge value={row.receiver_route.status} />
                  </div>
                )}
              </>
            ),
          },
          {
            title: "操作",
            key: "actions",
            render: (_, row) => (
              <Button
                size="small"
                disabled={!row.run_id}
                onClick={() => row.run_id && onOpen(row.run_id)}
              >
                调用与日志
              </Button>
            ),
          },
        ]}
      />
    </>
  );
}
export default function NativeCoverageDetails({
  api,
  taskID,
  state,
  runs,
  onClose,
  onOpen,
}: {
  api: API;
  taskID: string;
  state?: NativeState;
  runs: NativeRun[];
  onClose: () => void;
  onOpen: (id: string) => void;
}) {
  const [records, setRecords] = useState<Record<string, NativeRun>>({}),
    [error, setError] = useState("");
  const cache = useRef<Record<string, NativeRun>>({});
  useEffect(() => {
    let active = true;
    const load = async () => {
      const next: Record<string, NativeRun> = {};
      try {
        await Promise.all(
          Object.entries(state?.phases || {}).map(async ([phase, value]) => {
            const id = value.active_run_id || value.health.last_run_id;
            if (!id) return;
            const current =
              runs.find((run) => run.run_id === id) || cache.current[id];
            const record =
              current && !["RUNNING", "PREPARING"].includes(current.status)
                ? current
                : runs.find((run) => run.run_id === id) ||
                  (await api.get<NativeRun>(`/api/v1/rtc/runs/${id}`));
            next[phase] = record;
            cache.current[id] = record;
          }),
        );
        if (active) {
          setRecords(next);
          setError("");
        }
      } catch (e) {
        if (active)
          setError(e instanceof Error ? e.message : "覆盖详情读取失败");
      }
    };
    void load();
    return () => {
      active = false;
    };
  }, [api, state, runs]);
  return (
    <Drawer
      open
      title={`Native UDP / AUT 覆盖 · ${taskID}`}
      width="min(1160px, 100vw)"
      onClose={onClose}
    >
      {!state ? (
        <Empty description="等待首次 Native 执行计划" />
      ) : (
        <>
          {error && <Alert type="error" message={error} />}
          {state.scheduling_error && (
            <Alert type="error" message={state.scheduling_error} />
          )}
          {!!state.issues?.length && (
            <Alert
              type="warning"
              message={state.issues
                .map((reason) => reasons[reason] || reason)
                .join("；")}
            />
          )}
          <Typography.Paragraph type="secondary">
            已纳入 {state.expected.length} 个 Native 实例。AP 接入、固定 Edge
            和跨 Edge 分别验证后汇总整体状态。{state.cross_topology==="ring" && `跨 Edge 采用环形覆盖，每个实例各发送、接收一次；并发上限 ${state.worker_limit||1} 个工作进程。`}
          </Typography.Paragraph>
          <Space wrap style={{ marginBottom: 16 }}>
            {Object.entries(phases).map(([phase, label]) => {
              const value = state.phases[phase];
              if (!value) return null;
              return (
                <Card key={phase} size="small" title={label}>
                  <Badge value={value.health.state} />
                  {value.active_run_id && <Badge value="RUNNING" />}
                  <div>
                    {value.case_count} 项 / 已完成 {value.round} 轮
                  </div>
                  <div>
                    {reasons[value.health.reason] || value.health.reason}
                  </div>
                  {value.health.reason === "FAILURE_CONFIRMING" && (
                    <div>
                      失败确认 {value.health.failure_count}/
                      {value.health.failure_threshold}
                    </div>
                  )}
                  {value.health.reason === "RECOVERY_CONFIRMING" && (
                    <div>
                      恢复确认 {value.health.success_count}/
                      {value.health.recovery_threshold}
                    </div>
                  )}
                </Card>
              );
            })}
          </Space>
          <Tabs
            items={Object.entries(phases)
              .filter(([phase]) => state.phases[phase])
              .map(([phase, label]) => ({
                key: phase,
                label,
                children: records[phase] ? (
                  <NativeCases run={records[phase]} onOpen={onOpen} />
                ) : (
                  <Empty description="等待该类执行记录" />
                ),
              }))}
          />
          <Typography.Title level={5}>覆盖实例</Typography.Title>
          <Table<NativeMember>
            size="small"
            rowKey={(row) =>
              row.group + row.edge.agent_id + row.edge.container_name
            }
            dataSource={state.expected}
            pagination={false}
            scroll={{ x: 700 }}
            columns={[
              { title: "AP 分组", dataIndex: "group" },
              {
                title: "类型",
                key: "type",
                render: (_, row) => row.edge.transport.toUpperCase(),
              },
              {
                title: "客户端地址",
                key: "endpoint",
                render: (_, row) => row.edge.endpoint,
              },
              {
                title: "来源实例",
                key: "source",
                render: (_, row) =>
                  `${row.edge.agent_id} / ${row.edge.container_name}`,
              },
              {
                title: "发现状态",
                key: "fresh",
                render: (_, row) => (
                  <Tag color={row.edge.stale ? "warning" : "success"}>
                    {row.edge.stale ? "待验证" : "有效"}
                  </Tag>
                ),
              },
            ]}
          />
        </>
      )}
    </Drawer>
  );
}
