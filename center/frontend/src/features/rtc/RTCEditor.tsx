import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Form from "antd/es/form";
import Input from "antd/es/input";
import InputNumber from "antd/es/input-number";
import Modal from "antd/es/modal";
import Radio from "antd/es/radio";
import Select from "antd/es/select";
import Space from "antd/es/space";
import { useEffect, useRef, useState } from "react";
import type { API } from "../../shared/api";
import {
  clientLabel,
  errorText,
  isInteropClient,
  rtcServerOptions,
  rtcTargetSupports,
  type Edit,
  type Snapshot,
  type Target,
  type Task,
} from "./RTCModel";
import RTCSettings, { rtcDefaults, type NativeDiscovery } from "./RTCSettings";
import { RTCDialogIntro, RTCSectionHeading } from "./RTCVisual";
const minuteProps = {
  getValueProps: (v?: number) => ({
    value: v === undefined ? undefined : v / 60,
  }),
  normalize: (v: number | null) =>
    v === null ? undefined : Math.round(v * 60),
};
export default function RTCEditor({
  api,
  edit,
  snapshot,
  onClose,
  onSave,
}: {
  api: API;
  edit: Edit;
  snapshot: Snapshot;
  onClose: () => void;
  onSave: (value: Task | Target, revision: string) => Promise<void>;
}) {
  const target = edit.kind === "target",
    [form] = Form.useForm(),
    [revision] = useState(snapshot.config_revision),
    [error, setError] = useState(""),
    [saving, setSaving] = useState(false),
    [discovery, setDiscovery] = useState<NativeDiscovery | null>(null),
    [discoveryError, setDiscoveryError] = useState("");
  const errorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  const selectedID = Form.useWatch("target_id", form),
    type = Form.useWatch("client_type", form) || "native";
  const currentTask = edit.value as Task | undefined;
  const serverOptions = rtcServerOptions(
    snapshot.hosts,
    type,
    currentTask?.client_type === type ? currentTask?.host_id : undefined,
  );
  const selected = snapshot.targets.find((t) => t.target_id === selectedID),
    coverage =
      type === "web"
        ? selected?.rtc?.web_access?.coverage
        : selected?.rtc?.native_coverage;
  useEffect(() => {
    let live = true;
    if (target)
      void api
        .get<NativeDiscovery>("/api/v1/containers/media-edges")
        .then((v) => {
          if (!live) return;
          setDiscovery(v);
          const config = (edit.value as Target | undefined)?.rtc;
          const nativeGroups = config?.native_coverage?.groups || [];
          const webGroups = config?.web_access?.coverage?.groups || [];
          const find = (groups: typeof nativeGroups) => [
            ...new Set(
              groups.flatMap((g) =>
                g.agent_id
                  ? [g.agent_id]
                  : (v.access_points || [])
                      .filter((ap) => ap.groups.includes(g.group))
                      .map((ap) => ap.agent_id),
              ),
            ),
          ];
          const points = (v.access_points || []).filter((ap) => !ap.stale);
          if (!form.isFieldTouched("ap_agent_ids"))
            form.setFieldValue(
              "ap_agent_ids",
              edit.value
                ? find(nativeGroups.length ? nativeGroups : webGroups)
                : points.length === 1
                  ? [points[0].agent_id]
                  : [],
            );
          if (!form.isFieldTouched("web_ap_agent_ids"))
            form.setFieldValue("web_ap_agent_ids", find(webGroups));
        })
        .catch((e) => {
          if (live) setDiscoveryError(errorText(e));
        });
    return () => {
      live = false;
    };
  }, [api, target, edit.value, form]);
  const source = target ? (edit.value as Target | undefined) : undefined,
    rtc = source?.rtc || rtcDefaults,
    nativeGroups = rtc.native_coverage?.groups,
    webGroups = rtc.web_access?.coverage?.groups;
  const excluded = new Map<string, { label: string; value: string }>();
  for (const state of Object.values(snapshot.native_coverage || {}))
    for (const member of state.expected || []) {
      if (
        snapshot.tasks.find(
          (t) =>
            t.target_id === source?.target_id &&
            snapshot.native_coverage?.[t.task_id] === state,
        )
      ) {
        const e = member.edge;
        excluded.set(`${e.agent_id}/${e.container_name}`, {
          value: `${e.agent_id}/${e.container_name}`,
          label: `${e.transport.toUpperCase()} ${e.container_name} · ${e.endpoint}${e.stale ? "（待验证）" : ""}`,
        });
      }
    }
  for (const edges of Object.values(discovery?.by_ap || {}))
    for (const e of edges)
      excluded.set(`${e.agent_id}/${e.container_name}`, {
        value: `${e.agent_id}/${e.container_name}`,
        label: `${e.edge_type.toUpperCase()} ${e.container_name} · ${e.ip_for_client}:${e.user_port}`,
      });
  const initial = target
    ? {
        ...source,
        rtc: structuredClone(rtc),
        native_enabled: !!rtc.native_coverage || !rtc.web_access,
        web_enabled: !!rtc.web_access,
        ap_agent_ids: [],
        web_ap_agent_ids: [],
        web_independent:
          !!webGroups &&
          !!nativeGroups &&
          JSON.stringify(webGroups) !== JSON.stringify(nativeGroups),
      }
    : edit.value || {
        task_id: `rtc-${crypto.randomUUID().replaceAll("-", "")}`,
        client_type: "native",
        service_type: "rtc",
        interval_seconds: 600,
        sweep_interval_seconds: 900,
        cross_interval_seconds: 1800,
        timeout_seconds: 30,
        failure_threshold: 3,
        recovery_threshold: 2,
        unknown_notify_seconds: 120,
      };
  const close = () => {
    if (form.isFieldsTouched())
      Modal.confirm({
        className: "rtc-confirm",
        title: "放弃未保存的修改？",
        okText: "放弃修改",
        cancelText: "继续编辑",
        onOk: onClose,
      });
    else onClose();
  };
  return (
    <Modal
      open
      className={`rtc-dialog rtc-editor-dialog ${target ? "rtc-target-editor" : "rtc-task-editor"}`}
      width={target ? 800 : 660}
      title={`${edit.value ? "编辑" : "创建"}${target ? "探测目标配置" : "探测点任务配置"}`}
      onCancel={close}
      footer={null}
      maskClosable={!saving}
      closable={!saving}
      keyboard={!saving}
      destroyOnClose
    >
      <RTCDialogIntro>
        {target
          ? "维护共享接入配置，客户端能力与覆盖范围分别设置。"
          : "选择探测类型、探测目标配置和已上报的执行服务器，再设置调度规则。"}
      </RTCDialogIntro>
      <Form
        form={form}
        layout="vertical"
        initialValues={initial}
        disabled={saving}
        scrollToFirstError={{ block: "center", focus: true }}
        onFinish={async (submitted) => {
          setError("");
          const value = { ...form.getFieldsValue(true), ...submitted };
          if (!target) {
            value.service_type = "rtc";
            if (isInteropClient(value.client_type)) {
              delete value.sweep_interval_seconds;
              delete value.cross_interval_seconds;
            }
          }
          try {
            if (target) {
              if (!value.native_enabled && !value.web_enabled)
                throw new Error("至少选择一种客户端能力");
              const related = snapshot.tasks.filter(
                (t) => t.target_id === source?.target_id && t.enabled,
              );
              if (
                related.some((t) =>
                  isInteropClient(t.client_type)
                    ? !value.web_enabled || !value.native_enabled
                    : t.client_type === "web"
                      ? !value.web_enabled
                      : !value.native_enabled,
                )
              )
                throw new Error("请先停用依赖被关闭客户端能力的任务");
              const bindings = (ids: string[]) => {
                if (!discovery) throw new Error("AP 发现数据尚未就绪");
                if (!ids?.length) throw new Error("请选择自动发现的 AP 节点");
                return ids.flatMap((id) => {
                  const ap = discovery.access_points?.find(
                    (p) => p.agent_id === id,
                  );
                  if (!ap || ap.stale)
                    throw new Error("所选 AP 未发现或数据已过期");
                  return ap.groups.map((group) => ({
                    agent_id: id,
                    group,
                    entrypoints: [ap.address],
                  }));
                });
              };
              const config = value.rtc;
              config.native_coverage = value.native_enabled
                ? {
                    ...config.native_coverage,
                    groups: bindings(value.ap_agent_ids),
                    cross_edge: config.native_coverage?.cross_edge ?? true,
                  }
                : null;
              config.media_edges = null;
              if (value.web_enabled) {
                config.web_access = {
                  ...config.web_access,
                  coverage: {
                    ...config.web_access?.coverage,
                    cross_edge: config.web_access?.coverage?.cross_edge ?? true,
                    groups: bindings(
                      value.web_independent
                        ? value.web_ap_agent_ids
                        : value.ap_agent_ids,
                    ),
                  },
                };
              } else config.web_access = null;
              if (config.native_coverage) config.private_deployment = true;
              for (const key of [
                "native_enabled",
                "web_enabled",
                "ap_agent_ids",
                "web_ap_agent_ids",
                "web_independent",
              ])
                delete value[key];
            }
            const oldExcluded = [
              ...(source?.rtc?.native_coverage?.excluded || []),
              ...(source?.rtc?.web_access?.coverage?.excluded || []),
            ];
            const excluded = target
              ? [
                  ...(value.rtc.native_coverage?.excluded || []),
                  ...(value.rtc.web_access?.coverage?.excluded || []),
                ].filter((id: string) => !oldExcluded.includes(id))
              : [];
            const affected = snapshot.runs.filter(
              (r) =>
                !["SUCCESS", "FAILED", "ERROR", "CANCELLED"].includes(
                  r.status,
                ) &&
                (target
                  ? r.target_id === source?.target_id
                  : r.task_id === (edit.value as Task | undefined)?.task_id),
            );
            if (excluded.length || affected.length) {
              const confirmed = await new Promise<boolean>((resolve) =>
                Modal.confirm({
                  className: "rtc-confirm",
                  title: excluded.length
                    ? "确认移出覆盖实例？"
                    : "保存并取消旧执行？",
                  content: excluded.length
                    ? `将把 ${excluded.length} 个实例移出覆盖。请确认它们已退役。`
                    : `配置保存会取消 ${affected.length} 个未完成的执行。`,
                  okText: "确认保存",
                  cancelText: "继续编辑",
                  onOk: () => {
                    resolve(true);
                  },
                  onCancel: () => {
                    resolve(false);
                  },
                }),
              );
              if (!confirmed) return;
            }
            setSaving(true);
            await onSave(value, revision);
          } catch (e) {
            setError(errorText(e));
          } finally {
            setSaving(false);
          }
        }}
      >
        {target ? (
          <>
            <Form.Item
              label="服务名称"
              name="name"
              rules={[
                { required: true, whitespace: true, message: "请填写服务名称" },
              ]}
            >
              <Input maxLength={128} />
            </Form.Item>
            <RTCSettings
              hasCertificate={source?.has_certificate}
              existingAppId={source?.rtc?.app_id}
              discovery={discovery}
              discoveryError={discoveryError}
              excludedOptions={[...excluded.values()]}
            />
            <details className="rtc-advanced-section">
              <summary>业务关联</summary>
              <Form.Item label="关联集群编号（可选）" name="cluster_code">
                <Input />
              </Form.Item>
            </details>
          </>
        ) : (
          <>
            <section className="rtc-form-block">
              <RTCSectionHeading title="任务与执行端" />
              <Form.Item name="task_id" hidden>
                <Input />
              </Form.Item>
              <Form.Item
                label="任务名称"
                name="name"
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    message: "请填写任务名称",
                  },
                ]}
                initialValue={
                  edit.value
                    ? (edit.value as Task).name ||
                      `${clientLabel(edit.value as Task)} 音视频探测`
                    : undefined
                }
              >
                <Input maxLength={80} placeholder="例如：生产环境 Web 探测" />
              </Form.Item>
              <Form.Item
                label="探测类型"
                name="client_type"
                rules={[{ required: true, message: "请选择客户端类型" }]}
              >
                <Radio.Group
                  optionType="button"
                  buttonStyle="solid"
                  options={[
                    { value: "native", label: "Native · Go Linux SDK" },
                    { value: "web", label: "Web · 无头浏览器" },
                    { value: "interop", label: "Native ↔ Web 互通" },
                  ]}
                  onChange={(e) => {
                    const next = e.target.value;
                    if (
                      !rtcServerOptions(snapshot.hosts, next).some(
                        (option) =>
                          option.value === form.getFieldValue("host_id") &&
                          !option.disabled,
                      )
                    )
                      form.setFieldValue("host_id", undefined);
                    if (
                      !snapshot.targets.some(
                        (target) =>
                          target.target_id ===
                            form.getFieldValue("target_id") &&
                          rtcTargetSupports(target, next),
                      )
                    )
                      form.setFieldValue("target_id", undefined);
                    form.setFieldValue(
                      "timeout_seconds",
                      e.target.value === "native" ? 30 : 90,
                    );
                  }}
                />
              </Form.Item>
              <Form.Item
                label="探测目标配置"
                name="target_id"
                rules={[{ required: true, message: "请选择探测目标配置" }]}
              >
                <Select
                  showSearch
                  optionFilterProp="label"
                  placeholder="选择当前客户端支持的探测目标配置"
                  notFoundContent="请先创建支持该客户端的探测目标配置"
                  options={snapshot.targets
                    .filter((t) => rtcTargetSupports(t, type))
                    .map((t) => ({ value: t.target_id, label: t.name }))}
                />
              </Form.Item>
              <Form.Item
                label="执行服务器"
                name="host_id"
                extra="按服务器上报的 RTC 客户端能力匹配，同服务器的 Native/Web 共享服务器身份。"
                rules={[
                  { required: true, message: "请选择上报了所需客户端的服务器" },
                ]}
              >
                <Select
                  showSearch
                  optionFilterProp="label"
                  placeholder="选择执行本任务的服务器"
                  options={serverOptions}
                  notFoundContent="暂无匹配客户端，请先部署探测客户端"
                />
              </Form.Item>
              {!serverOptions.some((option) => !option.disabled) && (
                <Alert
                  type="warning"
                  showIcon
                  message="暂无可选的执行服务器"
                  description={
                    <span>
                      部署并启动对应客户端后，服务器会自动上报。
                      <a href="/rtc/probes">查看探测服务器</a>
                    </span>
                  }
                />
              )}
            </section>
            <section className="rtc-form-block">
              <RTCSectionHeading
                title="执行间隔"
                description="各类覆盖按现有调度规则执行。"
              />
              <div className="rtc-form-grid">
                <Form.Item
                  label={
                    isInteropClient(type)
                      ? "互通完成后间隔（分钟）"
                      : coverage
                        ? "AP 接入完成后间隔（分钟）"
                        : "探测间隔（分钟）"
                  }
                  name="interval_seconds"
                  {...minuteProps}
                  rules={[{ required: true }]}
                >
                  <InputNumber min={0.5} max={60} />
                </Form.Item>
                <Form.Item
                  label="单用例超时（秒）"
                  name="timeout_seconds"
                  dependencies={["interval_seconds"]}
                  rules={[
                    { required: true },
                    ({ getFieldValue }) => ({
                      validator: (_, v) =>
                        v < getFieldValue("interval_seconds")
                          ? Promise.resolve()
                          : Promise.reject(new Error("超时需小于间隔")),
                    }),
                  ]}
                >
                  <InputNumber
                    min={type === "web" || isInteropClient(type) ? 15 : 7}
                    max={120}
                    precision={0}
                  />
                </Form.Item>
              </div>
              {coverage && !isInteropClient(type) && (
                <div className="rtc-form-grid">
                  <Form.Item
                    label="Edge 直连完成后间隔（分钟）"
                    name="sweep_interval_seconds"
                    {...minuteProps}
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={1} max={1440} />
                  </Form.Item>
                  <Form.Item
                    label="跨 Edge 互通完成后间隔（分钟）"
                    name="cross_interval_seconds"
                    {...minuteProps}
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={1} max={1440} />
                  </Form.Item>
                </div>
              )}
            </section>
            <details className="rtc-advanced-section">
              <summary>高级执行规则</summary>
              <div className="rtc-form-grid">
                <Form.Item
                  label="连续失败确认次数"
                  name="failure_threshold"
                  rules={[{ required: true }]}
                >
                  <InputNumber min={1} max={10} precision={0} />
                </Form.Item>
                <Form.Item
                  label="连续成功恢复次数"
                  name="recovery_threshold"
                  rules={[{ required: true }]}
                >
                  <InputNumber min={1} max={10} precision={0} />
                </Form.Item>
              </div>
              <Form.Item
                label="持续待验证通知等待（分钟）"
                name="unknown_notify_seconds"
                {...minuteProps}
                rules={[{ required: true }]}
              >
                <InputNumber min={0.5} max={60} />
              </Form.Item>
              <Form.Item label="任务描述" name="description">
                <Input.TextArea maxLength={1000} rows={2} />
              </Form.Item>
            </details>
          </>
        )}
        {error && (
          <div ref={errorRef} role="alert" tabIndex={-1}>
            <Alert type="error" showIcon message={error} />
          </div>
        )}
        <div className="rtc-editor-footer">
          <span className="rtc-secondary">
            {!target && !edit.value
              ? "新任务默认停用"
              : "保存后关联任务使用新配置，旧执行将取消"}
          </span>
          <Space>
            <Button onClick={close} disabled={saving}>
              取消
            </Button>
            <Button type="primary" htmlType="submit" loading={saving}>
              保存配置
            </Button>
          </Space>
        </div>
      </Form>
    </Modal>
  );
}
