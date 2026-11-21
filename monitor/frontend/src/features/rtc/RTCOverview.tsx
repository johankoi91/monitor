import {
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  PauseCircleOutlined,
  QuestionCircleOutlined,
  RadarChartOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import Skeleton from "antd/es/skeleton";
import { useEffect, useState } from "react";
import type { WorkspaceRoute } from "../../app/navigation";
import type { API } from "../../shared/api";
import {
  OverviewLink,
  OverviewState,
  type OverviewTone,
} from "../dashboard/OverviewUI";
interface Summary {
  status: string;
  configured_tasks: number;
  enabled_tasks: number;
  last_success_at?: string;
  storage_available?: boolean;
}
export function RTCOverviewPanel({
  value,
  loading = false,
  error = "",
  onNavigate,
  onRetry,
}: {
  value: Summary | null;
  loading?: boolean;
  error?: string;
  onNavigate: (route: WorkspaceRoute) => void;
  onRetry: () => void;
}) {
  const configured = !!value && value.configured_tasks > 0;
  const enabled = !!value && value.enabled_tasks > 0;
  const code = error
    ? "UNAVAILABLE"
    : value?.storage_available === false
      ? "STORAGE_UNAVAILABLE"
      : !value
        ? "UNKNOWN"
        : !configured
          ? "UNCONFIGURED"
          : !enabled
            ? "DISABLED"
            : value.status;
  const presentation: Record<
    string,
    {
      label: string;
      tone: OverviewTone;
      icon: typeof CheckCircleOutlined;
    }
  > = {
    HEALTHY: {
      label: "正常",
      tone: "healthy",
      icon: CheckCircleOutlined,
    },
    UNHEALTHY: {
      label: "异常",
      tone: "danger",
      icon: ExclamationCircleOutlined,
    },
    UNKNOWN: {
      label: "待验证",
      tone: "warning",
      icon: QuestionCircleOutlined,
    },
    UNAVAILABLE: {
      label: "读取失败",
      tone: "warning",
      icon: QuestionCircleOutlined,
    },
    STORAGE_UNAVAILABLE: {
      label: "存储不可用",
      tone: "warning",
      icon: QuestionCircleOutlined,
    },
    UNCONFIGURED: {
      label: "未配置",
      tone: "neutral",
      icon: RadarChartOutlined,
    },
    DISABLED: {
      label: "已停用",
      tone: "neutral",
      icon: PauseCircleOutlined,
    },
  };
  const state = presentation[code] || presentation.UNKNOWN;
  const Icon = state.icon;
  const success = value?.last_success_at;
  return (
    <section
      className="overview-panel overview-rtc"
      aria-labelledby="overview-rtc-title"
    >
      <div className="overview-panel-heading">
        <div>
          <span className="overview-section-icon" aria-hidden="true">
            <RadarChartOutlined />
          </span>
          <h3 id="overview-rtc-title">RTC 音视频业务</h3>
        </div>
      </div>
      {loading ? (
        <div
          className="overview-rtc-loading"
          aria-label="正在读取 RTC 业务状态"
        >
          <Skeleton active paragraph={{ rows: 3 }} />
        </div>
      ) : (
        <>
          <div
            className={`overview-rtc-result is-${state.tone}`}
            role="status"
            aria-label="RTC 业务状态"
            aria-live="polite"
            aria-atomic="true"
          >
            <span className="overview-result-icon" aria-hidden="true">
              <Icon />
            </span>
            <OverviewState tone={state.tone}>{state.label}</OverviewState>
          </div>
          {error ? (
            <div className="overview-rtc-retry">
              <Button
                icon={<ReloadOutlined aria-hidden="true" />}
                onClick={onRetry}
              >
                重新读取
              </Button>
            </div>
          ) : (
            <dl className="overview-rtc-facts">
              <div>
                <dt>启用任务</dt>
                <dd>
                  {value?.enabled_tasks ?? "—"}
                  <small>/ {value?.configured_tasks ?? "—"}</small>
                </dd>
              </div>
              <div>
                <dt>历史最近成功</dt>
                <dd
                  className="overview-last-success"
                  title={
                    success
                      ? new Date(success).toLocaleString("zh-CN", {
                          hour12: false,
                        })
                      : undefined
                  }
                >
                  {success ? (
                    <time dateTime={success}>
                      {new Date(success).toLocaleString("zh-CN", {
                        month: "2-digit",
                        day: "2-digit",
                        hour: "2-digit",
                        minute: "2-digit",
                        second: "2-digit",
                        hour12: false,
                      })}
                    </time>
                  ) : (
                    "暂无记录"
                  )}
                </dd>
              </div>
            </dl>
          )}
        </>
      )}
      <div className="overview-card-actions" aria-label="RTC 业务操作">
        <OverviewLink
          variant="primary"
          route={{ view: "rtc", page: "tasks" }}
          onNavigate={onNavigate}
        >
          探测任务
        </OverviewLink>
        <OverviewLink
          variant="secondary"
          route={{ view: "rtc", page: "history" }}
          onNavigate={onNavigate}
        >
          执行记录
        </OverviewLink>
      </div>
    </section>
  );
}
export default function RTCOverview({
  api,
  onNavigate,
  refreshVersion = 0,
}: {
  api: API;
  onNavigate: (route: WorkspaceRoute) => void;
  refreshVersion?: number;
}) {
  const [value, setValue] = useState<Summary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true,
      inFlight = false;
    setLoading(true);
    const load = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const next = await api.get<Summary>("/api/v1/rtc/summary");
        if (active) {
          setValue(next);
          setError("");
        }
      } catch {
        if (active) {
          setValue(null);
          setError("读取失败");
        }
      } finally {
        inFlight = false;
        if (active) setLoading(false);
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
  }, [api, refreshVersion, retry]);
  return (
    <RTCOverviewPanel
      value={value}
      loading={loading}
      error={error}
      onNavigate={onNavigate}
      onRetry={() => setRetry((v) => v + 1)}
    />
  );
}
