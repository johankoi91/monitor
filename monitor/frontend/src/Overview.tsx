import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Statistic from "antd/es/statistic";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import RTCOverview from "./RTCOverview";
import type { API } from "./api";
import type { Notifications, ServiceStatus } from "./types";
import type { WorkspaceRoute } from "./navigation";
export default function Overview({
  api,
  services,
  notifications,
  onNavigate,
}: {
  api: API;
  services: ServiceStatus[];
  notifications: Notifications;
  onNavigate: (route: WorkspaceRoute) => void;
}) {
  const nodes = services.flatMap((s) => s.nodes);
  const healthy = nodes.filter(
    (n) => n.status === "HEALTHY" && !n.stale,
  ).length;
  const unknown = nodes.filter((n) => n.status === "UNKNOWN" || n.stale).length;
  return (
    <section aria-label="运行总览">
      <div className="workspace-metrics">
        <Card size="small">
          <Statistic title="已纳管容器" value={nodes.length} />
          <Typography.Text type="secondary">当前纳管范围</Typography.Text>
          <Button
            type="link"
            onClick={() => onNavigate({ view: "status", page: "" })}
          >
            查看容器
          </Button>
        </Card>
        <Card size="small">
          <Statistic
            title="容器检查正常"
            value={healthy}
            suffix={`/ ${nodes.length}`}
          />
          <Typography.Text type="secondary">
            当前配置的容器 / 就绪检查
          </Typography.Text>
        </Card>
        <Card size="small">
          <Statistic title="容器状态待确认" value={unknown} />
          <Typography.Text type="secondary">
            未知或采集数据已过期
          </Typography.Text>
        </Card>
        <Card size="small">
          <Statistic title="通知累计失败" value={notifications.failed ?? "—"} />
          <Typography.Text type="secondary">
            累计统计，非当前异常数量
          </Typography.Text>
          <Button
            type="link"
            onClick={() =>
              onNavigate({ view: "notifications", page: "delivery" })
            }
          >
            查看投递
          </Button>
        </Card>
      </div>
      <RTCOverview
        api={api}
        onOpen={() => onNavigate({ view: "rtc", page: "tasks" })}
      />
      <Alert
        type="info"
        showIcon
        message="容器检查与 RTC 业务分别判断"
        description="容器运行、TCP 就绪和探针连接在线，均不能单独证明音视频端到端可用。RTC 结论需结合对应执行轮次与有效证据。"
      />
      <Card title="纳管范围" className="page-card">
        <Table
          size="small"
          rowKey={(s) => `${s.cluster_code}/${s.service_code}`}
          dataSource={services}
          pagination={false}
          scroll={{ x: 520 }}
          locale={{ emptyText: "尚未建立监控基准" }}
          columns={[
            {
              title: "分组 / 服务",
              render: (_, s) => (
                <>
                  <Typography.Text strong>
                    {s.group_name || s.cluster_code}
                  </Typography.Text>
                  <div>{s.service_name}</div>
                </>
              ),
            },
            { title: "纳管容器", dataIndex: "expected_node_count" },
            {
              title: "容器状态",
              render: (_, s) => (
                <Tag
                  color={
                    s.status === "HEALTHY"
                      ? "success"
                      : s.status === "UNKNOWN"
                        ? "warning"
                        : "error"
                  }
                >
                  {s.status === "HEALTHY"
                    ? "检查正常"
                    : s.status === "UNKNOWN"
                      ? "待确认"
                      : "存在异常"}
                </Tag>
              ),
            },
            { title: "检查范围", render: () => "容器与已配置就绪检查" },
          ]}
        />
      </Card>
    </section>
  );
}
