import { useCallback, useEffect, useMemo, useState } from "react";

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
export type RTCPane = "tasks" | "targets" | "probes" | "history" | "retention";
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
    ...(can("monitor.read")
      ? [
          {
            id: "monitor",
            label: "监控",
            items: [
              item(
                "status",
                "",
                "容器服务",
                "按主机与分组检查容器和就绪状态；RTC 业务另行验证。",
              ),
            ],
          },
          {
            id: "rtc",
            label: "RTC 业务探测",
            items: [
              item(
                "rtc",
                "tasks",
                "探测任务",
                "任务调度、当前业务状态与覆盖证据。",
              ),
              item(
                "rtc",
                "targets",
                "目标服务",
                "管理 RTC 接入、鉴权和覆盖范围。",
              ),
              item(
                "rtc",
                "probes",
                "探针",
                "连接状态与端到端业务可用性分别判断。",
              ),
              item(
                "rtc",
                "history",
                "执行记录",
                "持久执行历史与最近轮次的证据分别查看。",
              ),
              item(
                "rtc",
                "retention",
                "历史与容量",
                "RTC 执行明细、日志和探针磁盘保留策略。",
              ),
            ],
          },
        ]
      : []),
    ...(can("inventory.read")
      ? [
          {
            id: "inventory",
            label: "容器台账",
            items: [
              item(
                "inventory",
                "discovery",
                "未纳管发现",
                "发现容器 → 设置分组与检查 → 核对变更 → 保存生效。",
              ),
              ...(can("baseline.read")
                ? [
                    item(
                      "inventory",
                      "baseline",
                      "当前基准",
                      "核对纳管成员、检查配置与重启许可；变更草稿跨页保留。",
                    ),
                  ]
                : []),
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
                      "追踪原容器重启与复查结果，业务健康仍需独立证据。",
                    ),
                  ]
                : []),
              ...(can("monitor.read")
                ? [
                    item(
                      "capacity",
                      "",
                      "中心容量",
                      "区分数据库提醒阈值、实际占用和数据盘可用空间。",
                    ),
                  ]
                : []),
            ],
          },
        ]
      : []),
    ...(canManageKeys || can("users.manage") || can("roles.manage")
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
              ...(can("roles.manage")
                ? [
                    item(
                      "accounts",
                      "roles",
                      "角色说明",
                      "查看既有固定角色的权限说明。",
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
  const params = new URLSearchParams(window.location.search);
  const aliases: Record<string, string> = {
    services: "status",
    keys: "access",
  };
  const raw =
    params.get("view") || (params.has("node") ? "status" : "overview");
  const view = aliases[raw] || raw;
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
  const url = new URL(window.location.href);
  url.searchParams.set("view", route.view);
  if (route.page) url.searchParams.set("page", route.page);
  else url.searchParams.delete("page");
  return `${url.pathname}${url.search}${url.hash}`;
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
      const href = routeHref(next);
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
    },
    [items, overview],
  );
  const current = items.find(
    (i) => i.route.view === route.view && i.route.page === route.page,
  );
  const section = sections.find((s) => s.items.some((i) => i === current));
  return { route, sections, current, section, navigate };
}
