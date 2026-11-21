import { ContainerOutlined, InfoCircleOutlined } from "@ant-design/icons";
import Descriptions from "antd/es/descriptions";
import Empty from "antd/es/empty";
import Tag from "antd/es/tag";
import type { ReactNode } from "react";
import type { StartupRecord } from "../../shared/types";
import StartupParameters from "../monitoring/StartupParameters";

export function InventoryStep({
  number,
  title,
  extra,
}: {
  number: string;
  title: string;
  extra?: ReactNode;
}) {
  return (
    <div className="inventory-step">
      <div>
        <span className="inventory-step-number" aria-hidden>
          {number}
        </span>
        <h3>{title}</h3>
      </div>
      {extra}
    </div>
  );
}
export function InventoryIdentity({
  name,
  host,
  image,
}: {
  name: string;
  host?: string;
  image?: string;
}) {
  return (
    <div className="inventory-container-identity">
      <span className="inventory-container-icon" aria-hidden>
        <ContainerOutlined />
      </span>
      <div>
        <strong>{name}</strong>
        {host && <span className="inventory-identity-host">{host}</span>}
        {image && (
          <span className="inventory-identity-image" title={image}>
            {image}
          </span>
        )}
      </div>
    </div>
  );
}
export function InventoryNote({ children }: { children: ReactNode }) {
  return (
    <div className="inventory-note">
      <InfoCircleOutlined aria-hidden />
      <p>{children}</p>
    </div>
  );
}
export function InventoryEmpty({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="inventory-empty">
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={
          <>
            <strong>{title}</strong>
            <span>{description}</span>
          </>
        }
      />
    </div>
  );
}
const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
const text = (value: unknown) =>
  value == null || value === ""
    ? "—"
    : typeof value === "object"
      ? JSON.stringify(value)
      : String(value);
const date = (value: unknown) =>
  typeof value === "string" && value && Number.isFinite(Date.parse(value))
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
const runtimeLabels: Record<string, string> = {
  running: "运行中",
  created: "已创建",
  exited: "已停止",
  paused: "已暂停",
  restarting: "重启中",
  removing: "未运行",
  dead: "未运行",
};

/** This view presents the existing detail response; its full payload remains available below. */
export function InventoryContainerDetails({ value }: { value: unknown }) {
  const detail = object(value),
    container = object(detail.container),
    startup = object(detail.startup),
    config = object(startup.config);
  const hasStartup =
    Object.keys(config).length > 0 &&
    [config.env, config.mounts, config.port_bindings].every(
      (value) => value == null || Array.isArray(value),
    );
  const startupRecord = hasStartup
    ? ({
        startup: detail.startup,
        stale: !!container.stale,
        reported_at: startup.collected_at,
      } as StartupRecord)
    : undefined;
  return (
    <div className="inventory-container-details">
      <div className="inventory-detail-identity">
        <InventoryIdentity
          name={text(container.container_name)}
          host={text(container.host_address)}
          image={
            typeof container.image === "string" ? container.image : undefined
          }
        />
        <div className="inventory-detail-tags">
          <Tag
            color={
              container.runtime_status === "running"
                ? "success"
                : container.runtime_status === "created"
                  ? "processing"
                  : undefined
            }
          >
            {runtimeLabels[text(container.runtime_status)] ||
              text(container.runtime_status)}
          </Tag>
          {container.stale === true && <Tag color="warning">离线 / 过期</Tag>}
          <Tag color={container.managed ? "blue" : undefined}>
            {container.managed === true
              ? "已纳管"
              : container.managed === false
                ? "未纳管"
                : "纳管状态未记录"}
          </Tag>
        </div>
      </div>
      <InventoryNote>
        这里展示当前读取到的容器与启动快照，用于核对配置；纳管和检查设置在台账中处理。
      </InventoryNote>
      <section className="inventory-detail-section">
        <h3>容器信息</h3>
        <Descriptions
          size="small"
          column={{ xs: 1, sm: 2 }}
          items={[
            {
              key: "agent",
              label: "所属 Agent",
              children: <code>{text(container.agent_id)}</code>,
            },
            {
              key: "collected",
              label: "采集时间",
              children: date(container.collected_at),
            },
            {
              key: "container",
              label: "容器 ID",
              children: <code>{text(container.container_id)}</code>,
              span: "filled",
            },
            {
              key: "snapshot",
              label: "快照 ID",
              children: <code>{text(container.snapshot_id)}</code>,
              span: "filled",
            },
            {
              key: "start",
              label: "本次启动",
              children: date(detail.started_at),
            },
            {
              key: "restart",
              label: "自动重启次数",
              children: text(detail.restart_count),
            },
            {
              key: "exit",
              label: "退出码",
              children:
                container.runtime_status === "running"
                  ? "运行中"
                  : text(detail.exit_code),
            },
            {
              key: "health",
              label: "Docker Healthcheck",
              children: detail.docker_health
                ? text(detail.docker_health)
                : "未配置",
            },
          ]}
        />
      </section>
      <section className="inventory-detail-section">
        <h3>启动配置</h3>
        {startupRecord ? (
          <StartupParameters data={startupRecord} />
        ) : (
          <InventoryEmpty
            title="暂无有效启动快照"
            description="等待 Agent 上报后可查看启动参数。完整响应仍可在原始数据中核对。"
          />
        )}
      </section>
      <details className="inventory-raw-detail">
        <summary>查看原始数据</summary>
        <pre>{JSON.stringify(value, null, 2)}</pre>
      </details>
    </div>
  );
}
