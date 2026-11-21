import {
  AppstoreOutlined,
  BellOutlined,
  CheckCircleOutlined,
  CloudServerOutlined,
  ExclamationCircleOutlined,
  InfoCircleOutlined,
  SendOutlined,
} from "@ant-design/icons";
import Table from "antd/es/table";
import type { ReactNode } from "react";
import type { WorkspaceRoute } from "../../app/navigation";
import type { API } from "../../shared/api";
import type { Notifications, ServiceStatus } from "../../shared/types";
import RTCOverview from "../rtc/RTCOverview";
import { OverviewLink, OverviewState, type OverviewTone } from "./OverviewUI";

function counts(nodes: ServiceStatus["nodes"]) {
  const healthy = nodes.filter(
    (n) => n.status === "HEALTHY" && !n.stale,
  ).length;
  const unknown = nodes.filter((n) => n.status === "UNKNOWN" || n.stale).length;
  return { healthy, unknown, unhealthy: nodes.length - healthy - unknown };
}
function Metric({
  label,
  value,
  note,
  icon,
  tone = "neutral",
}: {
  label: string;
  value: ReactNode;
  note?: string;
  icon: ReactNode;
  tone?: OverviewTone;
}) {
  return (
    <div className={`overview-metric is-${tone}`}>
      <div className="overview-metric-heading">
        <span>{label}</span>
        <span className="overview-metric-icon" aria-hidden="true">
          {icon}
        </span>
      </div>
      <div className="overview-metric-value">{value}</div>
      {note && <p>{note}</p>}
    </div>
  );
}
export default function Overview({
  api,
  services,
  notifications,
  onNavigate,
  canManageInventory = false,
  canConfigureNotifications = false,
  refreshVersion = 0,
  snapshotUnavailable = false,
  hasSnapshot = true,
}: {
  api: API;
  services: ServiceStatus[];
  notifications: Notifications;
  onNavigate: (route: WorkspaceRoute) => void;
  canManageInventory?: boolean;
  canConfigureNotifications?: boolean;
  refreshVersion?: number;
  snapshotUnavailable?: boolean;
  hasSnapshot?: boolean;
}) {
  const nodes = services.flatMap((s) => s.nodes),
    state = counts(nodes);
  const groups = new Set(services.map((s) => s.cluster_code)).size;
  const hosts = new Set(
    nodes.map((n) => n.host_address || n.host_name).filter(Boolean),
  ).size;
  const attention = state.unhealthy + state.unknown;
  const notificationTone =
    snapshotUnavailable || notifications.storage_available === false
      ? "warning"
      : notifications.enabled
        ? "healthy"
        : "neutral";
  const notificationLabel = snapshotUnavailable
    ? "读取待确认"
    : notifications.storage_available === false
      ? "存储不可用"
      : notifications.enabled
        ? "已启用"
        : "未启用";
  return (
    <section className="overview-suite" aria-label="运行总览">
      <div className="overview-metrics" aria-label="关键运行指标">
        <Metric
          label="监控容器"
          value={hasSnapshot ? nodes.length : "—"}
          icon={<CloudServerOutlined />}
          note={
            !hasSnapshot
              ? "数据暂不可读"
              : nodes.length
                ? `${groups} 个分组 · ${hosts} 台主机`
                : undefined
          }
        />
        <Metric
          label="容器检查正常"
          icon={<CheckCircleOutlined />}
          value={
            snapshotUnavailable || !nodes.length ? (
              "—"
            ) : (
              <>
                {state.healthy}
                <small>/ {nodes.length}</small>
              </>
            )
          }
          tone={
            !snapshotUnavailable && state.healthy > 0 ? "healthy" : "neutral"
          }
          note={snapshotUnavailable ? "当前数据不可确认" : undefined}
        />
        <Metric
          label="待关注容器"
          value={snapshotUnavailable ? "—" : attention}
          icon={<ExclamationCircleOutlined />}
          tone={
            snapshotUnavailable
              ? "warning"
              : state.unhealthy
                ? "danger"
                : state.unknown
                  ? "warning"
                  : "neutral"
          }
          note={
            snapshotUnavailable
              ? "当前数据不可确认"
              : nodes.length
                ? `${state.unhealthy} 异常 · ${state.unknown} 待确认`
                : undefined
          }
        />
        <Metric
          label="通知队列"
          icon={<SendOutlined />}
          value={snapshotUnavailable ? "—" : (notifications.pending ?? "—")}
          note={
            snapshotUnavailable
              ? "投递数据读取失败"
              : !notifications.enabled
                ? "通知通道未启用"
                : undefined
          }
        />
      </div>
      <div className="overview-columns">
        <RTCOverview
          api={api}
          onNavigate={onNavigate}
          refreshVersion={refreshVersion}
        />
        <section
          className="overview-panel overview-notifications"
          aria-labelledby="overview-notification-title"
        >
          <div className="overview-panel-heading">
            <div>
              <span className="overview-section-icon" aria-hidden="true">
                <BellOutlined />
              </span>
              <h3 id="overview-notification-title">通知投递</h3>
            </div>
            <OverviewState tone={notificationTone}>
              {notificationLabel}
            </OverviewState>
          </div>
          <div className="overview-notification-summary">
            <div className="overview-notification-numbers">
              <div>
                <span>累计送达</span>
                <strong>
                  {snapshotUnavailable ? "—" : (notifications.delivered ?? "—")}
                </strong>
              </div>
              <div>
                <span>累计失败</span>
                <strong>
                  {snapshotUnavailable ? "—" : (notifications.failed ?? "—")}
                </strong>
              </div>
              <div>
                <span>累计丢弃</span>
                <strong>
                  {snapshotUnavailable ? "—" : (notifications.dropped ?? "—")}
                </strong>
              </div>
            </div>
            {(snapshotUnavailable ||
              notifications.storage_available === false) && (
              <div className="overview-card-alert" role="status">
                <InfoCircleOutlined aria-hidden="true" />
                <span>
                  {snapshotUnavailable
                    ? "暂时无法读取投递状态"
                    : "投递存储暂不可用"}
                </span>
              </div>
            )}
          </div>
          <div className="overview-card-actions" aria-label="通知投递操作">
            <OverviewLink
              variant="primary"
              route={{ view: "notifications", page: "delivery" }}
              onNavigate={onNavigate}
            >
              投递记录
            </OverviewLink>
            {canConfigureNotifications && (
              <OverviewLink
                variant="secondary"
                route={{ view: "notifications", page: "config" }}
                onNavigate={onNavigate}
              >
                接收配置
              </OverviewLink>
            )}
          </div>
        </section>
      </div>
      <section
        className="overview-panel overview-inventory"
        aria-labelledby="overview-inventory-title"
      >
        <div className="overview-panel-heading">
          <div>
            <span className="overview-section-icon" aria-hidden="true">
              <AppstoreOutlined />
            </span>
            <h3 id="overview-inventory-title">容器监控</h3>
            <span className="overview-section-count">
              {hasSnapshot
                ? `${services.length} 个服务 · ${hosts} 台主机`
                : "等待读取"}
            </span>
          </div>
          <OverviewLink
            route={{ view: "status", page: "" }}
            onNavigate={onNavigate}
          >
            查看容器
          </OverviewLink>
        </div>
        {services.length ? (
          <Table<ServiceStatus>
            size="small"
            rowKey={(s) => `${s.cluster_code}/${s.service_code}`}
            dataSource={services}
            pagination={
              services.length > 8
                ? { pageSize: 8, size: "small", showSizeChanger: false }
                : false
            }
            scroll={{ x: 600 }}
            columns={[
              {
                title: "服务 / 分组",
                render: (_, s) => (
                  <div className="overview-service">
                    <strong>{s.service_name}</strong>
                    <span>{s.group_name || s.cluster_code}</span>
                  </div>
                ),
              },
              {
                title: "容器数量",
                dataIndex: "expected_node_count",
                align: "right",
                width: 110,
                render: (value: number) => (
                  <span className="overview-table-number">{value}</span>
                ),
              },
              {
                title: "正常",
                align: "right",
                width: 100,
                render: (_, s) => (
                  <span className="overview-table-number is-healthy">
                    {snapshotUnavailable ? "—" : counts(s.nodes).healthy}
                  </span>
                ),
              },
              {
                title: "异常",
                align: "right",
                width: 100,
                render: (_, s) => (
                  <span className="overview-table-number is-danger">
                    {snapshotUnavailable ? "—" : counts(s.nodes).unhealthy}
                  </span>
                ),
              },
              {
                title: "待确认",
                align: "right",
                width: 100,
                render: (_, s) => (
                  <span className="overview-table-number is-warning">
                    {snapshotUnavailable ? "—" : counts(s.nodes).unknown}
                  </span>
                ),
              },
            ]}
          />
        ) : !hasSnapshot ? (
          <div className="overview-empty-inventory">
            <span className="overview-empty-icon" aria-hidden="true">
              <InfoCircleOutlined />
            </span>
            <h4>容器监控数据暂不可读</h4>
          </div>
        ) : (
          <div className="overview-empty-inventory">
            <span className="overview-empty-icon" aria-hidden="true">
              <CloudServerOutlined />
            </span>
            <h4>暂无监控容器</h4>
            {canManageInventory && (
              <OverviewLink
                variant="primary"
                route={{ view: "inventory", page: "discovery" }}
                onNavigate={onNavigate}
              >
                添加容器
              </OverviewLink>
            )}
          </div>
        )}
      </section>
    </section>
  );
}
