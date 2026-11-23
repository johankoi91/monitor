import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Typography from "antd/es/typography";
import { useState, type FormEvent } from "react";

import { errorMessage as message } from "../../shared/format";
export default function Login({
  onLogin,
  error,
}: {
  onLogin: (id: string, secret: string) => Promise<void>;
  error: string;
}) {
  const [id, setID] = useState(""),
    [secret, setSecret] = useState(""),
    [busy, setBusy] = useState(false),
    [localError, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (!id.trim() || id.includes(":") || !secret) {
      setError("请填写有效的账号和密码");
      return;
    }
    setBusy(true);
    setError("");
    const value = secret;
    setSecret("");
    try {
      await onLogin(id.trim(), value);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card className="page-card login-panel">
      <Typography.Title level={2}>登录监控系统</Typography.Title>
      <p>使用已开通账号登录，权限由所分配角色决定。</p>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="账号" htmlFor="app-field-1">
          <Input
            id="app-field-1"
            autoComplete="username"
            value={id}
            onChange={(e) => setID(e.target.value)}
            required
          />
        </Form.Item>
        <Form.Item label="密码" htmlFor="app-field-2">
          <Input.Password
            id="app-field-2"
            autoComplete="current-password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            required
          />
        </Form.Item>
        {(localError || error) && (
          <Alert message={localError || error} type="error" showIcon />
        )}
        <Button type="primary" htmlType="submit" disabled={busy}>
          {busy ? "登录中…" : "登录"}
        </Button>
      </Form>
    </Card>
  );
}
