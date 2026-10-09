import { useEffect, useState } from "react";
import Alert from "antd/es/alert";
import Card from "antd/es/card";
import Descriptions from "antd/es/descriptions";
import Progress from "antd/es/progress";
import Table from "antd/es/table";
import type { API } from "./api";

type Usage = {
  backend: string;
  database_bytes: number;
  warning_bytes: number;
  disk_capacity_bytes: number;
  disk_available_bytes: number;
  warnings: string[];
  tables: { name: string; bytes: number }[];
  sessions: {
    checked_at: string | null;
    deleted_sessions: number;
    error: string;
  };
};
const bytes = (n: number) =>
  n >= 1073741824
    ? `${(n / 1073741824).toFixed(2)} GiB`
    : `${(n / 1048576).toFixed(1)} MiB`;
const labels: Record<string, string> = {
  DATABASE_SIZE_WARNING: "数据库占用达到提示阈值，请备份并评估归档或扩容",
  DISK_SPACE_LOW: "数据盘可用空间不足 2 GiB 或总容量的 10%",
  DISK_USAGE_UNAVAILABLE: "无法读取数据盘容量，请检查配置路径和权限",
  SESSION_CLEANUP_FAILED: "过期会话清理失败，请检查数据库状态",
};
export default function StorageStatus({ api }: { api: API }) {
  const [usage, setUsage] = useState<Usage | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const data = await api.get<Usage>("/api/v1/storage/status");
        if (active) {
          setUsage(data);
          setError("");
        }
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "容量读取失败");
      }
    };
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 60000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [api]);
  if (usage?.backend === "file-dev") return null;
  return (
    <Card title="存储容量" className="page-card">
      {error && <Alert type="warning" showIcon message={error} />}
      {usage && (
        <>
          {usage.warnings.map((code) => (
            <Alert
              key={code}
              type="warning"
              showIcon
              message={labels[code] || code}
              className="page-alert"
            />
          ))}
          <Descriptions
            column={{ xs: 1, sm: 2, lg: 3 }}
            items={[
              {
                key: "database",
                label: "数据库占用",
                children: bytes(usage.database_bytes),
              },
              {
                key: "threshold",
                label: "容量提示阈值",
                children: bytes(usage.warning_bytes),
              },
              {
                key: "disk",
                label: "数据盘可用",
                children: usage.disk_capacity_bytes
                  ? `${bytes(usage.disk_available_bytes)} / ${bytes(usage.disk_capacity_bytes)}`
                  : "无法读取",
              },
              {
                key: "cleanup",
                label: "最近会话清理",
                children: usage.sessions.checked_at
                  ? `${new Date(usage.sessions.checked_at).toLocaleString()} · 清理 ${usage.sessions.deleted_sessions} 条`
                  : "等待首次检查",
              },
            ]}
          />
          <Progress
            percent={Math.min(
              100,
              Number(
                ((usage.database_bytes / usage.warning_bytes) * 100).toFixed(1),
              ),
            )}
            status={
              usage.database_bytes >= usage.warning_bytes
                ? "exception"
                : "normal"
            }
          />
          <p>
            容量阈值用于提示，不是硬配额。每小时清理过期超过 24
            小时的会话；通知、审计和重启幂等记录保留。
          </p>
          <Table
            size="small"
            rowKey="name"
            dataSource={usage.tables}
            pagination={{ pageSize: 5, size: "small" }}
            columns={[
              { title: "数据表", dataIndex: "name" },
              { title: "占用（含索引）", dataIndex: "bytes", render: bytes },
            ]}
          />
        </>
      )}
    </Card>
  );
}
