import { useEffect, useState } from "react";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Space from "antd/es/space";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { APIError, type API } from "./api";
import { useFreshness } from "./viewState";
interface Summary {
  status: string;
  configured_tasks: number;
  enabled_tasks: number;
  last_success_at?: string;
}
export default function RTCOverview({
  api,
  onOpen,
}: {
  api: API;
  onOpen: () => void;
}) {
  const [value, setValue] = useState<Summary | null>(null);
  const [error, setError] = useState("");
  const [disabled, setDisabled] = useState(false);
  const [loadedAt, setLoadedAt] = useState(0);
  const [retry, setRetry] = useState(0);
  const stale = useFreshness(loadedAt, error);
  useEffect(() => {
    let active = true,
      running = false;
    const load = async () => {
      if (running) return;
      running = true;
      try {
        const next = await api.get<Summary>("/api/v1/rtc/summary");
        if (active) {
          setValue(next);
          setLoadedAt(Date.now());
          setError("");
          setDisabled(false);
        }
      } catch (e) {
        if (active) {
          setError(e instanceof Error ? e.message : "状态读取失败");
          setDisabled(e instanceof APIError && e.code === "RTC_PROBE_DISABLED");
        }
      } finally {
        running = false;
      }
    };
    void load();
    const timer = window.setInterval(() => {
      if (!document.hidden) void load();
    }, 15000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [api, retry]);
  const configured = value && value.configured_tasks > 0;
  const status = disabled
    ? "未接入"
    : stale
      ? "待确认"
      : !value
        ? "读取中"
        : !configured
          ? "未配置"
          : value.enabled_tasks === 0
            ? "已停用"
            : (
                {
                  HEALTHY: "正常",
                  UNHEALTHY: "异常",
                  UNKNOWN: "未知",
                } as Record<string, string>
              )[value.status] || "未知";
  return (
    <Card
      size="small"
      className="rtc-overview"
      title="RTC 业务探测"
      extra={
        <Button type="link" onClick={onOpen}>
          {!configured && !stale ? "配置探测" : "查看任务"}
        </Button>
      }
    >
      <Space wrap>
        <Tag
          color={
            stale
              ? "warning"
              : configured && value?.enabled_tasks
                ? (
                    {
                      HEALTHY: "success",
                      UNHEALTHY: "error",
                      UNKNOWN: "warning",
                    } as Record<string, string>
                  )[value.status]
                : "default"
          }
        >
          {status}
        </Tag>
        {value && (
          <span>
            {stale ? "最后快照" : "启用任务"} {value.enabled_tasks} /{" "}
            {value.configured_tasks}
          </span>
        )}
        <Typography.Text type="secondary">
          最近成功：
          {value?.last_success_at
            ? new Date(value.last_success_at).toLocaleString()
            : "暂无"}
        </Typography.Text>
        {stale && !disabled && (
          <>
            <Typography.Text type="warning">
              {error || "刷新已超时"} · 当前业务状态待确认
            </Typography.Text>
            <Button size="small" onClick={() => setRetry((v) => v + 1)}>
              重试
            </Button>
          </>
        )}
      </Space>
    </Card>
  );
}
