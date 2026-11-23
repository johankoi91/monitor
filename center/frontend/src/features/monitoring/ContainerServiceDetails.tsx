import {
  CloseOutlined,
  CloudServerOutlined,
  ContainerOutlined,
  LeftOutlined,
  ReloadOutlined,
  RightOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Drawer from "antd/es/drawer";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { useEffect, useRef } from "react";
import type { API } from "../../shared/api";
import type { NodeStatus } from "../../shared/types";
import "./ContainerServices.css";
import Resources from "./Resources";
import ContainerStartupParameters from "./ContainerStartupParameters";

export type ContainerServiceRow = NodeStatus & { groupLabel: string };

export default function ContainerServiceDetails({
  api,
  node,
  rows,
  onSelect,
  onClose,
  onSettings,
  onRestart,
  actionOpen,
}: {
  api: API;
  node?: ContainerServiceRow;
  rows: ContainerServiceRow[];
  onSelect: (id: string) => void;
  onClose: () => void;
  onSettings?: () => void;
  onRestart?: () => void;
  actionOpen: boolean;
}) {
  const scroll = useRef<HTMLDivElement>(null);
  const actionTrigger = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!actionOpen && node && actionTrigger.current?.isConnected) {
      actionTrigger.current.focus({ preventScroll: true });
      actionTrigger.current = null;
    }
  }, [actionOpen, node?.node_id]);
  const index = node
    ? rows.findIndex((row) => row.node_id === node.node_id)
    : -1;
  const status = node?.stale ? "UNKNOWN" : node?.status;
  const statuses: Record<string, { label: string; color: string }> = {
    HEALTHY: { label: "正常", color: "success" },
    UNHEALTHY: { label: "异常", color: "error" },
    DEGRADED: { label: "部分异常", color: "warning" },
    UNKNOWN: {
      label: node?.stale ? "数据过期 · 待确认" : "待确认",
      color: "default",
    },
  };
  const state = statuses[status || "UNKNOWN"] || {
    label: status,
    color: "default",
  };
  function select(offset: number) {
    const next = rows[index + offset];
    if (!next) return;
    onSelect(next.node_id);
    scroll.current?.scrollTo({ top: 0 });
  }
  return (
    <Drawer
      open={!!node}
      onClose={onClose}
      keyboard={!actionOpen}
      width="min(1040px, 100vw)"
      rootClassName="container-service-drawer"
      closable={false}
      destroyOnHidden
      title={
        <span className="service-panel-caption">
          <ContainerOutlined aria-hidden />
          容器详情
        </span>
      }
      extra={
        <div className="service-panel-navigation">
          {index >= 0 && (
            <span className="service-position">
              {index + 1} / {rows.length}
            </span>
          )}
          <Button
            aria-label="上一个容器"
            title="上一个容器"
            icon={<LeftOutlined />}
            disabled={index <= 0}
            onClick={() => select(-1)}
          />
          <Button
            aria-label="下一个容器"
            title="下一个容器"
            icon={<RightOutlined />}
            disabled={index < 0 || index >= rows.length - 1}
            onClick={() => select(1)}
          />
          <span className="service-panel-divider" />
          <Button
            aria-label="关闭容器详情"
            title="关闭 · Esc"
            type="text"
            icon={<CloseOutlined />}
            onClick={onClose}
          />
        </div>
      }
      footer={
        node && (
          <div className="service-panel-footer">
            <Button onClick={onClose}>返回容器列表</Button>
            <div className="service-panel-operations">
              {onSettings && (
                <Button
                  icon={<SettingOutlined aria-hidden />}
                  onClick={(event) => {
                    actionTrigger.current = event.currentTarget;
                    onSettings();
                  }}
                >
                  监控设置
                </Button>
              )}
              {onRestart && (
                <Button
                  danger
                  icon={<ReloadOutlined aria-hidden />}
                  disabled={!node.restart_enabled}
                  title={
                    !node.restart_enabled
                      ? node.restart_block_reason || "当前不可重启"
                      : undefined
                  }
                  onClick={(event) => {
                    actionTrigger.current = event.currentTarget;
                    onRestart();
                  }}
                >
                  重启容器
                </Button>
              )}
            </div>
          </div>
        )
      }
    >
      {node && (
        <div className="service-panel-scroll" ref={scroll}>
          <div className="service-identity">
            <div className="service-identity-title">
              <Typography.Title level={3}>
                {node.container_name}
              </Typography.Title>
              <Tag color={state.color}>{state.label}</Tag>
            </div>
            <div className="service-identity-meta">
              <span>
                <CloudServerOutlined aria-hidden />
                <Typography.Text copyable>{node.host_address}</Typography.Text>
              </span>
              <span className="service-meta-separator" />
              <span>{node.groupLabel}</span>
            </div>
            <div className="service-image">
              <span>镜像</span>
              <Typography.Text copyable>{node.image || "—"}</Typography.Text>
            </div>
          </div>
          {status !== "HEALTHY" && (
            <Alert
              className="service-health-alert"
              showIcon
              type={status === "UNHEALTHY" ? "error" : "warning"}
              message={
                node.stale
                  ? "采样已过期，当前状态待确认"
                  : node.reason || "等待有效检查结果"
              }
            />
          )}
          <Resources
            api={api}
            nodeID={node.node_id}
            value={node.resources}
            lifecycle={node.lifecycle}
            runtime={node.runtime_status}
            updatedAt={node.collected_at}
            dataStale={node.stale}
            startup={
              <ContainerStartupParameters
                api={api}
                nodeID={node.node_id}
                reference={node.startup_ref}
              />
            }
            detailsOpen
          />
        </div>
      )}
    </Drawer>
  );
}
