import { ArrowRightOutlined } from "@ant-design/icons";
import type { ReactNode } from "react";
import { routeHref, type WorkspaceRoute } from "../../app/navigation";

export type OverviewTone = "neutral" | "healthy" | "warning" | "danger";
export function OverviewState({
  tone = "neutral",
  children,
}: {
  tone?: OverviewTone;
  children: ReactNode;
}) {
  return (
    <span className={`overview-state is-${tone}`}>
      <span aria-hidden="true" />
      {children}
    </span>
  );
}
export function OverviewLink({
  route,
  onNavigate,
  children,
  variant = "link",
}: {
  route: WorkspaceRoute;
  onNavigate: (route: WorkspaceRoute) => void;
  children: ReactNode;
  variant?: "link" | "primary" | "secondary";
}) {
  return (
    <a
      className={`overview-link${variant === "link" ? "" : ` is-${variant}`}`}
      href={routeHref(route)}
      onClick={(event) => {
        if (
          event.button ||
          event.metaKey ||
          event.ctrlKey ||
          event.shiftKey ||
          event.altKey
        )
          return;
        event.preventDefault();
        onNavigate(route);
      }}
    >
      {children}
      <ArrowRightOutlined aria-hidden="true" />
    </a>
  );
}
