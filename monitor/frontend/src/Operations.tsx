import { useEffect, useMemo, useState } from "react";
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Descriptions from "antd/es/descriptions";
import Empty from "antd/es/empty";
import Input from "antd/es/input";
import Modal from "antd/es/modal";
import Select from "antd/es/select";
import Space from "antd/es/space";
import Statistic from "antd/es/statistic";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Timeline from "antd/es/timeline";
import Typography from "antd/es/typography";
import type { API } from "./api";
import type { Operation, ServiceStatus } from "./types";

const labels: Record<string, string> = {
  ACCEPTED: "已受理",
  EXECUTE_INTENT: "执行前核验通过",
  WAIT_RESULT: "正在重启",
  VERIFY: "正在复查",
  UNCERTAIN: "结果未知",
  SUCCESS: "成功",
  FAILED: "失败",
  REJECTED: "已拒绝",
  TIMEOUT: "超时",
  MANUALLY_CLOSED: "已现场核实",
};
const stages: Record<string, string> = {
  ACCEPTED: "受理请求",
  PREPARED: "执行前核验",
  EXECUTING: "执行重启",
  EXECUTED: "复查容器",
  VERIFY_SUCCESS: "复查通过",
  MANUAL_RESOLUTION: "现场核实",
};
const time = (v: string | null) =>
  v ? new Date(v).toLocaleString("zh-CN", { hour12: false }) : "—";
const healthLabels: Record<string, string> = {
  HEALTHY: "正常",
  UNHEALTHY: "异常",
  UNKNOWN: "未知",
  DEGRADED: "部分异常",
};
const stepDescriptions: Record<string, string> = {
  ACCEPTED: "重启请求已受理，开始检查操作条件。",
  PREPARED: "已核验授权规则与原容器身份。",
  EXECUTING: "正在重启原容器。",
  EXECUTED: "重启请求已返回，开始检查容器状态。",
  VERIFY_SUCCESS: "容器在已配置的检查层级复查通过。",
};
const duration = (o: Operation) =>
  o.finished_at
    ? `${Math.max(0, (Date.parse(o.finished_at) - Date.parse(o.requested_at)) / 1000).toFixed(1)} 秒`
    : o.node_locked
      ? "进行中"
      : "—";
const failed = (o: Operation) =>
  ["FAILED", "REJECTED", "TIMEOUT"].includes(o.status);
function Status({ operation: o }: { operation: Operation }) {
  return (
    <Tag
      icon={
        o.status === "SUCCESS" ? (
          <CheckCircleOutlined />
        ) : o.status === "UNCERTAIN" || failed(o) ? (
          <ExclamationCircleOutlined />
        ) : (
          <ClockCircleOutlined />
        )
      }
      color={
        o.status === "SUCCESS"
          ? "success"
          : o.status === "UNCERTAIN"
            ? "warning"
            : failed(o)
              ? "error"
              : o.node_locked
                ? "processing"
                : "default"
      }
    >
      {labels[o.status] || o.status}
    </Tag>
  );
}
function target(o: Operation, services: ServiceStatus[]) {
  const node = services
    .flatMap((s) => s.nodes)
    .find((n) => n.node_id === o.node_id);
  return {
    name: o.target?.container_name || node?.container_name || o.node_id,
    ip: node?.host_address || "历史台账未找到",
  };
}

