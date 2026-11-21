import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Checkbox from "antd/es/checkbox";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Select from "antd/es/select";
import { useState, type FormEvent } from "react";
import { type API } from "../../shared/api";
import type { Operation } from "../../shared/types";

import { errorMessage as message } from "../../shared/format";
import Modal from "../../shared/ui/Modal";
export default function ResolveModal({
  operation,
  api,
  onClose,
  onResolved,
}: {
  operation: Operation;
  api: API;
  onClose: () => void;
  onResolved: () => void;
}) {
  const [restartSecret, setRestartSecret] = useState(""),
    [reason, setReason] = useState(""),
    [evidence, setEvidence] = useState(""),
    [outcome, setOutcome] = useState("NOT_EXECUTED"),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!checked || busy) return;
    setBusy(true);
    try {
      if (!restartSecret) throw new Error("请填写有效的重启 Token");
      await api.postRestart(
        `/api/v1/operations/${operation.operation_id}/resolve`,
        {
          reason,
          evidence,
          outcome,
        },
        restartSecret,
      );
      onResolved();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title="现场核实未知结果"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p>
        {operation.node_id} · 关闭操作锁不会执行重启，也不会改变采集健康状态。
      </p>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="重启 Token" htmlFor="app-field-14">
          <Input.Password
            id="app-field-14"
            required
            autoComplete="off"
            value={restartSecret}
            onChange={(e) => setRestartSecret(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="核实结论" htmlFor="app-field-16">
          <Select
            id="app-field-16"
            style={{ minWidth: 180, width: "100%" }}
            value={outcome}
            onChange={(value) => setOutcome(value)}
          >
            <Select.Option value="NOT_EXECUTED">确认未执行</Select.Option>
            <Select.Option value="EXECUTED">确认已执行</Select.Option>
          </Select>
        </Form.Item>
        <Form.Item label="核实原因" htmlFor="app-field-17">
          <Input
            id="app-field-17"
            required
            minLength={2}
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="现场证据" htmlFor="app-field-18">
          <Input.TextArea
            id="app-field-18"
            required
            minLength={10}
            maxLength={1000}
            value={evidence}
            onChange={(e) => setEvidence(e.target.value)}
          />
        </Form.Item>
        <Form.Item>
          <Checkbox
            checked={checked}
            onChange={(e) => setChecked(e.target.checked)}
          >
            已核查现场，不依据旧快照推测结果
          </Checkbox>
        </Form.Item>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <Button htmlType="submit" type="primary" disabled={!checked || busy}>
          记录核实并关闭操作锁
        </Button>
      </Form>
    </Modal>
  );
}
