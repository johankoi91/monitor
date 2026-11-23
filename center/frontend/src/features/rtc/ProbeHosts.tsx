import { CloudServerOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Descriptions from "antd/es/descriptions";
import Empty from "antd/es/empty";
import Modal from "antd/es/modal";
import Tag from "antd/es/tag";
import { useState } from "react";
import type { ProbeHost, ProbeRuntime } from "./RTCModel";
import { PageHeading } from "./RTCUI";
import { RTCDialogIntro, RTCSectionHeading } from "./RTCVisual";

const clientName = (client: string) =>
  (({ native: "Native", web: "Web" }) as Record<string, string>)[client] ||
  client;
export default function ProbeHosts({
  hosts,
  canManage,
  busy,
  operate,
}: {
  hosts: ProbeHost[];
  canManage: boolean;
  busy: string;
  operate: (key: string, path: string, body: unknown) => Promise<void>;
}) {
  const [help, setHelp] = useState(false),
    [selected, setSelected] = useState<string | null>(null);
  const host = hosts.find((item) => item.host_id === selected);
  const toggle = (runtime: ProbeRuntime) => {
    const save = () =>
      operate(
        runtime.runtime_id,
        `/api/v1/probes/runtimes/${encodeURIComponent(runtime.runtime_id)}/enabled`,
        { enabled: runtime.enabled === false },
      );
    if (runtime.enabled === false) void save().catch(() => {});
    else
      Modal.confirm({
        className: "rtc-confirm",
        title: `停用 ${runtime.service_type.toUpperCase()} / ${clientName(runtime.client_type)}？`,
        content:
          "只停止该客户端的任务分配并取消其活动用例，服务器上的其他服务和客户端保持各自状态。",
        okText: "停用客户端",
        cancelText: "取消",
        onOk: save,
      });
  };
  return (
    <>
      <PageHeading
        title="探测服务器"
        subtitle="服务器主动接入，按服务类型和客户端汇总能力。"
        actions={
          <Button
            icon={<CloudServerOutlined aria-hidden="true" />}
            onClick={() => setHelp(true)}
          >
            部署接入
          </Button>
        }
      />
      <div className="rtc-panel">
        <div className="rtc-toolbar">
          <span>
            {hosts.filter((h) => h.online).length} 台在线 · {hosts.length}{" "}
            台服务器
          </span>
        </div>
        {hosts.length ? (
          <div className="rtc-table-wrap">
            <table className="rtc-table" aria-label="探测服务器清单">
              <thead>
                <tr>
                  <th>服务器</th>
                  <th>支持的探测服务 / 客户端</th>
                  <th>连接</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {hosts.map((item) => (
                  <tr key={item.host_id}>
                    <td>
                      <strong>{item.host_name}</strong>
                      <div className="rtc-secondary">{item.host_address}</div>
                      <div className="rtc-secondary">{item.location}</div>
                    </td>
                    <td>
                      {item.services.map((service) => (
                        <div
                          key={service.service_type}
                          style={{ marginBottom: 6 }}
                        >
                          <strong>{service.service_type.toUpperCase()}</strong>
                          {" · "}
                          {service.clients.map((runtime) => (
                            <Tag
                              key={runtime.runtime_id}
                              color={
                                runtime.enabled === false
                                  ? "default"
                                  : runtime.online
                                    ? "blue"
                                    : "warning"
                              }
                            >
                              {clientName(runtime.client_type)} ·{" "}
                              {runtime.enabled === false
                                ? "已停用"
                                : runtime.online
                                  ? "在线"
                                  : "离线"}
                            </Tag>
                          ))}
                        </div>
                      ))}
                    </td>
                    <td>
                      <Tag color={item.online ? "success" : "default"}>
                        {item.online ? "在线" : "离线"}
                      </Tag>
                    </td>
                    <td>
                      <Button
                        type="link"
                        onClick={() => setSelected(item.host_id)}
                      >
                        详情
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty description="尚无服务器上报。部署探测客户端后将自动出现在这里。" />
        )}
      </div>
      <Modal
        open={help}
        className="rtc-dialog"
        title="探测服务器主动接入"
        onCancel={() => setHelp(false)}
        footer={<Button onClick={() => setHelp(false)}>知道了</Button>}
      >
        <RTCDialogIntro>
          服务器启动后自动出现在列表中，共享服务器身份以合并多个客户端。
        </RTCDialogIntro>
        <ol>
          <li>在服务器配置中心地址和统一探测服务接入凭证。</li>
          <li>
            每台服务器部署一个探测服务容器，内置 Native 和 Web
            客户端，共用服务器身份、分别保存执行数据。
          </li>
          <li>
            启动后自动上报服务器、服务类型及客户端类型；任务选择服务器与所需客户端。
          </li>
        </ol>
        <Alert
          type="info"
          showIcon
          message="一个容器提供多个客户端能力，任务按客户端独立启停；接入凭证由部署侧配置。"
        />
      </Modal>
      <Modal
        open={!!host}
        width={720}
        className="rtc-dialog"
        title={host?.host_name}
        onCancel={() => setSelected(null)}
        footer={<Button onClick={() => setSelected(null)}>关闭</Button>}
      >
        {host && (
          <>
            <Descriptions
              size="small"
              column={1}
              items={[
                { key: "id", label: "服务器标识", children: host.host_id },
                { key: "address", label: "地址", children: host.host_address },
                {
                  key: "location",
                  label: "位置",
                  children: host.location || "未上报",
                },
              ]}
            />
            {host.services.map((service) => (
              <section key={service.service_type}>
                <RTCSectionHeading title={service.service_type.toUpperCase()} />
                {service.clients.map((runtime) => (
                  <div key={runtime.runtime_id} className="rtc-panel rtc-pad">
                    <div className="rtc-section-toolbar">
                      <strong>
                        {clientName(runtime.client_type)}{" "}
                        <Tag>
                          {runtime.enabled === false
                            ? "已停用"
                            : runtime.online
                              ? "在线"
                              : "离线"}
                        </Tag>
                      </strong>
                      {canManage && (
                        <Button
                          disabled={!!busy}
                          loading={busy === runtime.runtime_id}
                          danger={runtime.enabled !== false}
                          onClick={() => toggle(runtime)}
                        >
                          {runtime.enabled === false
                            ? "启用客户端"
                            : "停用客户端"}
                        </Button>
                      )}
                    </div>
                    <Descriptions
                      size="small"
                      column={{ xs: 1, sm: 2 }}
                      items={[
                        {
                          key: "version",
                          label: "客户端版本",
                          children: runtime.runtime_version || "未上报",
                        },
                        {
                          key: "sdk",
                          label: "SDK",
                          children: runtime.sdk_version || "未上报",
                        },
                        {
                          key: "capacity",
                          label: "容量",
                          children:
                            runtime.capacity === undefined
                              ? "未上报"
                              : `${runtime.capacity} 槽位`,
                        },
                        {
                          key: "platform",
                          label: "平台",
                          children: runtime.platform || "未上报",
                        },
                      ]}
                    />
                    {runtime.resource_error && (
                      <Alert type="warning" message={runtime.resource_error} />
                    )}
                  </div>
                ))}
              </section>
            ))}
          </>
        )}
      </Modal>
    </>
  );
}
