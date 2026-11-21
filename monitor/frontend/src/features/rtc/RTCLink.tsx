import { ArrowRightOutlined } from "@ant-design/icons";
import Button from "antd/es/button";
import type { MouseEvent, ReactNode } from "react";

export function visitRTC(href: string, replace = false) {
  const url = new URL(href, window.location.origin);
  if (url.origin !== window.location.origin) return;
  const path = `${url.pathname}${url.search}${url.hash}`;
  if (
    path !==
    `${window.location.pathname}${window.location.search}${window.location.hash}`
  )
    window.history[replace ? "replaceState" : "pushState"](null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
  if (!replace) {
    window.scrollTo(0, 0);
    window.requestAnimationFrame(() =>
      document
        .getElementById("workspace-page-title")
        ?.focus({ preventScroll: true }),
    );
  }
}
export function followRTC(
  event: MouseEvent<HTMLElement>,
  href: string,
  onNavigate?: () => void,
) {
  if (
    event.button !== 0 ||
    event.ctrlKey ||
    event.metaKey ||
    event.shiftKey ||
    event.altKey
  )
    return;
  event.preventDefault();
  if (onNavigate) onNavigate();
  else visitRTC(href);
}
export default function RTCLink({
  href,
  children,
  onNavigate,
  disabled = false,
  className,
}: {
  href?: string;
  children: ReactNode;
  onNavigate?: () => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <Button
      type="link"
      href={disabled ? undefined : href}
      disabled={disabled}
      className={`rtc-route-button ${className || ""}`}
      icon={<ArrowRightOutlined aria-hidden />}
      iconPosition="end"
      onClick={(event) => {
        if (href && !disabled) followRTC(event, href, onNavigate);
      }}
    >
      {children}
    </Button>
  );
}
