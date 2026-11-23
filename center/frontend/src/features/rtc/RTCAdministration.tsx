import { DatabaseOutlined, HddOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Form from "antd/es/form";
import InputNumber from "antd/es/input-number";
import Modal from "antd/es/modal";
import Space from "antd/es/space";
import { useEffect, useState } from "react";
import type { API } from "../../shared/api";
import { RTCSectionHeading } from "./RTCVisual";
export interface RTCStorageSettings {
  version: string;
  history_days: number;
  history_mb: number;
  logs_mb: number;
  probe_success_minutes: number;
  probe_failure_minutes: number;
  probe_disk_mb: number;
}
const unit = (scale: number) => ({
  getValueProps: (v?: number) => ({
    value: v === undefined ? undefined : v / scale,
  }),
  normalize: (v: number | null) =>
    v === null ? undefined : Math.round(v * scale),
});
export function RTCRetention({
  api,
  settings,
  onSaved,
  canManage,
}: {
  api: API;
  settings: RTCStorageSettings;
  onSaved: () => Promise<void>;
  canManage: boolean;
  dailyRuns?: number;
}) {
  const [form] = Form.useForm(),
    [baseline, setBaseline] = useState(settings),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [saved, setSaved] = useState(false),
    [usage, setUsage] = useState<{ runs: number; bytes: number } | null>(null);
  useEffect(() => {
    if (!form.isFieldsTouched()) {
      setBaseline(settings);
      form.setFieldsValue(settings);
    }
  }, [settings, form]);
  useEffect(() => {
    let live = true;
    void api
      .get<{ runs: number; bytes: number }>("/api/v1/rtc/usage")
      .then((v) => {
        if (live) setUsage(v);
      })
      .catch(() => {
        if (live) setUsage(null);
      });
    return () => {
      live = false;
    };
  }, [api, settings.version]);
  const save = async (value: RTCStorageSettings) => {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      await api.post("/api/v1/rtc/settings", {
        ...value,
        expected_version: baseline.version,
      });
      form.setFields(
        Object.keys(value).map((name) => ({ name, touched: false })),
      );
      await onSaved();
      setSaved(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="rtc-panel rtc-pad rtc-retention-panel">
      <RTCSectionHeading
        title="数据保留策略"
        description="中心历史与探针本地诊断分别保留，保存前可核对变更。"
        icon={<DatabaseOutlined />}
      />
      <Form
        form={form}
        layout="vertical"
        initialValues={settings}
        disabled={!canManage || busy}
        onValuesChange={() => setSaved(false)}
        onFinish={(value) => {
          if (
            value.history_days < baseline.history_days ||
            value.history_mb < baseline.history_mb ||
            value.logs_mb < baseline.logs_mb ||
            value.probe_disk_mb < baseline.probe_disk_mb ||
            value.probe_success_minutes < baseline.probe_success_minutes ||
            value.probe_failure_minutes < baseline.probe_failure_minutes
          )
            Modal.confirm({
              className: "rtc-confirm",
              title: "确认缩短数据保留？",
              content: "超出新时间或预算的 RTC 数据可能在后续清理中被回收。",
              okText: "保存策略",
              cancelText: "取消",
              onOk: () => save(value),
            });
          else void save(value);
        }}
      >
        <div className="rtc-two-grid rtc-retention-grid">
          <section className="rtc-retention-section">
            <RTCSectionHeading title="中心数据" icon={<DatabaseOutlined />} />
            <Form.Item
              name="history_days"
              label="执行记录保留（天）"
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={365} precision={0} />
            </Form.Item>
            <div className="rtc-form-grid">
              <Form.Item
                name="history_mb"
                label="历史记录预算（GiB）"
                {...unit(1024)}
                rules={[{ required: true }]}
              >
                <InputNumber min={16 / 1024} max={8} />
              </Form.Item>
              <Form.Item
                name="logs_mb"
                label="诊断包预算（GiB）"
                {...unit(1024)}
                rules={[{ required: true }]}
              >
                <InputNumber min={16 / 1024} max={8} />
              </Form.Item>
            </div>
            <span className="rtc-secondary">
              {usage
                ? `已存 ${usage.runs} 条记录 · 历史 ${(usage.bytes / 1048576).toFixed(1)} MiB / ${settings.history_mb / 1024} GiB`
                : "用量暂不可读"}
            </span>
          </section>
          <section className="rtc-retention-section">
            <RTCSectionHeading title="探针本地" icon={<HddOutlined />} />
            <div className="rtc-form-grid">
              <Form.Item
                name="probe_success_minutes"
                label="成功日志保留（小时）"
                {...unit(60)}
                rules={[{ required: true }]}
              >
                <InputNumber min={1 / 60} max={8760} />
              </Form.Item>
              <Form.Item
                name="probe_failure_minutes"
                label="失败日志保留（小时）"
                {...unit(60)}
                rules={[{ required: true }]}
              >
                <InputNumber min={1 / 60} max={8760} />
              </Form.Item>
            </div>
            <Form.Item
              name="probe_disk_mb"
              label="单探针磁盘预算（GiB）"
              {...unit(1024)}
              rules={[{ required: true }]}
            >
              <InputNumber min={16 / 1024} max={64} />
            </Form.Item>
          </section>
        </div>
        {error && <Alert type="error" showIcon message={error} />}
        {saved && <Alert type="success" showIcon message="保留策略已保存" />}
        {canManage && (
          <div className="rtc-editor-footer">
            <span />
            <Space>
              <Button
                onClick={() => {
                  form.setFieldsValue(baseline);
                  setError("");
                  setSaved(false);
                }}
              >
                还原
              </Button>
              <Button type="primary" htmlType="submit" loading={busy}>
                保存策略
              </Button>
            </Space>
          </div>
        )}
      </Form>
    </div>
  );
}
