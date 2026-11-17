import { useEffect, useRef } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Grid from "antd/es/grid";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Skeleton from "antd/es/skeleton";
import Statistic from "antd/es/statistic";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import {
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import type { API } from "./api";
import type { NodeStatus, ServiceStatus } from "./types";
import Resources from "./Resources";
import StartupParameters from "./StartupParameters";
import RTCOverview from "./RTCOverview";
import { useViewParam } from "./viewState";

type Row = NodeStatus & { group: string; groupCode: string };
const timestamp = (at?: string | null) =>
  at ? new Date(at).toLocaleString("zh-CN", { hour12: false }) : "暂无";
const cpu = (v?: number | null) =>
  v == null ? "—" : v > 0 && v < 0.001 ? "<0.001" : v.toFixed(3);
const memory = (v?: number | null) =>
  v == null
    ? "—"
    : v >= 1024 ** 3
      ? `${(v / 1024 ** 3).toFixed(1)} GiB`
      : `${(v / 1024 ** 2).toFixed(1)} MiB`;
const health = (n: NodeStatus, stale: boolean) =>
  stale || n.stale ? "UNKNOWN" : n.status;
function Status({ value, stale }: { value: string; stale?: boolean }) {
  const good = value === "HEALTHY",
    bad = ["UNHEALTHY", "DEGRADED"].includes(value);
  return (
    <Tag
      className="health-tag"
      color={good ? "success" : bad ? "error" : "warning"}
      icon={
        good ? (
          <CheckCircleOutlined />
        ) : bad ? (
          <ExclamationCircleOutlined />
        ) : (
          <QuestionCircleOutlined />
        )
      }
    >
      {stale ? "待确认" : good ? "正常" : bad ? "异常" : "未知"}
    </Tag>
  );
}

export default function ServiceStatusPanel({
  api,
  services,
  loading,
  loadedAt,
  error,
  stale,
  onRefresh,
  onInventory,
  onRTC,
  onRestart,
  canRestart,
}: {
  api: API;
  services: ServiceStatus[];
  loading: boolean;
  loadedAt: number;
  error: string;
  stale: boolean;
  onRefresh: () => void;
  onInventory: () => void;
  onRTC: () => void;
  onRestart: (n: NodeStatus) => void;
  canRestart: boolean;
}) {
  const [filter, setFilter] = useViewParam("health", "all");
  const [search, setSearch] = useViewParam("search");
  const [host, setHost] = useViewParam("host");
  const [group, setGroup] = useViewParam("group");
  const [sort, setSort] = useViewParam("sort", "priority");
  const [selected, setSelected] = useViewParam("node");
  const screens = Grid.useBreakpoint();
  const detailRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const rows: Row[] = services.flatMap((s) =>
    s.nodes.map((n) => ({
      ...n,
      group: s.group_name || s.cluster_code,
      groupCode: s.cluster_code,
    })),
  );
  const counts = {
    all: rows.length,
    bad: rows.filter((n) =>
      ["UNHEALTHY", "DEGRADED"].includes(health(n, stale)),
    ).length,
    unknown: rows.filter(
      (n) => !["HEALTHY", "UNHEALTHY", "DEGRADED"].includes(health(n, stale)),
    ).length,
    ok: rows.filter((n) => health(n, stale) === "HEALTHY").length,
  };
  const visible = rows
    .filter((n) => {
      const value = health(n, stale);
      const match =
        filter === "all" ||
        (filter === "ok" && value === "HEALTHY") ||
        (filter === "bad" && ["UNHEALTHY", "DEGRADED"].includes(value)) ||
        (filter === "unknown" &&
          !["HEALTHY", "UNHEALTHY", "DEGRADED"].includes(value));
      return (
        match &&
        (!host || n.host_address === host) &&
        (!group || n.groupCode === group) &&
        `${n.container_name} ${n.image} ${n.host_address}`
          .toLowerCase()
          .includes(search.trim().toLowerCase())
      );
    })
    .sort((a, b) => {
      const priority = (n: Row) =>
        ["UNHEALTHY", "DEGRADED"].includes(health(n, stale))
          ? 0
          : health(n, stale) === "HEALTHY"
            ? 2
            : 1;
      return (
        (sort === "name" ? 0 : priority(a) - priority(b)) ||
        a.container_name.localeCompare(b.container_name)
      );
    });
  const current = rows.find((n) => n.node_id === selected);
  useEffect(() => {
    if (selected && current) detailRef.current?.focus();
  }, [selected, !!current]);
  const show = (n: Row, button: HTMLElement) => {
    triggerRef.current = button;
    setSelected(n.node_id === selected ? "" : n.node_id);
  };
  const close = () => {
    setSelected("");
    const id = selected;
    requestAnimationFrame(() => {
      const trigger = triggerRef.current?.isConnected
        ? triggerRef.current
        : document.getElementById(`node-details-${encodeURIComponent(id)}`);
      trigger?.focus();
    });
  };
  const action = (n: Row) => (
    <Button
      id={`node-details-${encodeURIComponent(n.node_id)}`}
      type="link"
      aria-label={`查看 ${n.container_name} 详情`}
      aria-expanded={n.node_id === selected}
      aria-controls="service-node-details"
      onClick={(e) => show(n, e.currentTarget)}
    >
      查看详情
    </Button>
  );
  const nodeStatus = (n: Row) => (
    <>
      <Status value={health(n, stale)} stale={stale} />
      <div className="status-row-reason">
        {stale ? "历史快照 · 当前状态待确认" : n.reason || "等待有效快照"}
      </div>
    </>
  );
  const validMetrics = (n: Row) =>
    !stale &&
    !n.stale &&
    n.resources &&
    !n.resources.stale &&
    n.resources.status !== "UNAVAILABLE";
  const initialLoading = loading && !loadedAt;
  return (
    <section className="status-screen" aria-labelledby="status-page-title">
      <div className="status-page-heading">
        <div>
          <Typography.Title id="status-page-title" level={2}>
            服务状态
          </Typography.Title>
          <div className="status-update" role="status" aria-live="polite">
            {!loadedAt ? (
              initialLoading ? (
                "正在读取监控数据…"
              ) : (
                "尚未获得有效数据"
              )
            ) : (
              <>
                <Tag color={stale ? "warning" : "success"}>
                  {stale ? "数据待确认" : "采集正常"}
                </Tag>
                最后成功读取 {timestamp(new Date(loadedAt).toISOString())}
              </>
            )}
          </div>
        </div>
        <div className="status-refresh">
          <Typography.Text type="secondary">每 15 秒更新</Typography.Text>
          <Button
            icon={<ReloadOutlined />}
            loading={loading}
            onClick={onRefresh}
          >
            刷新
          </Button>
        </div>
      </div>
      {(error || (stale && loadedAt)) && (
        <Alert
          className="page-alert"
          type="warning"
          showIcon
          message={
            loadedAt
              ? "监控数据暂未更新，当前状态待确认"
              : "暂时无法读取监控数据"
          }
          description={`${error || "超过有效刷新时间"}${loadedAt ? "。以下保留最后成功快照，不能作为当前状态。" : "。请检查连接后重试。"}`}
          action={
            <Button onClick={onRefresh} loading={loading}>
              重新连接
            </Button>
          }
        />
      )}
      <div className="status-summary">
        {(
          [
            ["all", "全部容器", "已纳管"],
            ["bad", "异常", "优先处理"],
            ["unknown", "未知", "核对采集"],
            ["ok", "正常", "当前检查通过"],
          ] as const
        ).map(([key, label, note]) => (
          <button
            type="button"
            className={`status-summary-button status-summary-${key}`}
            key={key}
            aria-pressed={filter === key}
            onClick={() => setFilter(key)}
          >
            <Statistic
              title={label}
              value={!loadedAt || (stale && key !== "all") ? "—" : counts[key]}
            />
            <span>{!loadedAt ? "待获取" : stale ? "历史快照" : note}</span>
          </button>
        ))}
      </div>
      <RTCOverview api={api} onOpen={onRTC} />
      <Card
        className="status-table-card"
        title="纳管容器"
        extra={
          <Typography.Text type="secondary">
            {sort === "priority" ? "异常与未知优先" : "按名称排列"}
          </Typography.Text>
        }
      >
        <Form layout="vertical" component="div" className="status-filters-row">
          <Form.Item label="搜索容器、镜像或主机 IP" htmlFor="status-search">
            <Input
              id="status-search"
              allowClear
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="输入名称、镜像或 IP"
            />
          </Form.Item>
          <Form.Item label="主机" htmlFor="status-host">
            <Select
              id="status-host"
              value={host}
              onChange={setHost}
              options={[
                { value: "", label: "全部主机" },
                ...[...new Set(rows.map((n) => n.host_address))]
                  .sort()
                  .map((value) => ({ value, label: value })),
              ]}
            />
          </Form.Item>
          <Form.Item label="分组" htmlFor="status-group">
            <Select
              id="status-group"
              value={group}
              onChange={setGroup}
              options={[
                { value: "", label: "全部分组" },
                ...[
                  ...new Map(rows.map((n) => [n.groupCode, n.group])).entries(),
                ].map(([value, label]) => ({ value, label })),
              ]}
            />
          </Form.Item>
          <Form.Item label="排序" htmlFor="status-sort">
            <Select
              id="status-sort"
              value={sort}
              onChange={setSort}
              options={[
                { value: "priority", label: "异常优先" },
                { value: "name", label: "容器名称" },
              ]}
            />
          </Form.Item>
        </Form>
        {initialLoading ? (
          <div className="status-loading" aria-busy="true">
            <Skeleton active={false} paragraph={{ rows: 5 }} />
          </div>
        ) : !loadedAt ? (
          <div className="status-empty">
            <Typography.Title level={3}>等待有效监控数据</Typography.Title>
            <Button onClick={onRefresh}>重试</Button>
          </div>
        ) : !rows.length ? (
          <div className="status-empty">
            <Typography.Title level={3}>尚未保存基准</Typography.Title>
            <Typography.Paragraph>
              选择需要监控的容器，设置分组后保存生效。
            </Typography.Paragraph>
            <Button type="primary" onClick={onInventory}>
              前往容器发现
            </Button>
          </div>
        ) : !visible.length ? (
          <div className="status-empty">
            <Typography.Paragraph>没有匹配的容器</Typography.Paragraph>
            <Button
              onClick={() => {
                setFilter("all");
                setSearch("");
                setHost("");
                setGroup("");
              }}
            >
              清除筛选
            </Button>
          </div>
        ) : screens.md ? (
          <Table<Row>
            rowKey="node_id"
            size="small"
            dataSource={visible}
            pagination={{
              defaultPageSize: 20,
              showSizeChanger: true,
              showTotal: (n) => `共 ${n} 个容器`,
            }}
            rowClassName={(n) =>
              n.node_id === selected ? "status-selected-row" : ""
            }
            columns={[
              {
                title: "容器 / 分组",
                key: "node",
                render: (_, n) => (
                  <>
                    <Typography.Text strong>{n.container_name}</Typography.Text>
                    <div className="status-row-reason">{n.group}</div>
                  </>
                ),
              },
              {
                title: "主机 IP",
                dataIndex: "host_address",
                width: 150,
                render: (value) => <span className="status-mono">{value}</span>,
              },
              {
                title: "状态 / 原因",
                key: "health",
                width: 220,
                render: (_, n) => nodeStatus(n),
              },
              {
                title: "CPU / 核",
                key: "cpu",
                width: 100,
                responsive: ["lg"],
                render: (_, n) =>
                  cpu(validMetrics(n) ? n.resources?.cpu_usage_cores : null),
              },
              {
                title: "内存工作集",
                key: "memory",
                width: 120,
                responsive: ["lg"],
                render: (_, n) =>
                  memory(
                    validMetrics(n)
                      ? n.resources?.memory_working_set_bytes
                      : null,
                  ),
              },
              {
                title: "采样 / 操作",
                key: "action",
                width: 145,
                render: (_, n) => (
                  <>
                    <div className="status-row-reason">
                      {n.collected_at
                        ? new Date(n.collected_at).toLocaleTimeString("zh-CN", {
                            hour12: false,
                          })
                        : "暂无采样"}
                    </div>
                    {action(n)}
                  </>
                ),
              },
            ]}
          />
        ) : (
          <div className="status-mobile-list">
            {visible.map((n) => (
              <article
                key={n.node_id}
                className={
                  n.node_id === selected
                    ? "status-mobile-node status-selected-row"
                    : "status-mobile-node"
                }
              >
                <div className="status-mobile-identity">
                  <div>
                    <Typography.Text strong>{n.container_name}</Typography.Text>
                    <div className="status-row-reason">
                      {n.host_address} · {n.group}
                    </div>
                  </div>
                  <Status value={health(n, stale)} stale={stale} />
                </div>
                <div className="status-row-reason">
                  {stale ? "历史快照 · 当前状态待确认" : n.reason}
                </div>
                <div className="status-mobile-bottom">
                  <span>
                    CPU{" "}
                    {cpu(validMetrics(n) ? n.resources?.cpu_usage_cores : null)}{" "}
                    核 ·{" "}
                    {memory(
                      validMetrics(n)
                        ? n.resources?.memory_working_set_bytes
                        : null,
                    )}
                  </span>
                  {action(n)}
                </div>
              </article>
            ))}
          </div>
        )}
        <div className="status-list-footer">
          <span role="status" aria-live="polite">
            {loadedAt
              ? `显示 ${visible.length} / ${rows.length} 个容器`
              : "等待有效数据"}
          </span>
          <span>容器状态与业务状态分别判断</span>
        </div>
      </Card>
      {current && loadedAt > 0 && (
        <Card
          className="status-detail-card service-panel"
          id="service-node-details"
          ref={detailRef}
          tabIndex={-1}
          title={
            <div className="status-detail-title">
              <span>{current.container_name}</span>
              <Status value={health(current, stale)} stale={stale} />
            </div>
          }
          extra={<Button onClick={close}>收起详情</Button>}
        >
          {(stale || current.status !== "HEALTHY") && (
            <Alert
              showIcon
              type="warning"
              message={
                stale
                  ? "当前状态待确认，以下为最后成功快照"
                  : current.reason || "等待有效证据"
              }
              style={{ marginBottom: 12 }}
            />
          )}
          <Resources
            api={api}
            nodeID={current.node_id}
            value={
              stale && current.resources
                ? { ...current.resources, stale: true }
                : current.resources
            }
            lifecycle={
              stale && current.lifecycle
                ? { ...current.lifecycle, stale: true }
                : current.lifecycle
            }
            runtime={current.runtime_status}
            updatedAt={current.collected_at}
            dataStale={stale || current.stale}
            startup={<StartupParameters data={current.startup_record} />}
            detailsOpen
          />
          {canRestart && (
            <div className="status-controlled-actions">
              <Typography.Text type="secondary">
                受控操作需要独立重启 Token，并核对目标与权限。
              </Typography.Text>
              <Button
                danger
                disabled={stale || !current.restart_enabled}
                title={
                  !current.restart_enabled ? "当前基准未开放重启" : undefined
                }
                onClick={() => onRestart(current)}
              >
                重启容器
              </Button>
            </div>
          )}
        </Card>
      )}
    </section>
  );
}
