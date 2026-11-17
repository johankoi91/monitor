import { useEffect, useState } from "react";
import Form from "antd/es/form";
import Input from "antd/es/input";
import InputNumber from "antd/es/input-number";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Modal from "antd/es/modal";
import Alert from "antd/es/alert";
import Space from "antd/es/space";
import Typography from "antd/es/typography";
import type { API } from "./api";
export interface RTCStorageSettings {
  version: string;
  history_days: number;
  history_mb: number;
  logs_mb: number;
  probe_success_minutes: number;
  probe_failure_minutes: number;
  probe_disk_mb: number;
}
export function ProbeRegistration({
  api,
  onClose,
  onSaved,
}: {
  api: API;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const [secret, setSecret] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  return (
    <Modal
      open
      title="登记探针"
      onCancel={onClose}
      footer={null}
      destroyOnClose
    >
      {secret ? (
        <>
          <Alert
            showIcon
            type="success"
            message="探针已登记，机器凭证只显示本次"
          />
          <Typography.Paragraph
            copyable
            style={{ wordBreak: "break-all", marginTop: 16 }}
          >
            {secret}
          </Typography.Paragraph>
          <Typography.Paragraph type="secondary">
            部署时设置 RTC_PROBE_ID、RTC_PROBE_SECRET 和中心 WSS
            地址。此凭证只用于该探针。
          </Typography.Paragraph>
          <Button onClick={onClose}>已保存凭证</Button>
        </>
      ) : (
        <Form
          layout="vertical"
          onFinish={async (value) => {
            setBusy(true);
            setError("");
            try {
              const result = await api.post<{ secret: string }>(
                "/api/v1/rtc/probes",
                value,
              );
              setSecret(result.secret);
              await onSaved();
            } catch (e) {
              setError(e instanceof Error ? e.message : "登记失败");
            } finally {
              setBusy(false);
            }
          }}
        >
          <Form.Item
            label="探针标识"
            name="probe_id"
            rules={[
              { required: true },
              {
                pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$/,
                message: "使用字母、数字、下划线或连字符",
              },
            ]}
          >
            <Input />
          </Form.Item>
          <Form.Item label="名称" name="name">
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item
            label="网络区域 / 位置"
            name="location"
            rules={[{ required: true }]}
          >
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item label="主机地址" name="host_address">
            <Input />
          </Form.Item>
          {error && <Alert type="error" message={error} />}
          <Space>
            <Button onClick={onClose}>取消</Button>
            <Button type="primary" htmlType="submit" loading={busy}>
              登记并生成凭证
            </Button>
          </Space>
        </Form>
      )}
    </Modal>
  );
}
export function RTCRetention({
  api,
  settings,
  onSaved,
  canManage,
  dailyRuns,
}: {
  api: API;
  settings: RTCStorageSettings;
  onSaved: () => Promise<void>;
  canManage: boolean;
  dailyRuns: number;
}) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [saved, setSaved] = useState(false);
  const [usage, setUsage] = useState<{ runs: number; bytes: number } | null>(null);
  useEffect(() => {
    let active = true;
    void api.get<{ runs: number; bytes: number }>("/api/v1/rtc/usage")
      .then(v => { if (active) setUsage(v); })
      .catch(() => { if (active) setUsage(null); });
    return () => { active = false; };
  }, [api, settings.version]);
  return (
    <Card title="历史与容量设置" size="small">
      <Typography.Paragraph type="secondary">
        执行明细按保留时间和容量清理，只处理 RTC
        探测数据。容量不足时执行状态转未知，不沿用旧正常。
        探针达到预算时会提前回收已确认日志；未确认结果会保留，并暂停新任务。
      </Typography.Paragraph>
      {usage && <Typography.Paragraph>
        已保存 {usage.runs} 轮，约 {(usage.bytes / 1048576).toFixed(2)} MiB。
        每日计划 {Math.ceil(dailyRuns)} 轮；按当前平均记录大小和 {settings.history_days} 天估算，约 {usage.runs ? (dailyRuns * settings.history_days * usage.bytes / usage.runs / 1048576).toFixed(1) : "—"} MiB（日志包另计）。
      </Typography.Paragraph>}
      <Form
        key={settings.version}
        layout="vertical"
        initialValues={settings}
        disabled={!canManage || busy}
        onFinish={async (value) => {
          setBusy(true);
          setError("");
          setSaved(false);
          try {
            await api.post("/api/v1/rtc/settings", {
              ...value,
              expected_version: settings.version,
            });
            setSaved(true);
            await onSaved();
          } catch (e) {
            setError(e instanceof Error ? e.message : "保存失败");
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="rtc-form-grid">
          <Form.Item
            name="history_days"
            label="执行明细保留（天）"
            rules={[{ required: true }]}
          >
            <InputNumber min={1} max={365} />
          </Form.Item>
          <Form.Item
            name="history_mb"
            label="历史容量预算（MiB）"
            rules={[{ required: true }]}
          >
            <InputNumber min={16} max={8192} />
          </Form.Item>
          <Form.Item
            name="logs_mb"
            label="诊断日志预算（MiB）"
            rules={[{ required: true }]}
          >
            <InputNumber min={16} max={8192} />
          </Form.Item>
          <Form.Item name="probe_success_minutes" label="探针成功日志保留（分钟）" rules={[{required:true}]}><InputNumber min={1} max={525600}/></Form.Item>
          <Form.Item name="probe_failure_minutes" label="探针失败日志保留（分钟）" rules={[{required:true}]}><InputNumber min={1} max={525600}/></Form.Item>
          <Form.Item name="probe_disk_mb" label="单探针磁盘预算（MiB）" rules={[{required:true}]}><InputNumber min={16} max={65536}/></Form.Item>
        </div>
        {error && <Alert type="error" message={error} />}{" "}
        {saved && <Alert type="success" message="设置已保存" />}
        {canManage && (
          <Button type="primary" htmlType="submit" loading={busy}>
            保存保留设置
          </Button>
        )}
      </Form>
    </Card>
  );
}
