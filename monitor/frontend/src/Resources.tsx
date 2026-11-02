import { useState, type ReactNode } from "react";
import Tabs from "antd/es/tabs";
import Empty from "antd/es/empty";
import Typography from "antd/es/typography";
import Table from "antd/es/table";
import type {
  ResourceMetrics,
  ContainerLifecycle,
  HostFilesystem,
} from "./types";
import { MetricIcon } from "./MetricUI";
import ResourceTrends from "./ResourceTrends";
import type { API } from "./api";

const reasons: Record<string, string> = {
  CADVISOR_UNAVAILABLE: "采集器暂时不可用",
  CADVISOR_HTTP_ERROR: "指标读取失败",
  CADVISOR_CONTAINER_NOT_FOUND: "等待容器指标",
  CADVISOR_SAMPLE_STALE: "指标采样已过期",
  RESOURCE_STALE: "指标数据已过期",
  CONTAINER_NOT_RUNNING: "容器未运行",
  CADVISOR_INVALID_RESPONSE: "指标响应无效",
  CADVISOR_RESPONSE_TOO_LARGE: "指标响应超出限制",
  PARTIAL_METRICS: "部分指标尚不可用",
};
const number = (v: number | null | undefined, digits = 2) =>
  v == null
    ? "—"
    : v.toLocaleString("zh-CN", { maximumFractionDigits: digits });
const percent = (v: number | null | undefined) =>
  v == null ? "—" : `${(v * 100).toFixed(1)}%`;
const date = (v: string | null | undefined) =>
  v ? new Date(v).toLocaleString("zh-CN", { hour12: false }) : "—";
