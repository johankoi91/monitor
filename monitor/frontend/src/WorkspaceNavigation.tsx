import { useEffect, useState, type MouseEvent } from "react";
import {
  DashboardOutlined,
  CloudServerOutlined,
  RadarChartOutlined,
  UnorderedListOutlined,
  BellOutlined,
  ToolOutlined,
  SettingOutlined,
  RightOutlined,
  DownOutlined,
  MenuOutlined,
} from "@ant-design/icons";
import { routeHref, type NavSection, type WorkspaceRoute } from "./navigation";
const icons = {
  monitor: CloudServerOutlined,
  rtc: RadarChartOutlined,
  inventory: UnorderedListOutlined,
  notifications: BellOutlined,
  operations: ToolOutlined,
  management: SettingOutlined,
};
const storageKey = "avops-workspace-sections-v1";
function readExpanded(): Record<string, boolean> {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey) || "{}");
    return value && typeof value === "object" && !Array.isArray(value)
      ? value
      : {};
  } catch {
    return {};
  }
}
export default function WorkspaceNavigation({
  sections,
  route,
  activeSection,
  canOverview,
  onNavigate,
}: {
  sections: NavSection[];
  route: WorkspaceRoute;
  activeSection?: string;
  canOverview: boolean;
  onNavigate: (route: WorkspaceRoute) => void;
}) {
  const [expanded, setExpanded] = useState(readExpanded);
  const [mobile, setMobile] = useState(
    () => window.matchMedia("(max-width: 639px)").matches,
  );
  const [mobileOpen, setMobileOpen] = useState(false);
  const currentLabel =
    sections
      .flatMap((section) => section.items)
      .find(
        (item) =>
          item.route.view === route.view && item.route.page === route.page,
      )?.label || "运行总览";
  useEffect(() => {
    const media = window.matchMedia("(max-width: 639px)");
    const update = () => setMobile(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (activeSection)
      setExpanded((old) => ({ ...old, [activeSection]: true }));
  }, [activeSection, route.view, route.page]);
  const toggle = (id: string) =>
    setExpanded((old) => {
      const next = { ...old, [id]: !old[id] };
      try {
        localStorage.setItem(storageKey, JSON.stringify(next));
      } catch {
        /* Navigation remains usable without storage. */
      }
      return next;
    });
  const follow = (
    event: MouseEvent<HTMLAnchorElement>,
    next: WorkspaceRoute,
  ) => {
    if (
      event.button ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    onNavigate(next);
    if (mobile) {
      setMobileOpen(false);
      requestAnimationFrame(() =>
        document.getElementById("workspace-page-title")?.focus(),
      );
    }
  };
  return (
    <aside className="workspace-sidebar">
      <details
        className="workspace-nav-disclosure"
        open={!mobile || mobileOpen}
      >
        <summary
          className="workspace-mobile-nav-toggle"
          onClick={(event) => {
            event.preventDefault();
            if (mobile) setMobileOpen((open) => !open);
          }}
        >
          <MenuOutlined aria-hidden />
          <span>导航 · {currentLabel}</span>
          <DownOutlined aria-hidden />
        </summary>
        <nav aria-label="工作台导航">
          {canOverview && (
            <a
              className="workspace-nav-link workspace-home-link"
              href={routeHref({ view: "overview", page: "" })}
              onClick={(e) => follow(e, { view: "overview", page: "" })}
              aria-current={route.view === "overview" ? "page" : undefined}
            >
              <DashboardOutlined aria-hidden />
              <span>运行总览</span>
            </a>
          )}
          {sections.map((section) => {
            const Icon = icons[section.id as keyof typeof icons];
            return (
              <div key={section.id} className="workspace-nav-section">
                <button
                  type="button"
                  className="workspace-nav-heading"
                  aria-expanded={!!expanded[section.id]}
                  aria-controls={`workspace-nav-${section.id}`}
                  data-active={section.id === activeSection || undefined}
                  onClick={() => toggle(section.id)}
                >
                  <Icon aria-hidden />
                  <span>{section.label}</span>
                  {expanded[section.id] ? (
                    <DownOutlined aria-hidden />
                  ) : (
                    <RightOutlined aria-hidden />
                  )}
                </button>
                <div
                  id={`workspace-nav-${section.id}`}
                  hidden={!expanded[section.id]}
                >
                  {section.items.map((item) => (
                    <a
                      key={item.id}
                      className="workspace-nav-link"
                      href={routeHref(item.route)}
                      onClick={(e) => follow(e, item.route)}
                      aria-current={
                        item.route.view === route.view &&
                        item.route.page === route.page
                          ? "page"
                          : undefined
                      }
                    >
                      {item.label}
                    </a>
                  ))}
                </div>
              </div>
            );
          })}
        </nav>
      </details>
      <p className="workspace-sidebar-note">
        容器状态与 RTC 业务
        <br />
        分别判断
      </p>
    </aside>
  );
}