export default function Operations({
  operations,
  services,
  canAudit,
  canResolve,
  onOpen,
  onResolve,
  onRefresh,
}: {
  operations: Operation[];
  services: ServiceStatus[];
  canAudit: boolean;
  canResolve: boolean;
  onOpen: (id: string) => void;
  onResolve: (o: Operation) => void;
  onRefresh: () => void;
}) {
  const [search, setSearch] = useState(""),
    [filter, setFilter] = useState("all");
  const groups = {
    success: operations.filter((o) => o.status === "SUCCESS").length,
    active: operations.filter((o) => o.node_locked && o.status !== "UNCERTAIN")
      .length,
    attention: operations.filter((o) => o.status === "UNCERTAIN" || failed(o))
      .length,
  };
  const rows = useMemo(
    () =>
      operations.filter((o) => {
        const t = target(o, services);
        const match = [
          t.name,
          t.ip,
          o.operation_id,
          o.reason,
          o.authenticated_source,
        ]
          .join(" ")
          .toLowerCase()
          .includes(search.trim().toLowerCase());
        return (
          match &&
          (filter === "all" ||
            (filter === "success" && o.status === "SUCCESS") ||
            (filter === "active" &&
              o.node_locked &&
              o.status !== "UNCERTAIN") ||
            (filter === "attention" && (o.status === "UNCERTAIN" || failed(o))))
        );
      }),
    [operations, services, search, filter],
  );
  return (
    <section className="operations-page">
      <div className="operations-heading">
        <div>
          <Typography.Title level={2}>
            {canAudit ? "操作与审计" : "我的重启"}
          </Typography.Title>
          <Typography.Text type="secondary">
            {canAudit
              ? "追踪容器重启结果与执行过程"
              : "查看自己发起的重启及最新结果"}{" "}
            · 当前最近 {operations.length} 条记录
          </Typography.Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={onRefresh}>
          刷新记录
        </Button>
      </div>
      <div className="operations-summary">
        {[
          { label: "操作记录", count: operations.length, color: "#253858" },
          { label: "已成功", count: groups.success, color: "#16976a" },
          { label: "进行中", count: groups.active, color: "#1677ff" },
          { label: "需要关注", count: groups.attention, color: "#cf7c14" },
        ].map((item) => (
          <Card size="small" key={item.label}>
            <Statistic
              title={item.label}
              value={item.count}
              valueStyle={{ color: item.color }}
            />
          </Card>
        ))}
      </div>
      {operations.some((o) => o.status === "UNCERTAIN" && o.node_locked) && (
        <Alert
          className="page-alert"
          showIcon
          type="warning"
          message="存在结果未知的操作"
          description="相关节点保持锁定，需现场核实执行情况。请联系 Agora 人员处理。"
        />
      )}
      <Card className="page-card operation-table-card">
        <div className="operations-filters">
          <Input
            allowClear
            prefix={<SearchOutlined />}
            placeholder="搜索容器、主机 IP、操作 ID 或原因"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Select
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "全部结果" },
              { value: "success", label: "已成功" },
              { value: "active", label: "进行中" },
              { value: "attention", label: "需要关注" },
            ]}
          />
          <Typography.Text type="secondary">{rows.length} 条</Typography.Text>
        </div>
        <Table
          rowKey="operation_id"
          dataSource={rows}
          scroll={{ x: 1000 }}
          pagination={{
            pageSize: 10,
            showSizeChanger: false,
            showTotal: (n) => `共 ${n} 条`,
          }}
          locale={{
            emptyText: (
              <Empty
                description={
                  operations.length ? "没有匹配的操作" : "暂无重启操作"
                }
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ),
          }}
          columns={[
            {
              title: "操作对象",
              key: "target",
              width: 250,
              render: (_, o: Operation) => {
                const t = target(o, services);
                return (
                  <div className="operation-target">
                    <Typography.Text strong>{t.name}</Typography.Text>
                    <Typography.Text type="secondary">{t.ip}</Typography.Text>
                  </div>
                );
              },
            },
            {
              title: "结果",
              key: "status",
              width: 150,
              render: (_, o: Operation) => <Status operation={o} />,
            },
            { title: "操作原因", dataIndex: "reason", ellipsis: true },
            {
              title: "发起时间",
              dataIndex: "requested_at",
              width: 190,
              render: time,
              sorter: (a, b) =>
                Date.parse(a.requested_at) - Date.parse(b.requested_at),
              defaultSortOrder: "descend",
            },
            {
              title: "耗时",
              key: "duration",
              width: 100,
              render: (_, o: Operation) => duration(o),
            },
            {
              title: "操作",
              key: "actions",
              width: 160,
              fixed: "right",
              render: (_, o: Operation) => (
                <Space direction="vertical" size={0}>
                  <Button type="link" onClick={() => onOpen(o.operation_id)}>
                    {canAudit ? "详情与审计" : "查看结果"}
                  </Button>
                  {o.status === "UNCERTAIN" && o.node_locked && canResolve && (
                    <Button type="link" onClick={() => onResolve(o)}>
                      现场核实
                    </Button>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Typography.Paragraph className="operations-footnote" type="secondary">
        操作成功表示原容器在当前检查层级复查通过。业务恢复仍需结合服务状态确认。
      </Typography.Paragraph>
    </section>
  );
}

type Step = { at: string; kind: string; status: string; message: string };
export function OperationDetails({
  operation: o,
  services,
  api,
  canAudit,
  onClose,
}: {
  operation: Operation;
  services: ServiceStatus[];
  api: API;
  canAudit: boolean;
  onClose: () => void;
}) {
  const [steps, setSteps] = useState<Step[]>([]),
    [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    if (!canAudit) return;
    let active = true;
    setSteps([]);
    setError("");
    setLoading(true);
    api
      .get<{ records: Step[] }>(`/api/v1/operations/${o.operation_id}/audit`)
      .then((r) => {
        if (active) setSteps(r.records);
      })
      .catch((e) => {
        if (active)
          setError(e instanceof Error ? e.message : "过程记录读取失败");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, canAudit, o.operation_id, o.status]);
  const t = target(o, services);
  return (
    <Modal
      open
      title="重启操作详情"
      width={840}
      className="operation-detail-modal"
      footer={<Button onClick={onClose}>关闭</Button>}
      onCancel={onClose}
      destroyOnClose
      style={{ top: 48 }}
      styles={{
        body: {
          maxHeight: "calc(100dvh - 210px)",
          overflowY: "auto",
          paddingRight: 4,
        },
      }}
    >
      <div
        className={`operation-result-banner ${o.status === "SUCCESS" ? "is-success" : o.status === "UNCERTAIN" || failed(o) ? "is-warning" : "is-active"}`}
      >
        <div className="operation-result-icon">
          {o.status === "SUCCESS" ? (
            <CheckCircleOutlined />
          ) : o.status === "UNCERTAIN" || failed(o) ? (
            <ExclamationCircleOutlined />
          ) : (
            <ClockCircleOutlined />
          )}
        </div>
        <div>
          <div className="operation-result-title">
            {labels[o.status] || o.status}
          </div>
          <Typography.Paragraph>{o.message}</Typography.Paragraph>
          {o.node_locked && <Tag color="warning">节点操作锁定</Tag>}
        </div>
      </div>
      <div className="operation-detail-grid">
        <Card size="small" title="操作对象">
          <div className="operation-target">
            <Typography.Text strong>{t.name}</Typography.Text>
            <Typography.Text type="secondary">{t.ip}</Typography.Text>
          </div>
          <div className="operation-health">
            <span>检查状态</span>
            <Space>
              <Tag>{healthLabels[o.before_status] || o.before_status}</Tag>
              <span>→</span>
              <Tag color={o.after_status === "HEALTHY" ? "success" : "default"}>
                {healthLabels[o.after_status || ""] ||
                  o.after_status ||
                  "等待复查"}
              </Tag>
            </Space>
          </div>
        </Card>
        <Card size="small" title="执行时间">
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: "start", label: "发起", children: time(o.requested_at) },
              { key: "end", label: "完成", children: time(o.finished_at) },
              {
                key: "duration",
                label: "总耗时",
                children: (
                  <Typography.Text strong>{duration(o)}</Typography.Text>
                ),
              },
            ]}
          />
        </Card>
      </div>
      <Card size="small" title="发起信息" className="operation-info-card">
        <Descriptions
          column={1}
          size="small"
          items={[
            { key: "reason", label: "操作原因", children: o.reason },
            {
              key: "source",
              label: "实际来源",
              children: (
                <Typography.Text className="operation-identifier">
                  {o.authenticated_source}
                </Typography.Text>
              ),
            },
            {
              key: "id",
              label: "操作 ID",
              children: (
                <Typography.Text copyable className="operation-identifier">
                  {o.operation_id}
                </Typography.Text>
              ),
            },
          ]}
        />
      </Card>
      {canAudit && (
        <Card
          size="small"
          title="执行过程"
          className="operation-timeline-card"
          loading={loading}
        >
          {error ? (
            <Alert type="warning" showIcon message={error} />
          ) : !steps.length ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="暂无过程记录"
            />
          ) : (
            <Timeline
              items={steps.map((step, index) => ({
                color:
                  step.status === "SUCCESS"
                    ? "green"
                    : ["UNCERTAIN", "FAILED", "REJECTED", "TIMEOUT"].includes(
                          step.status,
                        )
                      ? "orange"
                      : "blue",
                children: (
                  <div className="operation-step">
                    <div>
                      <Typography.Text strong>
                        {stages[step.kind] || labels[step.status] || step.kind}
                      </Typography.Text>
                      <Typography.Text type="secondary">
                        {time(step.at)}
                      </Typography.Text>
                    </div>
                    <Typography.Paragraph type="secondary">
                      {["UNCERTAIN", "FAILED", "REJECTED", "TIMEOUT"].includes(
                        step.status,
                      )
                        ? step.message
                        : stepDescriptions[step.kind] || step.message}
                    </Typography.Paragraph>
                  </div>
                ),
              }))}
            />
          )}
        </Card>
      )}
      <Typography.Paragraph type="secondary" className="operations-footnote">
        {o.status === "UNCERTAIN"
          ? "结果未知不代表失败，也不会自动重复重启。请先现场核实。"
          : "操作结果与业务健康分别判断，详情只记录本次容器重启及复查。"}
      </Typography.Paragraph>
    </Modal>
  );
}
