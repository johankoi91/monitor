import { SafetyCertificateOutlined, SettingOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Checkbox from "antd/es/checkbox";
import Form from "antd/es/form";
import Input from "antd/es/input";
import { useState, type FormEvent } from "react";
import { type API } from "../../shared/api";
import { checksForPorts } from "../../shared/checks";
import type { Baseline, Node } from "../../shared/types";
import { InventoryIdentity, InventoryStep } from "../inventory/InventoryVisual";

import { errorMessage as message } from "../../shared/format";
import Modal from "../../shared/ui/Modal";
export default function NodeSettingsModal({
  api,
  node,
  onClose,
  onSaved,
  onRemove,
}: {
  api: API;
  node: Node;
  onClose: () => void;
  onSaved: (b: Baseline) => void;
  onRemove: () => void;
}) {
  const [ports, setPorts] = useState(
      [...new Set(node.checks.map((c) => c.port))].join(", "),
    ),
    [restartEnabled, setRestartEnabled] = useState(node.restart_enabled),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      let baseline = await api.get<Baseline>("/api/v1/baseline");
      const checks = checksForPorts(ports, node.checks);
      if (JSON.stringify(checks) !== JSON.stringify(node.checks)) {
        baseline = await api.post<Baseline>("/api/v1/baseline/checks", {
          expected_revision: baseline.revision,
          node_id: node.id,
          checks,
        });
      }
      if (restartEnabled !== node.restart_enabled) {
        baseline = await api.post<Baseline>("/api/v1/baseline/restart-policy", {
          expected_revision: baseline.revision,
          node_id: node.id,
          enabled: restartEnabled,
        });
      }
      onSaved(baseline);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={
        <h2 className="inventory-dialog-title">
          <SettingOutlined aria-hidden />
          节点设置
        </h2>
      }
      className="inventory-dialog inventory-settings-dialog"
      width={620}
      onClose={() => !busy && onClose()}
    >
      <div className="inventory-settings-identity">
        <InventoryIdentity
          name={node.container_name}
          host={node.host_address}
        />
      </div>
      <Form layout="vertical" onSubmitCapture={submit}>
        <section className="inventory-setting-section">
          <InventoryStep number="01" title="就绪检查" />
          <Form.Item
            label="TCP 端口与业务探测"
            htmlFor="app-field-8"
            extra="多个端口用逗号分隔；留空时仅使用容器状态和已有 Healthcheck。"
          >
            <Input
              id="app-field-8"
              autoFocus
              value={ports}
              placeholder="例如 443, 8002, 8003"
              onChange={(e) => setPorts(e.target.value)}
            />
          </Form.Item>
        </section>
        <section className="inventory-setting-section">
          <InventoryStep number="02" title="重启许可" />
          <div className="inventory-restart-permission">
            <SafetyCertificateOutlined aria-hidden />
            <div>
              <Checkbox
                checked={restartEnabled}
                onChange={(e) => setRestartEnabled(e.target.checked)}
              >
                开放该节点的重启权限（仍需独立重启 Token）
              </Checkbox>
              <p>节点许可与独立 Token 同时生效，沿用现有重启规则。</p>
            </div>
          </div>
        </section>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <div className="inventory-setting-submit">
          <span>保存设置后立即生效</span>
          <Button
            htmlType="submit"
            type="primary"
            disabled={busy}
            loading={busy}
          >
            {busy ? "正在保存…" : "保存设置"}
          </Button>
        </div>
        <div className="inventory-remove-area">
          <div>
            <strong>移出台账</strong>
            <p>加入待保存变更，不会删除或停止容器。</p>
          </div>
          <Button htmlType="button" danger disabled={busy} onClick={onRemove}>
            移出台账
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
