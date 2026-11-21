import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Input from "antd/es/input";
import { useRef, useState, type FormEvent } from "react";
import { type API } from "../../shared/api";
import type { NodeStatus, Operation } from "../../shared/types";

import { errorMessage as message } from "../../shared/format";
import Modal from "../../shared/ui/Modal";
export default function RestartModal({
  node,
  api,
  onClose,
  onAccepted,
}: {
  node: NodeStatus;
  api: API;
  onClose: () => void;
  onAccepted: (o: Operation) => void;
}) {
  const requestKey = useRef(crypto.randomUUID()),
    [restartSecret, setRestartSecret] = useState(""),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (!restartSecret) throw new Error("请填写有效的重启 Token");
      const r = await api.postRestart<{ operation: Operation }>(
        "/api/v1/operations/restart",
        {
          node_id: node.node_id,
          reason,
          request_key: requestKey.current,
        },
        restartSecret,
      );
      onAccepted(r.operation);
    } catch (e) {
      setError(
        `${message(e)}；若未确认受理结果，再次提交将保留同一个请求标识。`,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title="确认重启原容器"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p>
        重启会中断该容器的连接。请确认当前服务影响；成功只表示当前检查层级复查通过。
      </p>
      <Card className="page-card">
        <strong>{node.node_id}</strong>
        <p>
          {node.host_address} · {node.container_name}
        </p>
        <small>
          {node.image} · {node.check_level}
        </small>
      </Card>
      {!node.restart_allowed && (
        <Alert
          showIcon
          type="warning"
          message={`当前暂不可重启：${node.restart_block_reason || "前置条件未满足"}`}
          className="page-alert"
        />
      )}
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="重启 Token" htmlFor="app-field-10">
          <Input.Password
            id="app-field-10"
            required
            value={restartSecret}
            autoComplete="off"
            onChange={(e) => setRestartSecret(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="重启原因" htmlFor="app-field-12">
          <Input.TextArea
            id="app-field-12"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            required
            minLength={2}
            maxLength={500}
          />
        </Form.Item>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <Button
          htmlType="submit"
          type="primary"
          disabled={busy || !node.restart_allowed}
        >
          {busy ? "正在受理…" : "确认重启"}
        </Button>
      </Form>
    </Modal>
  );
}
