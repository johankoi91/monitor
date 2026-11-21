import { useCallback, useEffect, useMemo, useState } from "react";
import { canonicalHref, readLocationParams, workspaceHref } from "./routes";

export type WorkspaceView =
  | "overview"
  | "status"
  | "rtc"
  | "inventory"
  | "notifications"
  | "operations"
  | "capacity"
  | "access"
  | "accounts";
export type RTCPane = "tasks" | "targets" | "probes" | "history" | "settings";
export interface WorkspaceRoute {
  view: WorkspaceView;
  page: string;
}
export interface NavItem {
  id: string;
  label: string;
  description: string;
  route: WorkspaceRoute;
}
export interface NavSection {
  id: string;
  label: string;
  items: NavItem[];
}
const item = (
  view: WorkspaceView,
  page: string,
  label: string,
  description: string,
): NavItem => ({
  id: `${view}:${page}`,
  label,
  description,
  route: { view, page },
});

export function navigationSections(
  permissions: string[],
  canManageKeys: boolean,
): NavSection[] {
  const can = (p: string) => permissions.includes(p);
  return [
    ...(can("monitor.read") || can("inventory.read")
      ? [
          {
            id: "monitor",
            label: "监控",
            items: [
              ...(can("monitor.read")
                ? [item("status", "", "容器服务", "")]
                : []),
              ...(can("inventory.read")
                ? [item("inventory", "discovery", "部署服务容器发现", "")]
                : []),
            ],
          },
        ]
      : []),
    ...(can("monitor.read")
      ? [
          {
            id: "rtc",
            label: "服务自主探测器",
            items: [
              item("rtc", "tasks", "探测任务", ""),
              item("rtc", "targets", "目标服务", ""),
              item("rtc", "history", "执行记录", ""),
              item("rtc", "settings", "运行设置", ""),
            ],
          },
        ]
      : []),
    ...(can("monitor.read")
      ? [
          {
            id: "notifications",
            label: "通知投递",
            items: [
              item(
                "notifications",
                "delivery",
                "投递记录",
                "查看累计投递统计与最近事件，失败可按结果筛选。",
              ),
              ...(can("notifications.write")
                ? [
                    item(
                      "notifications",
                      "config",
                      "接收配置",
                      "管理接收目标、订阅事件、签名和 TLS 设置。",
                    ),
                  ]
                : []),
            ],
          },
        ]
      : []),
    ...(can("operations.read") || can("restart.result") || can("monitor.read")
      ? [
          {
            id: "operations",
            label: "运维",
            items: [
              ...(can("operations.read") || can("restart.result")
                ? [
                    item(
                      "operations",
                      "",
                      can("operations.read") ? "操作与审计" : "我的重启",
                      "",
                    ),
                  ]
                : []),
              ...(can("monitor.read")
                ? [item("capacity", "", "中心容量", "")]
                : []),
            ],
          },
        ]
      : []),
    ...(canManageKeys || can("users.manage")
      ? [
          {
            id: "management",
            label: "管理",
            items: [
              ...(canManageKeys
                ? [
                    item(
                      "access",
                      "",
                      "重启 Token",
                      "签发、轮换与停用独立操作凭证；新值仅展示一次。",
                    ),
                  ]
                : []),
              ...(can("users.manage")
                ? [
                    item(
                      "accounts",
                      "users",
                      "账号管理",
                      "审核账号状态和角色，保留至少一名管理者。",
                    ),
                  ]
                : []),
            ],
          },
        ]
      : []),
  ];
}

function readRoute(items: NavItem[], overview: boolean): WorkspaceRoute {
  const params = readLocationParams();
  const aliases: Record<string, string> = {
    services: "status",
    keys: "access",
  };
  const raw =
    params.get("view") || (params.has("node") ? "status" : "overview");
  const view = aliases[raw] || raw;
  if (view === "rtc" && params.get("page") === "retention")
    params.set("page", "settings");
  if (view === "overview" && overview) return { view: "overview", page: "" };
  return (
    (
      items.find(
        (i) =>
          i.route.view === view && i.route.page === (params.get("page") || ""),
      ) || items.find((i) => i.route.view === view)
    )?.route ||
    (overview
      ? { view: "overview", page: "" }
      : items[0]?.route || { view: "overview", page: "" })
  );
}
export function routeHref(route: WorkspaceRoute) {
  return workspaceHref(route);
}
export function useWorkspaceNavigation(
  permissions: string[],
  canManageKeys: boolean,
) {
  const sections = useMemo(
    () => navigationSections(permissions, canManageKeys),
    [permissions, canManageKeys],
  );
  const items = useMemo(() => sections.flatMap((s) => s.items), [sections]);
  const overview = permissions.includes("monitor.read");
  const [route, setRoute] = useState(() => readRoute(items, overview));
  useEffect(() => {
    const restore = () => {
      const next = readRoute(items, overview);
      const href = canonicalHref(next);
      if (
        href !==
        `${window.location.pathname}${window.location.search}${window.location.hash}`
      )
        window.history.replaceState(null, "", href);
      setRoute(next);
    };
    restore();
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, [items, overview]);
  const navigate = useCallback(
    (next: WorkspaceRoute) => {
      const allowed =
        next.view === "overview"
          ? overview
          : items.some(
              (i) => i.route.view === next.view && i.route.page === next.page,
            );
      if (!allowed) return;
      const href = routeHref(next);
      if (
        href !==
        `${window.location.pathname}${window.location.search}${window.location.hash}`
      )
        window.history.pushState(null, "", href);
      setRoute(next);
      window.dispatchEvent(new PopStateEvent("popstate"));
    },
    [items, overview],
  );
  const current = items.find(
    (i) => i.route.view === route.view && i.route.page === route.page,
  );
  const section = sections.find((s) => s.items.some((i) => i === current));
  return { route, sections, current, section, navigate };
}
