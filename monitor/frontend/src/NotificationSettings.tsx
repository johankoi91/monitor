import Form from "antd/es/form";
import Radio from "antd/es/radio";
import Select from "antd/es/select";
import { useEffect, useState, type FormEvent } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Checkbox from "antd/es/checkbox";
import Switch from "antd/es/switch";
import Divider from "antd/es/divider";
import Input from "antd/es/input";
import Space from "antd/es/space";
import Typography from "antd/es/typography";
import type { API } from "./api";
import type { NotificationSettings } from "./types";
const message = (error: unknown) =>
  error instanceof Error ? error.message : "请求未完成";
export default function NotificationSettingsForm({
  api,
  onSaved,
}: {
  api: API;
  onSaved: () => void;
}) {
  const [saved, setSaved] = useState<NotificationSettings | null>(null),
    [enabled, setEnabled] = useState(false),
    [url, setURL] = useState(""),
    [tls, setTLS] = useState(""),
    [secret, setSecret] = useState(""),
    [clearAuth, setClearAuth] = useState(false),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState(""),
    [testing, setTesting] = useState(false);
  function apply(value: NotificationSettings) {
    setSaved(value);
    setEnabled(value.enabled);
    setURL(value.url);
    setTLS(value.tls_server_name);
    setSecret("");
    setClearAuth(false);
  }
  async function load() {
    try {
      apply(
        await api.get<NotificationSettings>("/api/v1/notifications/config"),
      );
      setNotice("");
    } catch (error) {
      setNotice(message(error));
    }
  }
  useEffect(() => {
    void load();
  }, [api]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!saved || busy) return;
    setBusy(true);
    setNotice("");
    try {
      const endpoint = url.trim(),
        serverName = endpoint.startsWith("http://") ? "" : tls.trim();
      const targetChanged =
        endpoint !== saved.url || serverName !== saved.tls_server_name;
      const mode = clearAuth
        ? "clear"
        : secret
          ? "set"
          : targetChanged
            ? "clear"
            : "retain";
      const settings = await api.post<NotificationSettings>(
        "/api/v1/notifications/config",
        {
          expected_revision: saved.revision,
          enabled,
          url: endpoint,
          tls_server_name: serverName,
          auth_mode: mode,
          ...(mode === "set" ? { secret } : {}),
        },
      );
      apply(settings);
      setNotice(
        settings.storage_available
          ? "通知配置已保存并立即生效"
          : "通知配置已保存，通知存储异常，投递已暂停",
      );
      onSaved();
    } catch (error) {
      setNotice(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function testWebhook() {
    setTesting(true);
    try {
      const result = await api.post<{
        healthy: boolean;
        http_status: number;
        reason: string;
      }>("/api/v1/notifications/test", {});
      setNotice(
        result.healthy
          ? "健康检查通过：接收端已验证签名并返回 HTTP 200 和 JSON"
          : `健康检查未通过：${result.reason}（HTTP ${result.http_status || "连接失败"}）`,
      );
      onSaved();
    } catch (error) {
      setNotice(message(error));
    } finally {
      setTesting(false);
    }
  }
  function generateSigningSecret() {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    setSecret(
      Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join(""),
    );
    setClearAuth(false);
    setNotice(
      "已生成新的签名密钥。请复制到接收端配置，再保存通知设置；原密钥在保存前仍有效。",
    );
  }
  async function copySigningSecret() {
    try {
      await navigator.clipboard.writeText(secret);
      setNotice("签名密钥已复制，请在接收端配置同一密钥。");
    } catch {
      setNotice("浏览器未允许复制，可使用密钥输入框显示按钮后手动复制。");
    }
  }
  return (
    <Card title="通知接收配置" className="page-card">
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="状态通知">
          <Switch
            checked={enabled}
            onChange={setEnabled}
            checkedChildren="已启用"
            unCheckedChildren="已关闭"
            disabled={!saved || busy}
          />
        </Form.Item>
        <Form.Item
          label="Webhook 接收地址（仅 HTTPS）"
          htmlFor="notificationsettings-field-1"
        >
          <Input
            id="notificationsettings-field-1"
            type="url"
            value={url}
            required={enabled}
            placeholder="https://example.com/api/notifications"
            onChange={(e) => {
              setURL(e.target.value);
              if (e.target.value.startsWith("http://")) setTLS("");
            }}
            disabled={!saved || busy}
          />
        </Form.Item>
        <Form.Item
          label="HTTPS 校验域名（可选）"
          htmlFor="notificationsettings-field-2"
        >
          <Input
            id="notificationsettings-field-2"
            value={tls}
            placeholder="使用 IP 地址且证书为域名时填写"
            onChange={(e) => setTLS(e.target.value)}
            disabled={!saved || busy}
          />
        </Form.Item>
        <Divider orientation="left">Webhook 签名校验</Divider>
        <Alert
          type="info"
          showIcon
          message="HTTPS POST · HMAC-SHA256 / SHA1"
          description="使用原始请求体生成 Agora-Signature 和 Agora-Signature-V2，接收端必须在 10 秒内返回 HTTP 200 和 JSON，失败最多重试 3 次。"
          className="page-alert"
        />
        <div className="form-grid">
          <Form.Item
            label="Webhook 签名密钥"
            htmlFor="notificationsettings-field-4"
          >
            <Input.Password
              id="notificationsettings-field-4"
              value={secret}
              autoComplete="off"
              onChange={(e) => setSecret(e.target.value)}
              disabled={!saved || busy || clearAuth}
            />
          </Form.Item>
          <Space>
            <Button onClick={generateSigningSecret} disabled={!saved || busy}>
              生成签名密钥
            </Button>
            <Button onClick={() => void copySigningSecret()} disabled={!secret}>
              复制新密钥
            </Button>
          </Space>
        </div>
        <Typography.Paragraph>
          {saved?.auth_configured
            ? "当前已配置 Webhook 签名密钥。"
            : "当前未配置 Webhook 签名密钥。"}
          地址不变且密钥留空时保留现有签名密钥；更换目标须重新填写。启用或变更目标时必须通过签名健康检查。
        </Typography.Paragraph>
        {saved?.auth_configured && (
          <Checkbox
            checked={clearAuth}
            onChange={(e) => setClearAuth(e.target.checked)}
            disabled={busy}
          >
            清除签名密钥（需先关闭状态通知）
          </Checkbox>
        )}
        <Typography.Paragraph type="secondary">
          保存后新事件投递到新目标，旧配置的待发事件停止重试。
        </Typography.Paragraph>
        {notice && (
          <Alert
            message={notice}
            type={
              notice.includes("失败") || notice.includes("异常")
                ? "warning"
                : "success"
            }
            showIcon
          />
        )}
        <Space className="actions">
          <Button
            onClick={() => void testWebhook()}
            loading={testing}
            disabled={busy || !saved?.auth_configured}
          >
            测试已保存的 Webhook
          </Button>
          <Button htmlType="button" onClick={() => void load()} disabled={busy}>
            重新读取配置
          </Button>
          <Button type="primary" htmlType="submit" disabled={!saved || busy}>
            {busy ? "正在保存…" : "保存通知配置"}
          </Button>
        </Space>
      </Form>
    </Card>
  );
}
