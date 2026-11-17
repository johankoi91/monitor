import { useCallback, useEffect, useState } from "react";

const viewEvent = "monitor-view-change";

// Only presentation state belongs in the URL. Credentials and form drafts stay local.
export function useViewParam(
  key: string,
  fallback = "",
  mode: "push" | "replace" = "replace",
) {
  const read = useCallback(
    () => new URLSearchParams(location.search).get(key) ?? fallback,
    [key, fallback],
  );
  const [value, update] = useState(read);
  useEffect(() => {
    const sync = () => update(read());
    window.addEventListener("popstate", sync);
    window.addEventListener(viewEvent, sync);
    return () => {
      window.removeEventListener("popstate", sync);
      window.removeEventListener(viewEvent, sync);
    };
  }, [read]);
  const set = useCallback(
    (next: string) => {
      const url = new URL(location.href);
      if (next === fallback) url.searchParams.delete(key);
      else url.searchParams.set(key, next);
      if (url.href === location.href) return;
      window.history[mode === "push" ? "pushState" : "replaceState"](
        null,
        "",
        url,
      );
      window.dispatchEvent(new Event(viewEvent));
    },
    [key, fallback, mode],
  );
  return [value, set] as const;
}

export function useFreshness(
  lastSuccess: number,
  error: string,
  maxAge = 45000,
) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);
  return (
    !!error ||
    (!!lastSuccess && Math.max(now, Date.now()) - lastSuccess > maxAge)
  );
}
