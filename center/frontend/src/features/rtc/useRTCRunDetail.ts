import { useEffect, useMemo, useState } from "react";
import type { API } from "../../shared/api";
import { errorText, terminal, type Run } from "./RTCModel";

// Keep navigation within the RTC screen responsive without retaining an
// unbounded history or sharing evidence across account/API sessions.
export default function useRTCRunDetail(
  api: API,
  id: string | null,
  retry: number,
  fallback?: Run,
) {
  const cache = useMemo(() => new Map<string, Run>(), [api]);
  const pending = useMemo(
    () => new Map<string, { retry: number; request: Promise<Run> }>(),
    [api],
  );
  const [detail, setDetail] = useState<Run | null>(null);
  const [failure, setFailure] = useState<{
    id: string;
    message: string;
    missing: boolean;
  } | null>(null);

  useEffect(() => {
    if (!id) return;
    let live = true;
    let timer: ReturnType<typeof setTimeout>;
    setFailure(null);
    setDetail(cache.get(id) || null);

    function request(): Promise<Run> {
      const existing = pending.get(id!);
      if (existing?.retry === retry) return existing.request;
      const value: Promise<Run> = api
        .get<Run>(`/api/v1/rtc/runs/${encodeURIComponent(id!)}?view=detail`)
        .then((run) => {
          if (run.run_id !== id) throw new Error("执行记录与当前路径不匹配");
          if (pending.get(id!)?.request === value) {
            cache.delete(id!);
            cache.set(id!, run);
            while (cache.size > 32) cache.delete(cache.keys().next().value!);
          }
          return run;
        })
        .finally(() => {
          if (pending.get(id!)?.request === value) pending.delete(id!);
        });
      pending.set(id!, { retry, request: value });
      return value;
    }

    const load = async () => {
      try {
        const run = await request();
        if (!live) return;
        setDetail(run);
        setFailure(null);
        const pendingLogs = Object.values(run.logs || {}).some((log) =>
          ["REQUESTED", "UPLOADING"].includes(log.status),
        );
        if (terminal(run.status) && !pendingLogs) return;
      } catch (e) {
        if (!live) return;
        const missing = (e as { status?: number }).status === 404;
        if (missing) {
          cache.delete(id!);
          setDetail(null);
        }
        setFailure({ id: id!, message: errorText(e), missing });
        if (missing) return;
      }
      if (live) timer = setTimeout(() => void load(), 5000);
    };
    void load();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [api, id, retry, cache, pending]);

  const error = failure?.id === id ? failure : null;
  const run = error?.missing
    ? null
    : detail?.run_id === id
      ? detail
      : fallback?.run_id === id
        ? fallback
        : id
          ? cache.get(id) || null
          : null;
  return { run, error: error?.message || "" };
}
