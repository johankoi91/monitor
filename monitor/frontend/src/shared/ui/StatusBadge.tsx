import Tag from "antd/es/tag";

const operationLabels: Record<string, string> = {
  ACCEPTED: "已受理",
  EXECUTE_INTENT: "执行前核验通过",
  WAIT_RESULT: "正在重启",
  VERIFY: "正在复查",
  UNCERTAIN: "结果未知，需现场核实",
  SUCCESS: "成功",
  FAILED: "失败",
  REJECTED: "已拒绝",
  TIMEOUT: "超时",
  MANUALLY_CLOSED: "已现场核实关闭",
  DELIVERED: "已送达",
  PENDING: "待发",
  INFLIGHT: "投递中",
  DROPPED: "容量丢弃",
};
export default function Badge({
  value,
  label,
}: {
  value: string;
  label?: string;
}) {
  const colors: Record<string, string> = {
    HEALTHY: "success",
    DELIVERED: "success",
    FAILED: "error",
    PENDING: "processing",
    INFLIGHT: "processing",
    RUNNING: "success",
    running: "success",
    UNHEALTHY: "error",
    DEAD: "error",
    DEGRADED: "warning",
    UNKNOWN: "default",
    CREATED: "processing",
    created: "processing",
  };
  return (
    <Tag color={colors[value] || "default"}>
      {label || operationLabels[value] || value}
    </Tag>
  );
}
