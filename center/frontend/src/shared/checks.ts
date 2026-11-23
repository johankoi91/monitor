import type { Check } from "./types";
export function parsePorts(value: string): number[] {
  const parts = value.trim()
    ? value
        .trim()
        .split(/[\s,，;；、]+/)
        .filter(Boolean)
    : [];
  if (parts.some((p) => !/^\d+$/.test(p) || Number(p) < 1 || Number(p) > 65535))
    throw new Error("TCP 端口应为 1–65535，多个端口用逗号分隔");
  const ports = [...new Set(parts.map(Number))];
  if (ports.length > 8) throw new Error("每个容器最多配置 8 个 TCP 端口");
  return ports;
}
export function checksForPorts(value: string, existing: Check[] = []): Check[] {
  const checks = parsePorts(value).flatMap((port) => {
    const previous = existing.filter(
      (c) => c.type === "tcp" && c.port === port,
    );
    return previous.length
      ? previous.map((c) => ({ ...c }))
      : [{ type: "tcp", host: "127.0.0.1", port, timeout_ms: 2000 }];
  });
  if (checks.length > 8) throw new Error("每个容器最多配置 8 个 TCP 检查");
  return checks;
}