function updateTime(value: string | null | undefined, full = false) {
  if (!value) return "—";
  const at = new Date(value);
  if (!Number.isFinite(at.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  const clock = `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
  if (full)
    return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${clock}`;
  return at.toDateString() === new Date().toDateString()
    ? clock
    : `${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${clock}`;
}
function bytes(v: number | null | undefined) {
  if (v == null) return "—";
  for (const [unit, size] of [
    ["GiB", 1024 ** 3],
    ["MiB", 1024 ** 2],
    ["KiB", 1024],
  ] as const)
    if (v >= size) return `${(v / size).toFixed(1)} ${unit}`;
  return `${v.toFixed(0)} B`;
}
const rate = (v: number | null | undefined) =>
  v == null ? "—" : `${bytes(v)}/s`;
const cores = (v: number | null | undefined) =>
  v == null ? "—" : `${v > 0 && v < 0.001 ? "<0.001" : v.toFixed(3)} 核`;
function duration(start: string | null | undefined) {
  if (!start) return "—";
  const seconds = Math.max(
    0,
    Math.floor((Date.now() - Date.parse(start)) / 1000),
  );
  if (!Number.isFinite(seconds)) return "—";
  if (seconds >= 86400)
    return `${Math.floor(seconds / 86400)} 天 ${Math.floor((seconds % 86400) / 3600)} 小时`;
  if (seconds >= 3600)
    return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`;
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}
function OverviewItem({
  icon,
  label,
  value,
  note,
  ratio,
}: {
  icon: Parameters<typeof MetricIcon>[0]["name"];
  label: string;
  value: ReactNode;
  note: string;
  ratio?: number | null;
}) {
  return (
    <div className="resource-inline-metric" title={note}>
      <span className="resource-inline-label">
        <MetricIcon name={icon} />
        {label}
      </span>
      <strong>{value}</strong>
    </div>
  );
}
function Lifecycle({
  value,
  runtime,
}: {
  value?: ContainerLifecycle | null;
  runtime?: string;
}) {
  if (!value)
    return (
      <div className="metric-empty">
        <MetricIcon name="clock" />
        <strong>等待生命周期数据</strong>
        <p>Agent 上报后显示创建、启动及退出信息。</p>
      </div>
    );
  const previous =
    !!value.finished_at &&
    !!value.started_at &&
    Date.parse(value.finished_at) < Date.parse(value.started_at);
  return (
    <div className="lifecycle-unified">
      <Table
        size="small"
        bordered
        rowKey="name"
        pagination={false}
        tableLayout="fixed"
        columns={[
          { title: "指标", dataIndex: "name", width: "40%" },
          { title: "当前值", dataIndex: "value" },
        ]}
        dataSource={[
          { name: "创建时间", value: date(value.created_at) },
          { name: "本次启动时间", value: date(value.started_at) },
          {
            name: previous ? "上一轮结束时间" : "最近结束时间",
            value: date(value.finished_at),
          },
          {
            name: "本次运行时长",
            value:
              runtime === "running" && !value.stale
                ? duration(value.started_at)
                : "—",
          },
          {
            name: "Docker 自动重启次数",
            value: number(value.restart_count, 0) + " 次",
          },
          {
            name: "退出码",
            value:
              value.exit_code == null && runtime === "running"
                ? "运行中 · 不适用"
                : number(value.exit_code, 0),
          },
          {
            name: "OOMKilled",
            value:
              value.oom_killed == null
                ? "未知"
                : value.oom_killed
                  ? "是"
                  : "否",
          },
          {
            name: "Docker Healthcheck",
            value:
              value.docker_health === "healthy"
                ? "通过"
                : value.docker_health === "unhealthy"
                  ? "未通过"
                  : value.docker_health || "未配置",
          },
          { name: "最近观测时间", value: date(value.last_observed_at) },
          { name: "数据新鲜度", value: value.stale ? "已过期" : "有效" },
        ]}
      />
      <Typography.Paragraph type="secondary" className="metric-inline-note">
        重启次数为 Docker 自动重启计数；人工重启记录在“操作与审计”中。
      </Typography.Paragraph>
    </div>
  );
}
function Filesystem({
  value,
  current,
}: {
  value?: HostFilesystem | null;
  current: boolean;
}) {
  if (!value || !current)
    return (
      <div className="metric-empty">
        <MetricIcon name="disk" />
        <strong>数据盘信息暂不可用</strong>
        <p>等待 Agent 上报新的容量和 inode 数据。</p>
      </div>
    );
  return (
    <div className="filesystem-unified">
      <Table
        size="small"
        bordered
        rowKey="name"
        pagination={false}
        tableLayout="fixed"
        columns={[
          { title: "指标", dataIndex: "name", width: "40%" },
          { title: "当前值", dataIndex: "value" },
        ]}
        dataSource={[
          { name: "数据盘总容量", value: bytes(value.capacity_bytes) },
          { name: "可用空间", value: bytes(value.available_bytes) },
          { name: "空闲空间", value: bytes(value.free_bytes) },
          { name: "空间使用率", value: percent(value.used_ratio) },
          { name: "inode 总数", value: number(value.inodes_total, 0) },
          { name: "inode 剩余", value: number(value.inodes_free, 0) },
          { name: "inode 使用率", value: percent(value.inodes_used_ratio) },
          { name: "采集时间", value: date(value.collected_at) },
        ]}
      />
      <Typography.Paragraph type="secondary" className="metric-inline-note">
        宿主机 Docker 共享数据盘，容量不能按容器累加；空间与 inode
        任一耗尽均可能导致写入失败。
      </Typography.Paragraph>
    </div>
  );
}
export default function Resources({
  api,
  nodeID,
  value,
  compact = false,
  lifecycle,
  runtime,
  updatedAt,
  dataStale = false,
  startup,
  detailsOpen,
  onDetailsOpenChange,
}: {
  api: API;
  nodeID: string;
  value?: ResourceMetrics | null;
  compact?: boolean;
  lifecycle?: ContainerLifecycle | null;
  runtime?: string;
  updatedAt?: string | null;
  dataStale?: boolean;
  startup?: ReactNode;
  detailsOpen?: boolean;
  onDetailsOpenChange?: (open: boolean) => void;
}) {
  const [tab, setTab] = useState("resources"),
    [internalDetailsOpen, setInternalDetailsOpen] = useState(false);
  const isDetailsOpen = detailsOpen ?? internalDetailsOpen;
  const current = !!value && !value.stale && value.status !== "UNAVAILABLE";
  const f = value?.host_filesystem;
  const storageCurrent =
    !!f &&
    Date.now() - Date.parse(f.collected_at) <= 45000 &&
    Date.parse(f.collected_at) <= Date.now() + 5000;
  const liveDuration =
    runtime === "running" && lifecycle && !lifecycle.stale
      ? duration(lifecycle.started_at)
      : "—";
  return (
    <div className={`resource-view ${compact ? "resource-view-compact" : ""}`}>
      <div className="resource-overview">
        <OverviewItem
          icon="cpu"
          label="CPU 使用"
          value={current && value ? cores(value.cpu_usage_cores) : "—"}
          note={
            !current || !value
              ? "暂无有效资源采样"
              : value.cpu_quota_cores == null
                ? value.limits_known
                  ? "未设置配额"
                  : "配额未知"
                : `配额 ${cores(value.cpu_quota_cores)} · 节流 ${percent(value.cpu_throttled_ratio)}`
          }
        />
        <OverviewItem
          icon="memory"
          label="内存工作集"
          value={current && value ? bytes(value.memory_working_set_bytes) : "—"}
          note={
            !current || !value
              ? "暂无有效资源采样"
              : value.memory_limit_bytes
                ? `限额 ${bytes(value.memory_limit_bytes)} · ${percent(value.memory_working_set_ratio)}`
                : value.limits_known
                  ? "未设置内存限额"
                  : "限额未知"
          }
          ratio={current ? value?.memory_working_set_ratio : undefined}
        />
        <OverviewItem
          icon="clock"
          label="数据更新时间"
          value={
            <span className={dataStale ? "metric-update-stale" : undefined}>
              {updateTime(updatedAt)}
              {dataStale && <small> · 已过期</small>}
            </span>
          }
          note={
            updatedAt
              ? `${updateTime(updatedAt, true)}${dataStale ? " · 数据已过期" : ""}`
              : "暂无有效采集时间"
          }
        />
        {!compact && (
          <OverviewItem
            icon="clock"
            label="本次运行"
            value={liveDuration}
            note={
              lifecycle?.stale
                ? "生命周期已过期"
                : `自动重启 ${number(lifecycle?.restart_count, 0)} 次`
            }
          />
        )}
      </div>
      {!current && (
        <div className="resource-unavailable">
          <span className="status-dot" />
          <div>
            <strong>
              {value
                ? reasons[value.reason_code] || "指标暂不可用"
                : "资源指标未接入"}
            </strong>
            <span>
              {value?.sampled_at
                ? `最后采样 ${date(value.sampled_at)}`
                : "生命周期和数据盘信息仍可单独查看。"}
            </span>
          </div>
        </div>
      )}
      {isDetailsOpen && (
        <div className="resource-details">
          <Tabs
            destroyOnHidden
            activeKey={tab}
            onChange={setTab}
            items={[
              ...(startup
                ? [{ key: "startup", label: "启动参数", children: startup }]
                : []),
              {
                key: "resources",
                label: "资源详情",
                children: (
                  <ResourceTrends api={api} nodeID={nodeID} value={value} />
                ),
              },
              {
                key: "filesystem",
                label: "文件系统",
                children: <Filesystem value={f} current={storageCurrent} />,
              },
              {
                key: "lifecycle",
                label: "生命周期",
                children: <Lifecycle value={lifecycle} runtime={runtime} />,
              },
            ]}
          />
        </div>
      )}
    </div>
  );
}
