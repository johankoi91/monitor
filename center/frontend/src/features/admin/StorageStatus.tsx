import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  DatabaseOutlined,
  ExclamationCircleOutlined,
  HddOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Empty from "antd/es/empty";
import Progress from "antd/es/progress";
import Skeleton from "antd/es/skeleton";
import Table from "antd/es/table";
import { useEffect, useState } from "react";
import type { API } from "../../shared/api";

export type Usage = {
  backend: string;
  database_bytes: number;
  warning_bytes: number;
  disk_capacity_bytes: number;
  disk_available_bytes: number;
  warnings: string[];
  tables: { name: string; bytes: number }[];
  sessions: {
    checked_at: string | null;
    deleted_sessions: number;
    error: string;
  };
};
const bytes = (n: number) =>
  n >= 1073741824
    ? `${(n / 1073741824).toFixed(2)} GiB`
    : n >= 1048576
      ? `${(n / 1048576).toFixed(1)} MiB`
      : n >= 1024
        ? `${(n / 1024).toFixed(1)} KiB`
        : `${n} B`;
const labels: Record<string, string> = {
  DATABASE_SIZE_WARNING: "数据库占用达到提醒阈值",
  DISK_SPACE_LOW: "数据盘可用空间不足",
  DISK_USAGE_UNAVAILABLE: "数据盘容量暂不可读",
  SESSION_CLEANUP_FAILED: "过期会话清理失败",
};
const categories: Record<string, string> = {
  resource_samples: "资源采样",
  probe_runs_v2: "探测执行记录",
  probe_logs_v2: "探测诊断记录",
  notification_events: "通知投递",
  notification_audit: "通知审计",
  system_audit: "系统审计",
  operation_audit: "操作审计",
  operations: "重启操作",
  documents: "配置与状态",
  baseline_versions: "监控配置版本",
  containers: "容器快照",
  agent_snapshots: "主机快照",
  sessions: "登录会话",
};
const tableLabel = (name: string) =>
  categories[name.replace(/^avops\./, "")] || name;
