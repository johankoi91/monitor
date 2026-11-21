import {
  ApiOutlined,
  DeploymentUnitOutlined,
  HistoryOutlined,
  RadarChartOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import type { ReactNode } from "react";
import { useMemo, useSyncExternalStore } from "react";
import { locationKey, readLocationParams, rtcHref } from "../../app/routes";
import { visitRTC } from "./RTCLink";
import { type Snapshot, type Task, reasonLabel } from "./RTCModel";
import { CoverageBadge } from "./RTCNativeCoverage";
export function PageHeading({
  title,
  status,
  actions,
  subtitle,
}: {
  title: string;
  status?: ReactNode;
  actions?: ReactNode;
  subtitle?: string;
}) {
  const descriptions: Record<string, string> = {
    探测任务: "调度状态、覆盖证据与最近有效结果。",
    目标服务: "统一管理 Native / Web 接入与鉴权。",
    执行记录: "按客户端、任务与结果追溯每次验证。",
    探测服务器: "服务器主动接入、服务类型与客户端能力。",
    运行设置: "历史记录与诊断日志的保留策略。",
  };
  const Icon =
    title === "目标服务"
      ? ApiOutlined
      : title === "执行记录"
        ? HistoryOutlined
        : title === "探测服务器"
          ? DeploymentUnitOutlined
          : title === "运行设置"
            ? SettingOutlined
            : RadarChartOutlined;
  const description = subtitle || descriptions[title];
  return (
    <div className="workspace-page-heading rtc-page-heading">
      <div className="rtc-heading-start">
        <span className="rtc-page-icon">
          <Icon aria-hidden />
        </span>
        <div className="rtc-heading-copy">
          <div className="rtc-title">
            <h2 id="workspace-page-title" tabIndex={-1}>
              {title}
            </h2>
            {status}
          </div>
          {description && <p className="rtc-page-description">{description}</p>}
        </div>
      </div>
      <div className="rtc-actions">{actions}</div>
    </div>
  );
}
export function BusinessState({ task, data }: { task: Task; data: Snapshot }) {
  const h = data.task_health?.[task.task_id];
  return (
    <CoverageBadge value={!task.enabled ? "DISABLED" : h?.state || "UNKNOWN"} />
  );
}
export function healthReason(task: Task, data: Snapshot) {
  const h = data.task_health?.[task.task_id];
  if (!task.enabled || !h) return "";
  if (h.infrastructure_reason) return reasonLabel(h.infrastructure_reason);
  if (h.reason === "FAILURE_CONFIRMING")
    return `失败确认 ${h.failure_count}/${h.failure_threshold}`;
  if (h.reason === "RECOVERY_CONFIRMING")
    return `恢复确认 ${h.success_count}/${h.recovery_threshold}`;
  return h.state === "HEALTHY" ? "" : reasonLabel(h.reason);
}
const subscribeLocation = (notify: () => void) => {
  window.addEventListener("popstate", notify);
  return () => window.removeEventListener("popstate", notify);
};
const serverLocation = () => "/";
export function useRTCDetail() {
  const snapshot = useSyncExternalStore(
    subscribeLocation,
    locationKey,
    serverLocation,
  );
  return useMemo(() => {
    const query = snapshot.indexOf("?");
    return readLocationParams({
      pathname: query < 0 ? snapshot : snapshot.slice(0, query),
      search: query < 0 ? "" : snapshot.slice(query),
    });
  }, [snapshot]);
}
export function rtcNavigate(
  page: string,
  values: Record<string, string> = {},
  replace = false,
) {
  visitRTC(rtcHref(page, values), replace);
}
export function DetailBack({
  label = "返回上一级",
  onClick,
}: {
  label?: string;
  onClick: () => void;
}) {
  return <Button onClick={onClick}>{label}</Button>;
}
