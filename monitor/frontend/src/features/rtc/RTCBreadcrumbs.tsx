import { useEffect, useState } from "react";
import {
  locationKey,
  readLocationParams,
  rtcHref,
  type RouteLocation,
} from "../../app/routes";
import type { API } from "../../shared/api";
import { visitRTC } from "./RTCLink";
import type { Run, Snapshot } from "./RTCModel";
import { isParentRun, taskLabel } from "./RTCModel";
import { phaseLabels } from "./RTCNativeCoverage";
import { runTitle } from "./RTCRunDetails";
export interface RTCBreadcrumb {
  title: string;
  href?: string;
}
export interface RTCBreadcrumbTrail {
  location: string;
  items: RTCBreadcrumb[];
}

export function buildRTCBreadcrumbs(
  params: URLSearchParams,
  data: Snapshot,
  run: Run | null,
  parent: Run | null,
  location: RouteLocation = window.location,
): RTCBreadcrumb[] {
  if (run?.run_id !== params.get("rtc_run")) run = null;
  const page = params.get("page") || "tasks",
    id = params.get("rtc_run"),
    taskID = params.get("rtc_task"),
    targetID = params.get("rtc_target"),
    parentID = run?.parent_run_id || params.get("rtc_parent");
  if (parent?.run_id !== parentID) parent = null;
  const href = (page: string, values: Record<string, string>) =>
    rtcHref(page, values, location);
  const values: Record<string, string> = {};
  if (page === "tasks" && taskID) {
    values.task = taskID;
    values.phase = params.get("rtc_phase") || "AP";
  }
  const items: RTCBreadcrumb[] = [];
  if (taskID && page === "tasks") {
    const task = data.tasks.find((task) => task.task_id === taskID);
    items.push({
      title: task ? taskLabel(task) : "任务详情",
      href: href("tasks", { task: taskID }),
    });
    items.push({
      title: phaseLabels[values.phase] || "当前覆盖",
      ...(id ? { href: href("tasks", values) } : {}),
    });
  }
  if (targetID)
    items.push({
      title:
        data.targets.find((target) => target.target_id === targetID)?.name ||
        "目标详情",
    });
  if (id) {
    const logs = params.get("rtc_tab") === "logs" && !(run && isParentRun(run));
    if (
      page === "tasks" &&
      taskID &&
      (params.get("rtc_case") === "1" || parentID || (run && !isParentRun(run)))
    ) {
      items.push({
        title: run ? runTitle(run) : "路径详情",
        ...(logs
          ? { href: href(page, { ...values, run: id, case: "1" }) }
          : {}),
      });
      if (logs) items.push({ title: "诊断日志" });
      return items;
    }
    const hasPaths = !!parentID || params.get("rtc_paths") === "1";
    const parentValues = { ...values, run: parentID || id };
    items.push({
      title: (parentID ? parent : run)
        ? runTitle((parentID ? parent : run)!)
        : "本次执行",
      ...(hasPaths || logs ? { href: href(page, parentValues) } : {}),
    });
    if (hasPaths)
      items.push({
        title: "验证路径",
        ...(parentID || logs
          ? { href: href(page, { ...parentValues, paths: "1" }) }
          : {}),
      });
    if (parentID)
      items.push({
        title: run ? runTitle(run) : "路径详情",
        ...(logs
          ? { href: href(page, { ...values, run: id, parent: parentID }) }
          : {}),
      });
    if (logs) items.push({ title: "诊断日志" });
  }
  return items;
}

// A delayed effect may run after navigation. It must never rewrite a newer URL
// using the record or task context from its previous render.
export function correctedRunHref(
  params: URLSearchParams,
  run: Run | null,
  location: RouteLocation = window.location,
): string | null {
  if (
    !run ||
    params.get("view") !== "rtc" ||
    params.get("rtc_run") !== run.run_id
  )
    return null;
  if (readLocationParams(location).toString() !== params.toString())
    return null;
  const page = params.get("page") || "history";
  const values: Record<string, string> = { run: run.run_id };
  if (page === "tasks" && params.get("rtc_task")) {
    values.task = params.get("rtc_task")!;
    values.phase = params.get("rtc_phase") || "AP";
  }
  if (isParentRun(run) && params.get("rtc_tab") === "logs") {
    values.paths = params.get("rtc_paths") || "";
    return rtcHref(page, values, location);
  }
  if (page === "tasks" && !isParentRun(run)) {
    values.case = "1";
    values.tab = params.get("rtc_tab") || "";
    const href = rtcHref(page, values, location);
    return href === locationKey(location) ? null : href;
  }
  if (run.parent_run_id && !params.get("rtc_parent")) {
    values.parent = run.parent_run_id;
    values.tab = params.get("rtc_tab") || "";
    return rtcHref(page, values, location);
  }
  return null;
}

export default function RTCBreadcrumbs({
  api,
  data,
  run,
  params,
  onChange,
}: {
  api: API;
  data: Snapshot;
  run: Run | null;
  params: URLSearchParams;
  onChange?: (trail: RTCBreadcrumbTrail) => void;
}) {
  const parentID =
    params.get("page") === "tasks"
      ? null
      : run?.parent_run_id || params.get("rtc_parent");
  const [archivedParent, setArchivedParent] = useState<Run | null>(null);
  const cached = data.runs.find((item) => item.run_id === parentID);
  useEffect(() => {
    let active = true;
    if (!parentID || cached) return;
    void api
      .get<Run>(`/api/v1/rtc/runs/${encodeURIComponent(parentID)}`)
      .then((value) => {
        if (active) setArchivedParent(value);
      })
      .catch(() => {
        if (active) setArchivedParent(null);
      });
    return () => {
      active = false;
    };
  }, [api, parentID, cached?.run_id]);
  const parent =
    cached || (archivedParent?.run_id === parentID ? archivedParent : null);
  const items = buildRTCBreadcrumbs(params, data, run, parent);
  const key = locationKey(),
    serialized = JSON.stringify(items);
  useEffect(() => {
    if (
      locationKey() !== key ||
      readLocationParams().toString() !== params.toString()
    )
      return;
    onChange?.({ location: key, items: JSON.parse(serialized) });
  }, [onChange, key, serialized, params]);
  useEffect(() => {
    const href = correctedRunHref(params, run);
    if (href) visitRTC(href, true);
  }, [run?.run_id, run?.parent_run_id, run?.kind, params]);
  return null;
}