export function StoragePanel({
  usage,
  error = "",
  loading = false,
  updatedAt,
  onRefresh,
}: {
  usage: Usage | null;
  error?: string;
  loading?: boolean;
  updatedAt?: string;
  onRefresh: () => void;
}) {
  const tables = [...(usage?.tables || [])].sort((a, b) => b.bytes - a.bytes);
  const total = tables.reduce((sum, table) => sum + table.bytes, 0);
  const diskKnown = !!usage && usage.disk_capacity_bytes > 0;
  const diskPercent = diskKnown
    ? Math.min(
        100,
        Math.max(
          0,
          100 * (1 - usage!.disk_available_bytes / usage!.disk_capacity_bytes),
        ),
      )
    : 0;
  const warnings = usage?.warnings || [];
  const freePercent = diskKnown ? 100 - diskPercent : null;
  if (usage?.backend === "file-dev")
    return (
      <section className="capacity-suite">
        <Empty description="开发存储不提供数据库容量统计" />
      </section>
    );
  return (
    <section className="capacity-suite" aria-label="中心容量详情">
      <div className="capacity-toolbar">
        <span
          className={`capacity-health ${error || warnings.length ? "is-warning" : !usage ? "is-neutral" : ""}`}
          role="status"
        >
          {error || warnings.length ? (
            <ExclamationCircleOutlined aria-hidden />
          ) : !usage ? (
            <ClockCircleOutlined aria-hidden />
          ) : (
            <CheckCircleOutlined aria-hidden />
          )}
          {error
            ? "读取失败"
            : !usage
              ? "正在读取"
              : warnings.length
                ? "需要关注"
                : "运行正常"}
        </span>
        <div>
          {updatedAt && (
            <time dateTime={updatedAt}>
              {new Date(updatedAt).toLocaleTimeString("zh-CN", {
                hour12: false,
              })}{" "}
              更新
            </time>
          )}
          <Button
            icon={<ReloadOutlined aria-hidden />}
            loading={loading}
            onClick={onRefresh}
          >
            刷新
          </Button>
        </div>
      </div>
      {error && (
        <Alert
          type="warning"
          showIcon
          message={error}
          description={usage ? "显示最近一次读取的数据" : undefined}
        />
      )}
      {!usage && !error && <Skeleton active paragraph={{ rows: 7 }} />}
      {usage && (
        <>
          {!!warnings.length && (
            <div className="capacity-alerts">
              {warnings.map((code) => (
                <Alert
                  key={code}
                  type="warning"
                  showIcon
                  message={labels[code] || code}
                />
              ))}
            </div>
          )}
          <div className="capacity-metrics">
            <article className="capacity-metric">
              <div>
                <span>数据库占用</span>
                <DatabaseOutlined aria-hidden />
              </div>
              <strong>{bytes(usage.database_bytes)}</strong>
              <span className="capacity-muted">
                提醒阈值 {bytes(usage.warning_bytes)}
              </span>
            </article>
            <article className="capacity-metric">
              <div>
                <span>数据盘可用</span>
                <HddOutlined aria-hidden />
              </div>
              <strong>
                {diskKnown ? bytes(usage.disk_available_bytes) : "—"}
              </strong>
              <span className="capacity-muted">
                {diskKnown
                  ? `剩余 ${freePercent!.toFixed(1)}% · 总计 ${bytes(usage.disk_capacity_bytes)}`
                  : "容量暂不可读"}
              </span>
            </article>
            <article className="capacity-metric">
              <div>
                <span>会话清理</span>
                <ClockCircleOutlined aria-hidden />
              </div>
              <strong className="capacity-cleanup-value">
                {usage.sessions?.error
                  ? "清理失败"
                  : usage.sessions?.checked_at
                    ? "已完成"
                    : "待检查"}
              </strong>
              <span className="capacity-muted">
                {usage.sessions?.checked_at
                  ? `最近清理 ${usage.sessions.deleted_sessions} 条 · ${new Date(usage.sessions.checked_at).toLocaleString("zh-CN", { hour12: false })}`
                  : "等待首次清理"}
              </span>
            </article>
          </div>
          <div className="capacity-panels">
            <section className="capacity-panel">
              <div className="capacity-panel-heading">
                <h3>数据盘使用</h3>
                <span>
                  {diskKnown ? `${diskPercent.toFixed(1)}% 已使用` : "待确认"}
                </span>
              </div>
              <div className="capacity-disk">
                <strong>
                  {diskKnown
                    ? bytes(
                        usage.disk_capacity_bytes - usage.disk_available_bytes,
                      )
                    : "—"}
                  <small>已使用</small>
                </strong>
                {diskKnown ? (
                  <Progress
                    percent={Number(diskPercent.toFixed(1))}
                    showInfo={false}
                    strokeColor={
                      warnings.includes("DISK_SPACE_LOW")
                        ? "#ad6800"
                        : "#0958d9"
                    }
                    aria-label={`数据盘已使用 ${diskPercent.toFixed(1)}%`}
                  />
                ) : (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="无法读取数据盘容量"
                  />
                )}
                <div className="capacity-disk-legend">
                  <span>
                    <i aria-hidden />
                    已使用 {diskKnown ? `${diskPercent.toFixed(1)}%` : "—"}
                  </span>
                  <span>
                    <i aria-hidden />
                    可用{" "}
                    {freePercent === null ? "—" : `${freePercent.toFixed(1)}%`}
                  </span>
                </div>
              </div>
            </section>
            <section className="capacity-panel">
              <div className="capacity-panel-heading">
                <h3>数据库构成</h3>
                <span>占已统计数据表</span>
              </div>
              <div className="capacity-top-tables">
                {tables.length ? (
                  tables.slice(0, 4).map((table) => (
                    <div key={table.name}>
                      <div>
                        <span>{tableLabel(table.name)}</span>
                        <strong>{bytes(table.bytes)}</strong>
                      </div>
                      <Progress
                        percent={total ? (100 * table.bytes) / total : 0}
                        showInfo={false}
                        strokeColor="#698bb9"
                        aria-label={`${tableLabel(table.name)}占 ${total ? ((100 * table.bytes) / total).toFixed(1) : 0}%`}
                      />
                    </div>
                  ))
                ) : (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="暂无数据表统计"
                  />
                )}
              </div>
            </section>
          </div>
          <section className="capacity-panel capacity-details">
            <div className="capacity-panel-heading">
              <h3>数据表明细</h3>
              <span>
                {tables.length} 张表 · 合计 {bytes(total)}
              </span>
            </div>
            <Table
              size="small"
              rowKey="name"
              dataSource={tables}
              pagination={
                tables.length > 8
                  ? { pageSize: 8, size: "small", showSizeChanger: false }
                  : false
              }
              scroll={{ x: 560 }}
              columns={[
                {
                  title: "数据分类",
                  dataIndex: "name",
                  render: (name: string) => (
                    <div className="capacity-table-name">
                      <strong>{tableLabel(name)}</strong>
                      {tableLabel(name) !== name && <code>{name}</code>}
                    </div>
                  ),
                },
                {
                  title: "占用（含索引）",
                  dataIndex: "bytes",
                  align: "right",
                  width: 160,
                  render: bytes,
                  sorter: (a, b) => a.bytes - b.bytes,
                },
                {
                  title: "表空间占比",
                  width: 200,
                  render: (_, table) => (
                    <div className="capacity-table-share">
                      <Progress
                        percent={total ? (100 * table.bytes) / total : 0}
                        showInfo={false}
                      />
                      <span>
                        {total
                          ? ((100 * table.bytes) / total).toFixed(1)
                          : "0.0"}
                        %
                      </span>
                    </div>
                  ),
                },
              ]}
            />
          </section>
          <div className="capacity-policy">
            <span>容量提醒阈值不设硬配额</span>
            <span>过期会话每小时清理，保留 24 小时缓冲</span>
          </div>
        </>
      )}
    </section>
  );
}
export default function StorageStatus({ api }: { api: API }) {
  const [usage, setUsage] = useState<Usage | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [updatedAt, setUpdatedAt] = useState<string>(),
    [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true,
      inFlight = false;
    const load = async () => {
      if (inFlight) return;
      inFlight = true;
      if (active) setLoading(true);
      try {
        const data = await api.get<Usage>("/api/v1/storage/status");
        if (active) {
          setUsage(data);
          setError("");
          setUpdatedAt(new Date().toISOString());
        }
      } catch (error) {
        if (active)
          setError(error instanceof Error ? error.message : "容量读取失败");
      } finally {
        inFlight = false;
        if (active) setLoading(false);
      }
    };
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 60000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [api, retry]);
  return (
    <StoragePanel
      usage={usage}
      error={error}
      loading={loading}
      updatedAt={updatedAt}
      onRefresh={() => setRetry((value) => value + 1)}
    />
  );
}
