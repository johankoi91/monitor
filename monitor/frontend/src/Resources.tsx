import { useState, type ReactNode } from "react";
import Collapse from "antd/es/collapse";
import Tabs from "antd/es/tabs";
import Empty from "antd/es/empty";
import Typography from "antd/es/typography";
import Timeline from "antd/es/timeline";
import Card from "antd/es/card";
import Statistic from "antd/es/statistic";
import type {
  ResourceMetrics,
  ContainerLifecycle,
  HostFilesystem,
} from "./types";
import { MetricIcon, MetricRow, MetricPanel, Meter } from "./MetricUI";

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
const time = (v: string | null | undefined) =>
  v ? new Date(v).toLocaleTimeString("zh-CN", { hour12: false }) : "—";
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
    <Card size="small" className="resource-tile">
      <Statistic
        title={
          <>
            <MetricIcon name={icon} /> {label}
          </>
        }
        value={0}
        formatter={() => value}
      />
      <Typography.Text type="secondary">{note}</Typography.Text>
      <Meter ratio={ratio} label={label} />
    </Card>
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
    <div className="metric-panel-grid">
      <MetricPanel
        title="运行时间线"
        icon="clock"
        tag={value.stale ? "数据过期" : "Docker 观测"}
      >
        <Timeline
          items={[
            {
              children: (
                <>
                  创建容器{" "}
                  <Typography.Text type="secondary">
                    {date(value.created_at)}
                  </Typography.Text>
                </>
              ),
            },
            ...(previous
              ? [
                  {
                    color: "gray",
                    children: (
                      <>
                        上一轮结束{" "}
                        <Typography.Text type="secondary">
                          {date(value.finished_at)}
                        </Typography.Text>
                      </>
                    ),
                  },
                ]
              : []),
            {
              color: runtime === "running" && !value.stale ? "green" : "gray",
              children: (
                <>
                  本次启动{" "}
                  <Typography.Text type="secondary">
                    {date(value.started_at)}
                  </Typography.Text>
                </>
              ),
            },
            ...(!previous && value.finished_at
              ? [
                  {
                    color: "gray",
                    children: (
                      <>
                        最近结束{" "}
                        <Typography.Text type="secondary">
                          {date(value.finished_at)}
                        </Typography.Text>
                      </>
                    ),
                  },
                ]
              : []),
            {
              children: (
                <>
                  最近观测{" "}
                  <Typography.Text type="secondary">
                    {date(value.last_observed_at)}
                  </Typography.Text>
                </>
              ),
            },
          ]}
        />
      </MetricPanel>
      <MetricPanel
        title="运行与退出"
        icon="cpu"
        note="重启次数来自 Docker 自动重启计数；人工重启记录在操作与审计中。"
      >
        <div className="metric-list">
          <MetricRow
            label="本次运行时长"
            value={
              runtime === "running" && !value.stale
                ? duration(value.started_at)
                : "—"
            }
          />
          <MetricRow
            label="自动重启"
            value={`${number(value.restart_count, 0)} 次`}
          />
          <MetricRow
            label="退出码"
            value={
              value.exit_code == null && runtime === "running"
                ? "运行中 · 不适用"
                : number(value.exit_code, 0)
            }
            muted={value.exit_code == null}
          />
          <MetricRow
            label="OOMKilled"
            value={
              value.oom_killed == null ? "未知" : value.oom_killed ? "是" : "否"
            }
          />
          <MetricRow
            label="Healthcheck"
            value={
              value.docker_health === "healthy"
                ? "通过"
                : value.docker_health === "unhealthy"
                  ? "未通过"
                  : value.docker_health || "未配置"
            }
            muted={!value.docker_health}
          />
        </div>
      </MetricPanel>
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
    <div className="metric-panel-grid">
      <MetricPanel
        title="Docker 数据盘"
        icon="disk"
        tag="宿主机共享"
        note="同机容器共享这块数据盘，容量不能按容器累加。"
      >
        <div className="filesystem-hero">
          <span>空间使用率</span>
          <strong>{percent(value.used_ratio)}</strong>
        </div>
        <Meter ratio={value.used_ratio} label="数据盘使用率" />
        <div className="metric-list">
          <MetricRow label="总容量" value={bytes(value.capacity_bytes)} />
          <MetricRow label="可用空间" value={bytes(value.available_bytes)} />
          <MetricRow label="空闲空间" value={bytes(value.free_bytes)} />
        </div>
      </MetricPanel>
      <MetricPanel
        title="inode"
        icon="disk"
        tag="文件数量资源"
        note="inode 与磁盘字节容量分别计算；任一耗尽都可能使文件写入失败。"
      >
        <div className="filesystem-hero">
          <span>inode 使用率</span>
          <strong>{percent(value.inodes_used_ratio)}</strong>
        </div>
        <Meter ratio={value.inodes_used_ratio} label="inode 使用率" />
        <div className="metric-list">
          <MetricRow label="总数" value={number(value.inodes_total, 0)} />
          <MetricRow label="剩余" value={number(value.inodes_free, 0)} />
          <MetricRow label="采集时间" value={date(value.collected_at)} />
        </div>
      </MetricPanel>
    </div>
  );
}
function ResourcePanels({ value }: { value: ResourceMetrics }) {
  const io = !!value.disk_devices?.length;
  return (
    <div className="metric-panel-grid">
      <MetricPanel
        title="CPU"
        icon="cpu"
        note="1 核表示一个逻辑核。节流率是受限调度周期的比例。"
      >
        <div className="metric-list">
          <MetricRow label="使用核数" value={cores(value.cpu_usage_cores)} />
          <MetricRow
            label="用户态 / 内核态"
            value={`${cores(value.cpu_user_usage_cores)} / ${cores(value.cpu_system_usage_cores)}`}
          />
          <MetricRow
            label="配额"
            value={
              value.cpu_quota_cores == null
                ? value.limits_known
                  ? "未设置"
                  : "未知"
                : cores(value.cpu_quota_cores)
            }
            muted={value.cpu_quota_cores == null}
          />
          <MetricRow
            label="节流率"
            value={
              value.cpu_quota_cores == null
                ? value.limits_known
                  ? "未设置配额"
                  : "配额未知"
                : percent(value.cpu_throttled_ratio)
            }
            muted={value.cpu_quota_cores == null}
          />
          <MetricRow
            label="节流时间增长"
            value={
              value.cpu_throttled_seconds_per_second == null
                ? "—"
                : `${number(value.cpu_throttled_seconds_per_second, 3)} 秒/秒`
            }
          />
        </div>
      </MetricPanel>
      <MetricPanel
        title="内存"
        icon="memory"
        note="工作集用于计算限额占比；分配失败次数不等于 OOM 次数。"
      >
        <div className="metric-list">
          <MetricRow
            label="工作集"
            value={bytes(value.memory_working_set_bytes)}
          />
          <MetricRow
            label="限额 / 占比"
            value={
              value.memory_limit_bytes
                ? `${bytes(value.memory_limit_bytes)} / ${percent(value.memory_working_set_ratio)}`
                : value.limits_known
                  ? "未设置限额"
                  : "限额未知"
            }
            muted={!value.memory_limit_bytes}
          />
          <MetricRow
            label="使用量 / RSS"
            value={`${bytes(value.memory_usage_bytes)} / ${bytes(value.memory_rss_bytes)}`}
          />
          <MetricRow
            label="缓存 / Swap"
            value={`${bytes(value.memory_cache_bytes)} / ${bytes(value.memory_swap_bytes)}`}
          />
          <MetricRow label="峰值" value={bytes(value.memory_peak_bytes)} />
          <MetricRow
            label="分配失败 / OOM 事件"
            value={`${number(value.memory_failures_total, 0)} / ${number(value.oom_events_total, 0)}`}
          />
        </div>
        <div className="metric-inline-note">
          OOM 事件从采集器启动以来累计，重启后重新计数。
        </div>
      </MetricPanel>
      <MetricPanel
        title="磁盘 I/O"
        icon="disk"
        tag={io ? "按设备" : "暂无有效样本"}
        note={
          io
            ? "逻辑盘与底层设备可能记录同一笔 I/O，分别展示，避免重复求和。"
            : "未取得有效计数时留空，不用 0 代替未知。"
        }
      >
        {io ? (
          <div className="device-list">
            {value.disk_devices!.map((d) => (
              <div className="device-metrics" key={`${d.major}:${d.minor}`}>
                <div className="device-heading">
                  <strong>{d.device}</strong>
                  <span>
                    {d.major}:{d.minor}
                  </span>
                </div>
                <div className="metric-list">
                  <MetricRow
                    label="读取 / 写入"
                    value={`${rate(d.read_bytes_per_second)} / ${rate(d.write_bytes_per_second)}`}
                  />
                  <MetricRow
                    label="读取 / 写入 IOPS"
                    value={`${number(d.read_operations_per_second)} / ${number(d.write_operations_per_second)}`}
                  />
                </div>
              </div>
            ))}
          </div>
        ) : value.disk_io_status === "AVAILABLE" ? (
          <div className="metric-list">
            <MetricRow
              label="读取 / 写入"
              value={`${rate(value.disk_read_bytes_per_second)} / ${rate(value.disk_write_bytes_per_second)}`}
            />
            <MetricRow
              label="IOPS"
              value={`${number(value.disk_read_operations_per_second)} / ${number(value.disk_write_operations_per_second)}`}
            />
          </div>
        ) : (
          <div className="metric-empty compact-empty">
            <MetricIcon name="disk" />
            <strong>
              {value.disk_io_status === "UNSUPPORTED"
                ? "当前环境未提供 I/O 指标"
                : "等待有效 I/O 采样"}
            </strong>
          </div>
        )}
      </MetricPanel>
      <MetricPanel
        title="网络"
        icon="network"
        tag={value.network_scope === "HOST_SHARED" ? "主机共享" : "容器网络"}
        note={
          value.network_scope === "HOST_SHARED"
            ? "host/container 模式共享网络命名空间，不能把主机流量归属到单个容器。"
            : "接口丢包不等于 RTC 媒体丢包率。"
        }
      >
        {value.network_scope === "HOST_SHARED" ? (
          <div className="metric-empty compact-empty">
            <MetricIcon name="network" />
            <strong>共享主机网络</strong>
            <p>不展示无法单独归属的流量、丢包和错误。</p>
          </div>
        ) : (
          <div className="metric-list">
            <MetricRow
              label="接收 / 发送"
              value={`${rate(value.network_receive_bytes_per_second)} / ${rate(value.network_transmit_bytes_per_second)}`}
            />
            <MetricRow
              label="收包 / 发包"
              value={`${number(value.network_receive_packets_per_second)} / ${number(value.network_transmit_packets_per_second)} 包/秒`}
            />
            <MetricRow
              label="接收 / 发送丢包"
              value={`${number(value.network_receive_dropped_per_second)} / ${number(value.network_transmit_dropped_per_second)} 包/秒`}
            />
            <MetricRow
              label="接收 / 发送错误"
              value={`${number(value.network_receive_errors_per_second)} / ${number(value.network_transmit_errors_per_second)} 次/秒`}
            />
          </div>
        )}
      </MetricPanel>
    </div>
  );
}
export default function Resources({
  value,
  compact = false,
  lifecycle,
  runtime,
}: {
  value?: ResourceMetrics | null;
  compact?: boolean;
  lifecycle?: ContainerLifecycle | null;
  runtime?: string;
}) {
  const [tab, setTab] = useState("resources");
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
      {current && value ? (
        <div className="resource-overview">
          <OverviewItem
            icon="cpu"
            label="CPU 使用"
            value={cores(value.cpu_usage_cores)}
            note={
              value.cpu_quota_cores == null
                ? value.limits_known
                  ? "未设置配额"
                  : "配额未知"
                : `配额 ${cores(value.cpu_quota_cores)} · 节流 ${percent(value.cpu_throttled_ratio)}`
            }
          />
          <OverviewItem
            icon="memory"
            label="内存工作集"
            value={bytes(value.memory_working_set_bytes)}
            note={
              value.memory_limit_bytes
                ? `限额 ${bytes(value.memory_limit_bytes)} · ${percent(value.memory_working_set_ratio)}`
                : value.limits_known
                  ? "未设置内存限额"
                  : "限额未知"
            }
            ratio={value.memory_working_set_ratio}
          />
          <OverviewItem
            icon="disk"
            label="容器文件系统"
            value={bytes(value.filesystem_usage_bytes)}
            note="不代表全部挂载卷占用"
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
      ) : (
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
      <Collapse
        ghost
        items={[
          {
            key: "details",
            label: "资源与生命周期详情",
            extra: (
              <Typography.Text type="secondary">
                采样 {time(value?.sampled_at)}
              </Typography.Text>
            ),
            children: (
              <Tabs
                activeKey={tab}
                onChange={setTab}
                items={[
                  {
                    key: "resources",
                    label: "资源详情",
                    children:
                      current && value ? (
                        <>
                          <ResourcePanels value={value} />
                          <Typography.Paragraph type="secondary">
                            实际采样窗口 {number(value.sample_window_seconds)}{" "}
                            秒 · {date(value.sampled_at)}
                          </Typography.Paragraph>
                        </>
                      ) : (
                        <Empty description="等待新的采样；缺失数据不会显示为 0" />
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
            ),
          },
        ]}
      />
    </div>
  );
}
