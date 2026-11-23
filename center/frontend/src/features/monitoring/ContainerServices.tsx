import {
  ContainerOutlined,
  DownloadOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Space from "antd/es/space";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { useEffect, useState } from "react";
import { type API } from "../../shared/api";
import type {
  Baseline,
  Node,
  NodeStatus,
  ServiceStatus,
} from "../../shared/types";
import ContainerServiceDetails from "./ContainerServiceDetails";

import { formatTimestamp as stamp } from "../../shared/format";
import Empty from "../../shared/ui/EmptyState";
import Badge from "../../shared/ui/StatusBadge";

export function useContainerFilters() {
  const [statusGroup, setStatusGroup] = useState("");
  const [statusHost, setStatusHost] = useState("");
  const [serviceSearch, setServiceSearch] = useState("");
  return {
    statusGroup,
    setStatusGroup,
    statusHost,
    setStatusHost,
    serviceSearch,
    setServiceSearch,
  };
}

export default function ContainerServices({
  api,
  services,
  baseline,
  members,
  removals,
  filters,
  canDiscover,
  canConfigure,
  canRestart,
  settingsBlocked,
  actionOpen,
  yamlDownloading,
  downloadBaseline,
  onDiscover,
  onSettings,
  onRestart,
}: {
  filters: ReturnType<typeof useContainerFilters>;
  api: API;
  services: ServiceStatus[];
  baseline: Baseline | null;
  members: Node[];
  removals: ReadonlySet<string>;
  canDiscover: boolean;
  canConfigure: boolean;
  canRestart: boolean;
  settingsBlocked: boolean;
  actionOpen: boolean;
  yamlDownloading: boolean;
  downloadBaseline: () => Promise<void>;
  onDiscover: () => void;
  onSettings: (node: Node) => void;
  onRestart: (node: NodeStatus) => void;
}) {
  const {
    statusGroup,
    setStatusGroup,
    statusHost,
    setStatusHost,
    serviceSearch,
    setServiceSearch,
  } = filters;
  const [selectedNodeID, setSelectedNodeID] = useState(() =>
    new URLSearchParams(window.location.search).get("node"),
  );
  useEffect(() => {
    const restore = () =>
      setSelectedNodeID(
        new URLSearchParams(window.location.search).get("node"),
      );
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);
  function openNode(id: string | null) {
    const url = new URL(window.location.href);
    if (id) url.searchParams.set("node", id);
    else url.searchParams.delete("node");
    window.history.pushState(
      null,
      "",
      `${url.pathname}${url.search}${url.hash}`,
    );
    setSelectedNodeID(id);
  }
  const statusGroups = [
    ...services
      .reduce((groups, service) => {
        const group = groups.get(service.cluster_code);
        if (group) group.nodes.push(...service.nodes);
        else
          groups.set(service.cluster_code, {
            ...service,
            nodes: [...service.nodes],
          });
        return groups;
      }, new Map<string, ServiceStatus>())
      .values(),
  ].filter((group) => group.nodes.length > 0);
  const statusHosts = [
    ...new Set(services.flatMap((s) => s.nodes.map((n) => n.host_address))),
  ].sort();
  const visibleStatus = statusGroups
    .filter((s) => !statusGroup || s.cluster_code === statusGroup)
    .map((s) => ({
      ...s,
      nodes: s.nodes.filter(
        (n) => !statusHost || n.host_address === statusHost,
      ),
    }))
    .filter((s) => s.nodes.length > 0);
  const statusRows = visibleStatus
    .flatMap((s) =>
      s.nodes.map((n) => ({
        ...n,
        groupLabel: s.group_name || s.cluster_code,
      })),
    )
    .filter((n) =>
      `${n.container_name} ${n.host_address} ${n.groupLabel}`
        .toLowerCase()
        .includes(serviceSearch.toLowerCase()),
    );
  const selectedStatusNode = services
    .flatMap((s) =>
      s.nodes.map((n) => ({
        ...n,
        groupLabel: s.group_name || s.cluster_code,
      })),
    )
    .find((n) => selectedNodeID === n.node_id);
  const selectedStatusMember = members.find(
    (n) => n.id === selectedStatusNode?.node_id,
  );

  return (
    <section className="container-services-screen">
      <div className="section-title monitor-service-actions">
        <Typography.Text type="secondary">
          {services.flatMap((service) => service.nodes).length} 个监控容器
        </Typography.Text>
        <Space wrap>
          {baseline?.yaml_url && (
            <Button
              icon={<DownloadOutlined aria-hidden />}
              loading={yamlDownloading}
              onClick={() => void downloadBaseline()}
            >
              下载配置
            </Button>
          )}
          {canDiscover && (
            <Button type="primary" onClick={() => onDiscover()}>
              部署服务容器发现
            </Button>
          )}
        </Space>
      </div>
      <Form layout="vertical" className="filters status-filters">
        <Form.Item label="搜索容器、主机或分组" htmlFor="service-search">
          <Input
            id="service-search"
            placeholder="搜索容器名称、IP 或分组"
            prefix={<SearchOutlined aria-hidden />}
            allowClear
            value={serviceSearch}
            onChange={(e) => setServiceSearch(e.target.value)}
          />
        </Form.Item>

        <Form.Item label="主机 IP">
          <Select
            style={{ minWidth: 200 }}
            value={statusHost}
            onChange={setStatusHost}
            options={[
              { value: "", label: "全部主机" },
              ...statusHosts.map((value) => ({
                value,
                label: value,
              })),
            ]}
          />
        </Form.Item>
        <Form.Item label="分组名" htmlFor="app-field-status-group">
          <Select
            id="app-field-status-group"
            style={{ minWidth: 240, width: "100%" }}
            value={statusGroup}
            onChange={setStatusGroup}
          >
            <Select.Option value="">全部分组</Select.Option>
            {statusGroups.map((group) => (
              <Select.Option
                key={group.cluster_code}
                value={group.cluster_code}
              >
                {group.group_name || group.cluster_code}
              </Select.Option>
            ))}
          </Select>
        </Form.Item>
      </Form>
      {!services.length && (
        <Empty>暂无监控容器，请从“部署服务容器发现”中添加。</Empty>
      )}
      {services.length > 0 && !visibleStatus.length && (
        <Empty>没有匹配的分组状态。</Empty>
      )}
      <Card className="page-card service-list-card">
        <Table<NodeStatus & { groupLabel: string }>
          size="small"
          rowKey="node_id"
          scroll={{ x: 760 }}
          pagination={{
            defaultPageSize: 20,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 个容器`,
          }}
          dataSource={statusRows}
          rowClassName={(n) =>
            selectedNodeID === n.node_id ? "service-row-selected" : ""
          }
          columns={[
            {
              title: "容器 / 分组",
              render: (_, n) => (
                <div className="service-list-identity">
                  <span className="service-list-symbol">
                    <ContainerOutlined aria-hidden />
                  </span>
                  <div>
                    <span className="workspace-service-name">
                      {n.container_name}
                    </span>
                    <span className="workspace-service-note">
                      {n.groupLabel}
                    </span>
                  </div>
                </div>
              ),
            },
            {
              title: "主机 IP",
              dataIndex: "host_address",
              width: 160,
            },
            {
              title: "检查状态",
              width: 180,
              render: (_, n) => (
                <>
                  <Badge
                    value={n.stale ? "UNKNOWN" : n.status}
                    label={
                      n.stale
                        ? "数据过期 · 待确认"
                        : {
                            HEALTHY: "正常",
                            UNHEALTHY: "异常",
                            DEGRADED: "部分异常",
                            UNKNOWN: "待确认",
                          }[n.status] || n.status
                    }
                  />
                  {(n.stale || n.status !== "HEALTHY") && (
                    <span className="workspace-service-note">
                      {n.stale ? "等待有效采样" : n.reason}
                    </span>
                  )}
                </>
              ),
            },
            {
              title: "资源 / 采样",
              width: 180,
              render: (_, n) => (
                <>
                  <span>
                    {n.resources &&
                    !n.resources.stale &&
                    n.resources.status !== "UNAVAILABLE"
                      ? `${n.resources.cpu_usage_cores == null ? "—" : n.resources.cpu_usage_cores.toFixed(4)} 核 · ${n.resources.memory_working_set_bytes == null ? "—" : (n.resources.memory_working_set_bytes / 1048576).toFixed(1)} MiB`
                      : "等待有效资源采样"}
                  </span>
                  <span className="workspace-service-note">
                    {stamp(n.collected_at)}
                  </span>
                </>
              ),
            },
            {
              title: "操作",
              width: 160,
              render: (_, n) => {
                const member = members.find(
                  (member) => member.id === n.node_id,
                );
                return (
                  <Space size={4} wrap>
                    <Button type="link" onClick={() => openNode(n.node_id)}>
                      查看详情
                    </Button>
                    {member && (
                      <Button
                        type="link"
                        disabled={
                          !canConfigure ||
                          settingsBlocked ||
                          !!n.active_operation_id
                        }
                        onClick={() => onSettings(member)}
                      >
                        设置
                      </Button>
                    )}
                    {removals.has(n.node_id) && (
                      <Tag color="warning">待保存移出</Tag>
                    )}
                  </Space>
                );
              },
            },
          ]}
        />
      </Card>
      <ContainerServiceDetails
        api={api}
        actionOpen={actionOpen}
        node={selectedStatusNode}
        rows={statusRows}
        onSelect={openNode}
        onClose={() => openNode(null)}
        onSettings={
          selectedStatusMember &&
          canConfigure &&
          !settingsBlocked &&
          !selectedStatusNode?.active_operation_id
            ? () => onSettings(selectedStatusMember)
            : undefined
        }
        onRestart={
          selectedStatusNode && canRestart
            ? () => onRestart(selectedStatusNode)
            : undefined
        }
      />
    </section>
  );
}
