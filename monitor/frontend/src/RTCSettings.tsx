import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import Switch from "antd/es/switch";
import Typography from "antd/es/typography";
import InputNumber from "antd/es/input-number";
import Button from "antd/es/button";

export interface MediaEdgeRange {
  enabled: boolean;
  host: string;
  start_port: number;
  end_port: number;
}
export interface NativeCoverageConfig {
  groups: Array<{ group: string; entrypoints: string[] }>;
  cross_edge: boolean;
  excluded?: string[];
}
export interface NativeDiscovery {
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
    host: "111.230.192.59",
    start_port: 4001,
    end_port: 4008,
  },
  app_id: "",
  auth_mode: "certificate",
  private_deployment: true,
  access_ips: ["111.230.192.59"],
  access_domains: ["111.230.192.59"],
  verify_domain: "ap.1145326.agora.local",
  proxy_mode: 0,
  disable_aut: true,
};
const listProps = {
  normalize: (value: string) => value.split(/[\s,，]+/).filter(Boolean),
  getValueProps: (value?: string[]) => ({ value: (value || []).join(", ") }),
};
export default function RTCSettings({
  hasCertificate,
  discovery,
  excludedOptions = [],
}: {
  hasCertificate?: boolean;
  discovery: NativeDiscovery | null;
  excludedOptions?: Array<{ label: string; value: string }>;
}) {
  const form = Form.useFormInstance();
  const native = Form.useWatch("native_enabled");
  const edgeEnabled = Form.useWatch(["rtc", "media_edges", "enabled"]);
  const mode = Form.useWatch(["rtc", "auth_mode"]);
  const privateDeployment = Form.useWatch(["rtc", "private_deployment"]);
  const groupOptions = Object.entries(discovery?.by_ap || {}).map(
    ([group, edges]) => ({
      value: group,
      label: `${group} · UDP ${edges.filter((e) => e.edge_type === "udp").length} / AUT ${edges.filter((e) => e.edge_type === "aut").length}`,
    }),
  );
  return (
    <>
      <Typography.Title level={5}>RTC 接入与鉴权</Typography.Title>
      <Form.Item
        label="Agora AppID"
        name={["rtc", "app_id"]}
        rules={[
          { required: true, message: "请填写 AppID" },
          {
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
            { value: "certificate", label: "App Certificate" },
            { value: "none", label: "未开启鉴权" },
          ]}
        />
      </Form.Item>
      {mode === "certificate" && (
        <Form.Item
          label="App Certificate"
          name={["credentials", "app_certificate"]}
          extra={
            hasCertificate
              ? "已设置；留空保留原值。凭证不回显。"
              : "中心加密保存，不下发 App Certificate 给探针。"
          }
          rules={[
            {
              validator: (_, value) =>
                (hasCertificate && !value) ||
                /^[a-fA-F0-9]{32}$/.test(value || "")
                  ? Promise.resolve()
                  : Promise.reject(new Error("请填写 32 位 App Certificate")),
            },
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      )}
      <Form.Item
        label="Native UDP / AUT 自动覆盖"
        name="native_enabled"
        valuePropName="checked"
      >
        <Switch
          onChange={(enabled) => {
            if (enabled && !form.getFieldValue(["rtc", "native_coverage"]))
              form.setFieldValue(["rtc", "native_coverage"], {
                groups: [
                  {
                    group:
                      groupOptions.length === 1
                        ? groupOptions[0].value
                        : undefined,
                    entrypoints:
                      form.getFieldValue(["rtc", "access_ips"]) || [],
                  },
                ],
                cross_edge: true,
                excluded: [],
              });
          }}
        />
      </Form.Item>
      {native ? (
        <>
          <Typography.Paragraph type="secondary">
            按 AP 分组自动发现 UDP/AUT 实例，验证 AP 接入、逐实例固定地址和跨
            Edge 音视频互通。
          </Typography.Paragraph>
          <Form.List name={["rtc", "native_coverage", "groups"]}>
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <div key={field.key}>
                    <Form.Item
                      label="AP 分组"
                      name={[field.name, "group"]}
                      rules={[{ required: true, message: "请选择 AP 分组" }]}
                    >
                      <Select
                        options={groupOptions}
                        placeholder="从已上报的启动参数中选择"
                      />
                    </Form.Item>
                    <Form.Item
                      label="AP 客户端 IP（ip_for_client）"
                      name={[field.name, "entrypoints"]}
                      {...listProps}
                      extra="填写 AP 的客户端入口 IP，多个入口用逗号分隔。分组地址和机器 IP 不作为连接地址。"
                      rules={[
                        { required: true, message: "请填写 AP 客户端 IP" },
                      ]}
                    >
                      <Input />
                    </Form.Item>
                    {fields.length > 1 && (
                      <Button size="small" onClick={() => remove(field.name)}>
                        移除此分组
                      </Button>
                    )}
                  </div>
                ))}
                <Button size="small" onClick={() => add({ entrypoints: [] })}>
                  添加 AP 分组
                </Button>
              </>
            )}
          </Form.List>
          <Form.Item
            style={{ marginTop: 16 }}
            label="跨 Edge 环形覆盖"
            name={["rtc", "native_coverage", "cross_edge"]}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>

          <Form.Item
            label="已确认移出覆盖的实例"
            name={["rtc", "native_coverage", "excluded"]}
            extra="消失或离线的实例会保留为待验证；确认下线后在这里移出。"
          >
            <Select mode="multiple" allowClear options={excludedOptions} />
          </Form.Item>
          <Form.Item label="证书校验域名" name={["rtc", "verify_domain"]}>
            <Input />
          </Form.Item>
        </>
      ) : (
        <>
          <Typography.Title level={5}>
            UDP media edge 逐端口探测
          </Typography.Title>
          <Form.Item
            label="启用逐端口探测"
            name={["rtc", "media_edges", "enabled"]}
            valuePropName="checked"
          >
            <Switch
              onChange={(enabled) => {
                if (enabled) {
                  const current =
                    form.getFieldValue(["rtc", "media_edges"]) || {};
                  form.setFieldValue(["rtc", "media_edges"], {
                    enabled: true,
                    host: current.host || "111.230.192.59",
                    start_port: current.start_port || 4001,
                    end_port: current.end_port || 4008,
                  });
                }
              }}
            />
          </Form.Item>
          {edgeEnabled && (
            <>
              <Form.Item
                label="UDP edge IP / 主机名"
                name={["rtc", "media_edges", "host"]}
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>
              <div className="rtc-form-grid">
                <Form.Item
                  label="起始 UDP 端口"
                  name={["rtc", "media_edges", "start_port"]}
                  rules={[{ required: true }]}
                >
                  <InputNumber min={1} max={65535} precision={0} />
                </Form.Item>
                <Form.Item
                  label="结束 UDP 端口"
                  name={["rtc", "media_edges", "end_port"]}
                  dependencies={[["rtc", "media_edges", "start_port"]]}
                  rules={[
                    { required: true },
                    ({ getFieldValue }) => ({
                      validator: (_, value) =>
                        value >=
                          getFieldValue(["rtc", "media_edges", "start_port"]) &&
                        value -
                          getFieldValue(["rtc", "media_edges", "start_port"]) <
                          64
                          ? Promise.resolve()
                          : Promise.reject(
                              new Error(
                                "结束端口不得小于起始端口，一轮最多 64 个端口",
                              ),
                            ),
                    }),
                  ]}
                >
                  <InputNumber min={1} max={65535} precision={0} />
                </Form.Item>
              </div>
            </>
          )}
          <Typography.Title level={5}>私有化部署</Typography.Title>
          <Form.Item
            label="连接私有化部署"
            name={["rtc", "private_deployment"]}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          {privateDeployment && (
            <>
              <Form.Item
                label="AP 客户端 IP（ip_for_client）"
                name={["rtc", "access_ips"]}
                {...listProps}
              >
                <Input />
              </Form.Item>
              <Form.Item
                label="接入域名列表"
                name={["rtc", "access_domains"]}
                {...listProps}
              >
                <Input />
              </Form.Item>
              <Form.Item label="证书校验域名" name={["rtc", "verify_domain"]}>
                <Input />
              </Form.Item>
              <Form.Item label="接入路径策略" name={["rtc", "proxy_mode"]}>
                <Select
                  options={[
                    { value: 0, label: "优先私有化接入（ConnectivityFirst）" },
                    { value: 1, label: "仅连接配置的私有化接入点" },
                  ]}
                />
              </Form.Item>
              <Form.Item
                label="禁用 vos-aut"
                name={["rtc", "disable_aut"]}
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
            </>
          )}
        </>
      )}
    </>
  );
}
