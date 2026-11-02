import { useEffect, useState } from "react";
import Alert from "antd/es/alert";
import AutoComplete from "antd/es/auto-complete";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Checkbox from "antd/es/checkbox";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Modal from "antd/es/modal";
import Popconfirm from "antd/es/popconfirm";
import Space from "antd/es/space";
import Switch from "antd/es/switch";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import type { API } from "./api";
import type { NotificationEndpoint, NotificationSettings } from "./types";
const events = [
  { value: "NORMAL", label: "正常状态" },
  { value: "CONTAINER_ABNORMAL", label: "容器异常" },
  { value: "COLLECTION_ABNORMAL", label: "Agent 离线 / 数据过期" },
  { value: "RECOVERED", label: "恢复正常" },
];
const labels = Object.fromEntries(events.map((e) => [e.value, e.label]));
const message = (e: unknown) => (e instanceof Error ? e.message : "请求未完成");
type Draft = NotificationEndpoint & { secret: string; replaceSecret: boolean };
export default function NotificationSettingsForm({
  api,
  onSaved,
}: {
  api: API;
  onSaved: () => void;
}) {
  const [saved, setSaved] = useState<NotificationSettings | null>(null),
    [draft, setDraft] = useState<Draft | null>(null),
    [advanced, setAdvanced] = useState(false),
    [busy, setBusy] = useState(false),
    [testing, setTesting] = useState(""),
    [notice, setNotice] = useState(""),
    [error, setError] = useState(""),
    [modalError, setModalError] = useState("");
  useEffect(() => {
    void api
      .get<NotificationSettings>("/api/v1/notifications/config")
      .then(setSaved)
      .catch((e) => setError(message(e)));
  }, [api]);
  const endpoints = saved?.endpoints || [];
  function open(e?: NotificationEndpoint) {
    setModalError("");
    setDraft({
      ...(e || {
        id: "",
        enabled: true,
        url: "",
        tls_server_name: "",
        events: events.map((v) => v.value),
        auth_configured: false,
      }),
      secret: "",
      replaceSecret: false,
    });
    setAdvanced(!!e?.tls_server_name);
  }
  function request(e: NotificationEndpoint) {
    return {
      id: e.id,
      enabled: e.enabled,
      url: e.url,
      tls_server_name: e.tls_server_name,
      events: e.events,
      auth_mode: "retain",
    };
  }
  async function persist(next: ReturnType<typeof request>[]) {
    if (!saved) throw Error("请先读取通知配置");
    const value = await api.post<NotificationSettings>(
      "/api/v1/notifications/config",
      { expected_revision: saved.revision, endpoints: next },
    );
    setSaved(value);
    onSaved();
    return value;
  }
  const original = endpoints.find((e) => e.id === draft?.id);
  const needsSecret =
    !!draft &&
    (!draft.auth_configured ||
      draft.replaceSecret ||
      draft.url.trim() !== original?.url ||
      draft.tls_server_name.trim() !== original?.tls_server_name);
  async function save() {
    if (!draft || busy) return;
    if (!draft.url.trim() || !draft.events.length) {
      setModalError("请填写接收地址并至少选择一种事件");
      return;
    }
    if (needsSecret && !draft.secret) {
      setModalError("请填写签名密钥，并在接收服务中配置相同密钥");
      return;
    }
    setBusy(true);
    setModalError("");
    try {
      const item = {
        ...request(draft),
        url: draft.url.trim(),
        tls_server_name: draft.tls_server_name.trim(),
        auth_mode: needsSecret ? "set" : "retain",
        ...(needsSecret ? { secret: draft.secret } : {}),
      };
      await persist(
        draft.id
          ? endpoints.map((e) => (e.id === draft.id ? item : request(e)))
          : [...endpoints.map(request), item],
      );
      setDraft(null);
      setNotice("通知配置已保存");
      setError("");
    } catch (e) {
      setModalError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function change(next: ReturnType<typeof request>[], notice: string) {
    setBusy(true);
    try {
      await persist(next);
      setNotice(notice);
      setError("");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function test(e: NotificationEndpoint) {
    setTesting(e.id);
    try {
      const r = await api.post<{ healthy: boolean; reason: string }>(
        `/api/v1/notifications/test?target_id=${encodeURIComponent(e.id)}`,
        {},
      );
      if (!r.healthy) throw Error(`测试失败：${r.reason}`);
      setNotice("测试成功，接收端已确认收到通知");
      setError("");
      onSaved();
    } catch (e) {
      setError(message(e));
    } finally {
      setTesting("");
    }
  }
  function generate() {
    if (draft)
      setDraft({
        ...draft,
        secret: Array.from(crypto.getRandomValues(new Uint8Array(32)), (v) =>
          v.toString(16).padStart(2, "0"),
        ).join(""),
      });
  }
  async function copy() {
    if (!draft?.secret) return;
    try {
      await navigator.clipboard.writeText(draft.secret);
      setModalError("");
    } catch {
      setModalError("请点击密钥输入框的显示按钮后手动复制");
    }
  }
  return (
    <>
      <Card
        title="通知配置"
        className="page-card"
        extra={
          <Button
            type="primary"
            onClick={() => open()}
            disabled={!saved || busy}
          >
            添加配置
          </Button>
        }
      >
        {(error || notice) && (
          <Alert
            type={error ? "error" : "success"}
            message={error || notice}
            showIcon
            style={{ marginBottom: 16 }}
          />
        )}
        <Table<NotificationEndpoint>
          size="middle"
          rowKey="id"
          loading={!saved && !error}
          dataSource={endpoints}
          pagination={false}
          scroll={{ x: 850 }}
          locale={{ emptyText: "暂无通知配置，点击“添加配置”开始" }}
          columns={[
            {
              title: "接收地址",
              render: (_, e) => (
                <>
                  <Typography.Text style={{ wordBreak: "break-all" }}>
                    {e.url}
                  </Typography.Text>
                  {e.tls_server_name && (
                    <div>
                      <Typography.Text type="secondary">
                        证书域名：{e.tls_server_name}
                      </Typography.Text>
                    </div>
                  )}
                </>
              ),
            },
            {
              title: "事件类型",
              width: 310,
              render: (_, e) => (
                <Space wrap size={[0, 4]}>
                  {e.events.map((v) => (
                    <Tag key={v}>{labels[v] || v}</Tag>
                  ))}
                </Space>
              ),
            },
            {
              title: "启用",
              width: 85,
              render: (_, e) => (
                <Switch
                  size="small"
                  aria-label={`${e.url} 启用`}
                  checked={e.enabled}
                  disabled={busy}
                  onChange={(value) =>
                    void change(
                      endpoints.map((v) => ({
                        ...request(v),
                        enabled: v.id === e.id ? value : v.enabled,
                      })),
                      value ? "配置已启用" : "配置已停用",
                    )
                  }
                />
              ),
            },
            {
              title: "操作",
              width: 190,
              render: (_, e) => (
                <Space size={0}>
                  <Button type="link" onClick={() => open(e)} disabled={busy}>
                    编辑
                  </Button>
                  <Button
                    type="link"
                    onClick={() => void test(e)}
                    loading={testing === e.id}
                    disabled={busy || (!!testing && testing !== e.id)}
                  >
                    测试
                  </Button>
                  <Popconfirm
                    title="删除这条通知配置？"
                    description="删除后停止向此地址投递通知。"
                    onConfirm={() =>
                      change(
                        endpoints.filter((v) => v.id !== e.id).map(request),
                        "配置已删除",
                      )
                    }
                  >
                    <Button type="link" danger disabled={busy}>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title={draft?.id ? "编辑通知配置" : "添加通知配置"}
        open={!!draft}
        onCancel={() => {
          if (!busy) setDraft(null);
        }}
        onOk={() => void save()}
        confirmLoading={busy}
        okText="保存配置"
        cancelText="取消"
        width={600}
        destroyOnHidden
        maskClosable={!busy}
      >
        {draft && (
          <Form layout="vertical" onFinish={() => void save()}>
            {modalError && (
              <Alert
                type="error"
                message={modalError}
                showIcon
                style={{ marginBottom: 16 }}
              />
            )}
            <Form.Item label="通知接收地址" required>
              <AutoComplete
                aria-label="通知接收地址"
                value={draft.url}
                options={[...new Set(endpoints.map((e) => e.url))].map(
                  (value) => ({ value }),
                )}
                onChange={(value) => setDraft({ ...draft, url: value })}
                placeholder="选择已有地址或输入 https:// 地址"
              />
            </Form.Item>
            <Form.Item label="事件类型" required>
              <Checkbox.Group
                options={events.map((e) => ({
                  ...e,
                  label:
                    e.value === "NORMAL" ? (
                      <span title="启用时同步当前正常节点，后续恢复正常时通知；持续正常不重复发送。">
                        {e.label}
                      </span>
                    ) : (
                      e.label
                    ),
                }))}
                value={draft.events}
                onChange={(value) =>
                  setDraft({ ...draft, events: value as string[] })
                }
              />
            </Form.Item>
            <Form.Item
              label="签名密钥"
              extra="接收服务需配置相同密钥，用于校验通知来源。"
            >
              {needsSecret ? (
                <Space.Compact block>
                  <Input.Password
                    aria-label="Webhook 签名密钥"
                    autoComplete="new-password"
                    value={draft.secret}
                    onChange={(e) =>
                      setDraft({ ...draft, secret: e.target.value })
                    }
                    placeholder="填写接收服务的签名密钥"
                  />
                  <Button onClick={generate}>生成</Button>
                  <Button onClick={() => void copy()} disabled={!draft.secret}>
                    复制
                  </Button>
                </Space.Compact>
              ) : (
                <Space>
                  <Tag color="success">已配置</Tag>
                  <Button
                    type="link"
                    size="small"
                    onClick={() => setDraft({ ...draft, replaceSecret: true })}
                  >
                    更换密钥
                  </Button>
                </Space>
              )}
            </Form.Item>
            <Form.Item label="启用配置">
              <Switch
                checked={draft.enabled}
                onChange={(value) => setDraft({ ...draft, enabled: value })}
              />
            </Form.Item>
            <Button
              type="link"
              size="small"
              onClick={() => setAdvanced(!advanced)}
              style={{ padding: 0 }}
            >
              {advanced ? "收起证书域名设置" : "设置证书域名（可选）"}
            </Button>
            {advanced && (
              <Form.Item
                label="TLS 证书域名"
                extra="使用 IP 地址访问 HTTPS 服务时，填写证书对应域名。"
                style={{ marginTop: 12 }}
              >
                <Input
                  value={draft.tls_server_name}
                  onChange={(e) =>
                    setDraft({ ...draft, tls_server_name: e.target.value })
                  }
                />
              </Form.Item>
            )}
          </Form>
        )}
      </Modal>
    </>
  );
}
