import Alert from "antd/es/alert";
import Checkbox from "antd/es/checkbox";
import Form from "antd/es/form";
import Input from "antd/es/input";
import InputNumber from "antd/es/input-number";
import Select from "antd/es/select";
import Tabs from "antd/es/tabs";
import { RTCSectionHeading } from "./RTCVisual";
export interface MediaEdgeRange {
  enabled: boolean;
  host: string;
  start_port: number;
  end_port: number;
}
export interface NativeCoverageConfig {
  groups: Array<{ agent_id?: string; group: string; entrypoints: string[] }>;
  cross_edge: boolean;
  excluded?: string[];
}
export interface DiscoveredAP {
  agent_id: string;
  address: string;
  groups: string[];
  stale: boolean;
  container_name: string;
}
export interface NativeDiscovery {
  access_points: DiscoveredAP[];
  by_ap: Record<
    string,
    Array<{
      edge_type: string;
      agent_id: string;
      container_name: string;
      ip_for_client: string;
      user_port: number;
      stale: boolean;
    }>
  >;
  skipped: Array<{ container_name: string; reason: string }>;
}
export interface RTCParameters {
  native_coverage?: NativeCoverageConfig | null;
  web_access?: {
    port: number;
    report_port: number;
    log_port: number;
    ignore_certificates: boolean;
    coverage?: NativeCoverageConfig;
  } | null;
  media_edges?: MediaEdgeRange | null;
  app_id: string;
  auth_mode: string;
  private_deployment: boolean;
  access_ips?: string[];
  access_domains?: string[];
  verify_domain?: string;
  proxy_mode: number;
  disable_aut: boolean;
}
export const rtcDefaults: RTCParameters = {
  media_edges: {
    enabled: false,
    host: "",
    start_port: 4001,
    end_port: 4008,
  },
  app_id: "",
  auth_mode: "certificate",
  private_deployment: true,
  access_ips: [],
  access_domains: [],
  verify_domain: "",
  proxy_mode: 0,
  disable_aut: true,
};
function APSelector({
  name,
  label,
  discovery,
}: {
  name: string;
  label: string;
  discovery: NativeDiscovery | null;
}) {
  const form = Form.useFormInstance(),
    selected = Form.useWatch(name, form) || [];
  const points = discovery?.access_points || [];
  const options = points.map((ap) => ({
    value: ap.agent_id,
    label: ap.address + (ap.stale ? "（待更新）" : ""),
    disabled: ap.stale,
  }));
  for (const id of selected)
    if (!options.some((o) => o.value === id))
      options.push({ value: id, label: "未发现的 AP 节点", disabled: true });
  return (
    <Form.Item
      label={label}
      name={name}
      rules={[{ required: true, message: "请选择自动发现的 AP 节点" }]}
    >
      <Select
        mode="multiple"
        options={options}
        placeholder={discovery ? "选择 AP 所在节点" : "正在读取台账"}
      />
    </Form.Item>
  );
}
export default function RTCSettings({
  hasCertificate,
  existingAppId,
  discovery,
  discoveryError,
  excludedOptions = [],
}: {
  hasCertificate?: boolean;
  existingAppId?: string;
  discovery: NativeDiscovery | null;
  discoveryError?: string;
  excludedOptions?: { label: string; value: string }[];
}) {
  const form = Form.useFormInstance(),
    native = Form.useWatch("native_enabled", form),
    web = Form.useWatch("web_enabled", form),
    auth = Form.useWatch(["rtc", "auth_mode"], form),
    app = Form.useWatch(["rtc", "app_id"], form),
    independent = Form.useWatch("web_independent", form),
    selected = Form.useWatch("ap_agent_ids", form) || [];
  const groups = new Set(
    (discovery?.access_points || [])
      .filter((p) => selected.includes(p.agent_id))
      .flatMap((p) => p.groups),
  );
  const edges = [
    ...new Map(
      Object.entries(discovery?.by_ap || {})
        .filter(([group]) => groups.has(group))
        .flatMap(([, rows]) => rows)
        .map((e) => [`${e.edge_type}/${e.ip_for_client}/${e.user_port}`, e]),
    ).values(),
  ];
  const ensureWeb = (enabled: boolean) => {
    if (enabled && !form.getFieldValue(["rtc", "web_access"]))
      form.setFieldValue(["rtc", "web_access"], {
        port: 443,
        report_port: 6443,
        log_port: 6444,
        ignore_certificates: false,
        coverage: { cross_edge: true, excluded: [] },
      });
  };
  return (
    <>
      <section className="rtc-form-block">
        <RTCSectionHeading title="基础与鉴权" />
        <div className="rtc-form-grid">
          <Form.Item
            label="Agora AppID"
            name={["rtc", "app_id"]}
            rules={[
              {
                required: true,
                pattern: /^[a-fA-F0-9]{32}$/,
                message: "AppID 为 32 位十六进制标识",
              },
            ]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item label="鉴权方式" name={["rtc", "auth_mode"]}>
            <Select
              options={[
                { value: "none", label: "未开启鉴权" },
                { value: "certificate", label: "App Certificate" },
              ]}
            />
          </Form.Item>
        </div>
        {auth === "certificate" && (
          <Form.Item
            label="App Certificate"
            name={["credentials", "app_certificate"]}
            extra={
              hasCertificate && app === existingAppId
                ? "已设置，留空保留原值；凭证不回显。"
                : "凭证不回显，仅由中心签发短期 Token。"
            }
            rules={[
              {
                validator: (_, v) =>
                  (!v && hasCertificate && app === existingAppId) ||
                  /^[a-fA-F0-9]{32}$/.test(v || "")
                    ? Promise.resolve()
                    : Promise.reject(new Error("请填写 32 位 App Certificate")),
              },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        )}
      </section>
      <section className="rtc-form-block rtc-capability-section">
        <RTCSectionHeading
          title="客户端能力"
          description="Native 与 Web 可独立配置接入参数。"
        />
        <div className="rtc-form-grid">
          <Form.Item name="native_enabled" valuePropName="checked">
            <Checkbox>Native · Go Linux SDK</Checkbox>
          </Form.Item>
          <Form.Item name="web_enabled" valuePropName="checked">
            <Checkbox onChange={(e) => ensureWeb(e.target.checked)}>
              Web · 无头浏览器
            </Checkbox>
          </Form.Item>
        </div>
      </section>
      {(native || web) && (
        <>
          <RTCSectionHeading title="AP 接入" />
          {discoveryError && (
            <Alert type="warning" showIcon message={discoveryError} />
          )}
          {discovery && !discovery.access_points?.length && (
            <Alert
              type="warning"
              showIcon
              message="台账中尚未发现包含 agora_local_ap 的节点"
            />
          )}
          <APSelector
            name="ap_agent_ids"
            label="AP 节点（自动发现）"
            discovery={discovery}
          />
          {edges.length > 0 && (
            <details>
              <summary>发现的 Edge（{edges.length}）</summary>
              <table className="rtc-table">
                <thead>
                  <tr>
                    <th scope="col">类型</th>
                    <th scope="col">客户端 IP</th>
                    <th scope="col">端口</th>
                  </tr>
                </thead>
                <tbody>
                  {edges.map((e) => (
                    <tr
                      key={`${e.edge_type}/${e.ip_for_client}/${e.user_port}`}
                    >
                      <td data-label="类型">{e.edge_type}</td>
                      <td data-label="客户端 IP">{e.ip_for_client}</td>
                      <td data-label="端口">{e.user_port}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </details>
          )}
        </>
      )}
      <Tabs
        className="rtc-client-config-tabs"
        items={[
          ...(native
            ? [
                {
                  key: "native",
                  label: "Native 接入",
                  forceRender: true,
                  children: (
                    <>
                      <Form.Item
                        label="证书校验域名"
                        name={["rtc", "verify_domain"]}
                      >
                        <Input />
                      </Form.Item>
                      <details>
                        <summary>覆盖设置</summary>
                        <Form.Item
                          label="跨 Edge 环形覆盖"
                          name={["rtc", "native_coverage", "cross_edge"]}
                          valuePropName="checked"
                        >
                          <Checkbox>启用</Checkbox>
                        </Form.Item>
                        <Form.Item
                          label="已确认移出覆盖的 Native 实例"
                          name={["rtc", "native_coverage", "excluded"]}
                        >
                          <Select
                            mode="multiple"
                            allowClear
                            options={excludedOptions.filter(
                              (o) => !o.label.startsWith("WEB"),
                            )}
                          />
                        </Form.Item>
                      </details>
                    </>
                  ),
                },
              ]
            : []),
          ...(web
            ? [
                {
                  key: "web",
                  label: "Web 接入",
                  forceRender: true,
                  children: (
                    <>
                      <div className="rtc-form-grid rtc-three">
                        <Form.Item
                          label="Web AP HTTPS 端口"
                          name={["rtc", "web_access", "port"]}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={1} max={65535} precision={0} />
                        </Form.Item>
                        <Form.Item
                          label="事件上报端口"
                          name={["rtc", "web_access", "report_port"]}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={1} max={65535} precision={0} />
                        </Form.Item>
                        <Form.Item
                          label="SDK 日志服务端口"
                          name={["rtc", "web_access", "log_port"]}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={1} max={65535} precision={0} />
                        </Form.Item>
                      </div>
                      <Form.Item
                        name={["rtc", "web_access", "ignore_certificates"]}
                        valuePropName="checked"
                      >
                        <Checkbox>忽略浏览器证书校验</Checkbox>
                      </Form.Item>
                      <details open={independent || undefined}>
                        <summary>覆盖设置</summary>
                        <Form.Item
                          name="web_independent"
                          valuePropName="checked"
                        >
                          <Checkbox>Web 使用独立 AP 关联</Checkbox>
                        </Form.Item>
                        {independent && (
                          <APSelector
                            name="web_ap_agent_ids"
                            label="Web AP 节点（自动发现）"
                            discovery={discovery}
                          />
                        )}
                        <Form.Item
                          name={["rtc", "web_access", "coverage", "cross_edge"]}
                          valuePropName="checked"
                        >
                          <Checkbox>Web Edge 环形互通</Checkbox>
                        </Form.Item>
                        <Form.Item
                          label="已确认移出覆盖的 Web 实例"
                          name={["rtc", "web_access", "coverage", "excluded"]}
                        >
                          <Select
                            mode="multiple"
                            allowClear
                            options={excludedOptions.filter((o) =>
                              o.label.startsWith("WEB"),
                            )}
                          />
                        </Form.Item>
                      </details>
                    </>
                  ),
                },
              ]
            : []),
        ]}
      />
    </>
  );
}
