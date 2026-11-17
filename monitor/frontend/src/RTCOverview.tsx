import { useEffect, useState } from "react";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Space from "antd/es/space";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import type { API } from "./api";
interface Summary {
  status: string;
  configured_tasks: number;
  enabled_tasks: number;
  last_success_at?: string;
}
export default function RTCOverview({ api, onOpen }: { api: API; onOpen: () => void }) {
  const [value, setValue] = useState<Summary | null>(null);
  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const next = await api.get<Summary>("/api/v1/rtc/summary");
        if (active) setValue(next);
      } catch {
        if (active) setValue(null);
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 15000);
    return () => { active = false; window.clearInterval(timer); };
  }, [api]);
  const configured = value && value.configured_tasks > 0;
  const status = !value ? "未知" : !configured ? "未配置" : value.enabled_tasks === 0 ? "已停用" : ({ HEALTHY: "正常", UNHEALTHY: "异常", UNKNOWN: "未知" } as Record<string, string>)[value.status] || "未知";
  return (
    <Card size="small" title="RTC 音视频业务探测" style={{ marginBottom: 16 }} extra={<Button type="link" onClick={onOpen}>管理与执行记录</Button>}>
      <Space wrap>
        <Tag color={configured && value?.enabled_tasks ? ({ HEALTHY: "green", UNHEALTHY: "red", UNKNOWN: "orange" } as Record<string, string>)[value.status] : "default"}>{status}</Tag>
        {value && <span>启用任务 {value.enabled_tasks} / {value.configured_tasks}</span>}
        <Typography.Text type="secondary">最近成功：{value?.last_success_at ? new Date(value.last_success_at).toLocaleString() : "暂无"}</Typography.Text>
      </Space>
    </Card>
  );
}
