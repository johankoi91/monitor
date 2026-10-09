import type { ReactNode } from "react";
import Card from "antd/es/card";
import Descriptions from "antd/es/descriptions";
import Progress from "antd/es/progress";
import Tag from "antd/es/tag";

export function MetricIcon({
  name,
}: {
  name: "cpu" | "memory" | "disk" | "clock" | "network" | "lock" | "chevron";
}) {
  const paths: Record<string, ReactNode> = {
    cpu: (
      <>
        <rect x="6" y="6" width="12" height="12" rx="2" />
        <rect x="9" y="9" width="6" height="6" rx="1" />
        <path d="M9 3v3m6-3v3M9 18v3m6-3v3M3 9h3m-3 6h3m12-6h3m-3 6h3" />
      </>
    ),
    memory: (
      <>
        <rect x="3" y="7" width="18" height="10" rx="2" />
        <path d="M7 10v4m5-4v4m5-4v4M7 17v3m5-3v3m5-3v3" />
      </>
    ),
    disk: (
      <>
        <ellipse cx="12" cy="5" rx="8" ry="3" />
        <path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7" />
      </>
    ),
    clock: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </>
    ),
    network: (
      <>
        <rect x="8" y="3" width="8" height="5" rx="1" />
        <path d="M12 8v5m-7 4v-4h14v4" />
        <rect x="2" y="17" width="6" height="4" rx="1" />
        <rect x="16" y="17" width="6" height="4" rx="1" />
      </>
    ),
    lock: (
      <>
        <rect x="5" y="10" width="14" height="11" rx="2" />
        <path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3" />
      </>
    ),
    chevron: <path d="m9 5 7 7-7 7" />,
  };
  return (
    <svg
      className="metric-icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}

export function MetricRow({
  label,
  value,
  muted = false,
}: {
  label: string;
  value: ReactNode;
  muted?: boolean;
}) {
  return (
    <Descriptions
      size="small"
      column={1}
      items={[{ key: label, label, children: value }]}
      styles={{
        label: { color: "#8c8c8c" },
        content: {
          justifyContent: "flex-end",
          color: muted ? "#8c8c8c" : undefined,
        },
      }}
    />
  );
}
export function MetricPanel({
  title,
  icon,
  tag,
  children,
  note,
}: {
  title: string;
  icon: Parameters<typeof MetricIcon>[0]["name"];
  tag?: string;
  children: ReactNode;
  note?: ReactNode;
}) {
  return (
    <Card
      size="small"
      className="metric-panel"
      title={
        <>
          <MetricIcon name={icon} /> {title}
        </>
      }
      extra={tag && <Tag>{tag}</Tag>}
    >
      {children}
      {note && <p className="metric-panel-note">{note}</p>}
    </Card>
  );
}
export function Meter({
  ratio,
  label,
}: {
  ratio?: number | null;
  label: string;
}) {
  if (ratio == null) return null;
  return (
    <Progress
      aria-label={label}
      percent={Math.min(100, Math.max(0, ratio * 100))}
      showInfo={false}
      size="small"
      strokeColor={
        ratio >= 0.9 ? "#ff4d4f" : ratio >= 0.8 ? "#faad14" : "#1677ff"
      }
    />
  );
}
