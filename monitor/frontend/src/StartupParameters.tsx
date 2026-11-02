import { useEffect, useState } from "react";
import Alert from "antd/es/alert";
import Empty from "antd/es/empty";
import Spin from "antd/es/spin";
import Button from "antd/es/button";
import Table from "antd/es/table";
import Typography from "antd/es/typography";
import type { API } from "./api";

type Startup = {
  collected_at: string;
  incomplete: boolean;
  truncated: boolean;
  redacted_fields: string[];
  config: {
    image: string;
    entrypoint: string[];
    cmd: string[];
    path: string;
    args: string[];
    working_dir: string;
    user: string;
    network_mode: string;
    restart_policy: { name: string };
    env: Array<{ name: string; value: string }>;
    mounts: Array<{
      source: string;
      target: string;
      type: string;
      read_only: boolean;
    }>;
    port_bindings: Array<{
      container_port: string;
      host_ip: string;
      host_port: string;
    }>;
  };
};
export default function StartupParameters({
  api,
  nodeID,
}: {
  api: API;
  nodeID: string;
}) {
  const [data, setData] = useState<{
      startup: Startup | null;
      stale: boolean;
      reported_at?: string;
      container_id?: string;
    } | null>(null),
    [error, setError] = useState("");
  const [requestVersion, setRequestVersion] = useState(0);
  useEffect(() => {
    let active = true,
      loading = false;
    setData(null);
    setError("");
    async function load() {
      if (loading) return;
      loading = true;
      await api
        .get<{
          startup: Startup | null;
          stale: boolean;
          reported_at?: string;
          container_id?: string;
        }>(`/api/v1/nodes/startup?node_id=${encodeURIComponent(nodeID)}`)
        .then((d) => {
          if (active) {
            setData(d);
            setError("");
          }
        })
        .catch((e) => {
          if (active)
            setError(e instanceof Error ? e.message : "启动参数读取失败");
        })
        .finally(() => {
          loading = false;
        });
    }
    void load();
    const timer = window.setInterval(() => {
      if (!document.hidden) void load();
    }, 15000);
    return () => {
      window.clearInterval(timer);
      active = false;
    };
  }, [api, nodeID, requestVersion]);
  if (error)
    return (
      <Alert
        showIcon
        type="warning"
        message={error}
        action={
          <Button size="small" onClick={() => setRequestVersion((v) => v + 1)}>
            重试
          </Button>
        }
      />
    );
  if (!data) return <Spin />;
  if (!data.startup)
    return (
      <Empty
        description="等待 Agent 首次上报启动参数"
        image={Empty.PRESENTED_IMAGE_SIMPLE}
      />
    );
  const s = data.startup,
    c = s.config,
    command = (values: string[]) =>
      values?.length ? JSON.stringify(values) : "未设置";
  type Row = { key: string; group: string; name: string; value: string };
  const rows: Row[] = [
    { key: "image", group: "基础", name: "镜像", value: c.image },
    {
      key: "entry",
      group: "命令",
      name: "Entrypoint",
      value: command(c.entrypoint),
    },
    { key: "cmd", group: "命令", name: "Cmd", value: command(c.cmd) },
    { key: "path", group: "命令", name: "执行文件", value: c.path || "未设置" },
    ...(JSON.stringify(c.args) !== JSON.stringify(c.cmd)
      ? [{ key: "args", group: "命令", name: "Args", value: command(c.args) }]
      : []),
    {
      key: "dir",
      group: "运行",
      name: "工作目录",
      value: c.working_dir || "默认",
    },
    {
      key: "user",
      group: "运行",
      name: "运行用户",
      value: c.user || "镜像默认",
    },
    {
      key: "network",
      group: "运行",
      name: "网络模式",
      value: c.network_mode || "默认",
    },
    {
      key: "restart",
      group: "运行",
      name: "重启策略",
      value: c.restart_policy?.name || "默认",
    },
    ...(c.env?.length
      ? c.env.map((item, index) => ({
          key: "env-" + index,
          group: "环境",
          name: item.name,
          value: item.value === "" ? '""' : item.value,
        }))
      : [
          {
            key: "env-empty",
            group: "环境",
            name: "环境变量",
            value: "未配置",
          },
        ]),
    ...(c.mounts?.length
      ? c.mounts.map((item, index) => ({
          key: "mount-" + index,
          group: "挂载",
          name: item.target,
          value:
            item.source +
            " · " +
            item.type +
            " · " +
            (item.read_only ? "只读" : "读写"),
        }))
      : [{ key: "mount-empty", group: "挂载", name: "挂载", value: "未配置" }]),
    ...(c.port_bindings?.length
      ? c.port_bindings.map((item, index) => ({
          key: "port-" + index,
          group: "端口",
          name: item.container_port,
          value: (item.host_ip || "默认地址") + ":" + item.host_port,
        }))
      : [
          {
            key: "port-empty",
            group: "端口",
            name: "端口绑定",
            value:
              c.network_mode === "host"
                ? "使用主机网络，无独立端口绑定"
                : "未配置",
          },
        ]),
  ];
  return (
    <div className="startup-parameters startup-unified">
      <div className="startup-toolbar">
        <Typography.Text type="secondary">
          最新上报 ·{" "}
          {new Date(data.reported_at || s.collected_at).toLocaleString(
            "zh-CN",
            { hour12: false },
          )}{" "}
          · {rows.length} 项
        </Typography.Text>
        <Button size="small" onClick={() => setRequestVersion((v) => v + 1)}>
          刷新记录
        </Button>
      </div>
      {(data.stale || s.incomplete || s.truncated) && (
        <Alert
          className="page-alert"
          type="warning"
          showIcon
          message={
            data.stale
              ? "最后一次有效上报记录；容器未运行、离线或记录已过期"
              : "部分启动配置未完整读取"
          }
        />
      )}
      {data.stale && data.container_id && (
        <Typography.Paragraph
          type="secondary"
          style={{ fontSize: 12, marginBottom: 8 }}
        >
          记录对应容器 ID：{data.container_id}
        </Typography.Paragraph>
      )}
      <Table<Row>
        size="small"
        bordered
        rowKey="key"
        dataSource={rows}
        pagination={false}
        scroll={{ y: 360 }}
        tableLayout="fixed"
        columns={[
          { title: "分类", dataIndex: "group", width: 64 },
          { title: "参数", dataIndex: "name", width: 130 },
          {
            title: "实际值",
            dataIndex: "value",
            render: (value: string) => (
              <Typography.Text
                className="startup-table-value"
                copyable={{ text: value }}
              >
                {value}
              </Typography.Text>
            ),
          },
        ]}
      />
    </div>
  );
}
