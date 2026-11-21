/** Shared, data-independent URL codec. Deep links never depend on tab history. */
export interface RouteLocation {
  pathname: string;
  search: string;
  hash?: string;
}
const rtcPages = new Set(["tasks", "targets", "probes", "history", "settings"]);
const structural = (key: string) =>
  key === "view" || key === "page" || key.startsWith("rtc_");
const encode = encodeURIComponent;
const decode = (value: string) => {
  try {
    return decodeURIComponent(value);
  } catch {
    return "";
  }
};
function normalizeMonitorLinks(params: URLSearchParams) {
  if (params.get("view") === "inventory" && params.get("page") === "baseline") {
    params.set("view", "status");
    params.delete("page");
  }
  if (
    params.get("view") === "rtc" &&
    ["probes", "retention"].includes(params.get("page") || "")
  ) {
    params.set(
      "rtc_setting",
      params.get("page") === "probes" ? "servers" : "retention",
    );
    params.set("page", "settings");
  }
}
const currentLocation = (): RouteLocation => window.location;
export const locationKey = (location: RouteLocation = currentLocation()) =>
  `${location.pathname}${location.search}`;

function inheritLegacy(params: URLSearchParams) {
  let source = params.get("rtc_from");
  const seen = new Set<string>();
  for (let depth = 0; source && depth < 8 && !seen.has(source); depth++) {
    seen.add(source);
    // Old links stored only a query string. Do not follow another origin.
    if (!source.startsWith("?") && !source.startsWith("/")) break;
    const parent = readLocationParams(
      {
        pathname: source.split("?")[0] || "/",
        search: source.includes("?")
          ? "?" + source.split("?").slice(1).join("?")
          : "",
      },
      false,
    );
    for (const key of ["rtc_task", "rtc_phase"])
      if (!params.has(key) && parent.has(key))
        params.set(key, parent.get(key)!);
    if (
      !params.has("rtc_parent") &&
      params.get("rtc_run") &&
      parent.get("rtc_run") &&
      parent.get("rtc_run") !== params.get("rtc_run")
    )
      params.set("rtc_parent", parent.get("rtc_run")!);
    source = parent.get("rtc_from");
  }
}

export function readLocationParams(
  location: RouteLocation = currentLocation(),
  inherit = true,
): URLSearchParams {
  const params = new URLSearchParams(location.search);
  const parts = location.pathname.split("/").filter(Boolean).map(decode);
  if (!parts.length || location.pathname === "/index.html") {
    normalizeMonitorLinks(params);
    if (params.get("view") === "rtc" && !params.get("page"))
      params.set("page", "tasks");
    if (inherit) inheritLegacy(params);
    if (params.get("view") === "rtc" && params.has("rtc_run")) {
      params.delete("rtc_target");
      if (params.get("page") !== "tasks" || !params.has("rtc_task"))
        params.set("page", "history");
    }
    return params;
  }
  const bases: Record<string, [string, string]> = {
    overview: ["overview", ""],
    operations: ["operations", ""],
    capacity: ["capacity", ""],
    access: ["access", ""],
  };
  const mapped = parts.length === 1 ? bases[parts[0]] : undefined;
  if (mapped) {
    for (const key of [...params.keys()])
      if (structural(key)) params.delete(key);
    params.set("view", mapped[0]);
    return params;
  }
  if (parts[0] !== "rtc") {
    if (
      parts[0] === "monitor" &&
      parts[1] === "discovery" &&
      parts.length === 2
    ) {
      for (const key of [...params.keys()])
        if (structural(key)) params.delete(key);
      params.set("view", "inventory");
      params.set("page", "discovery");
      return params;
    }
    const pages: Record<string, [string, string[]]> = {
      monitor: ["status", ["containers"]],
      inventory: ["inventory", ["discovery", "baseline"]],
      notifications: ["notifications", ["delivery", "config"]],
      accounts: ["accounts", ["users", "roles"]],
    };
    const config = pages[parts[0]];
    if (config && parts.length === 2 && config[1].includes(parts[1])) {
      for (const key of [...params.keys()])
        if (structural(key)) params.delete(key);
      params.set("view", config[0]);
      if (config[0] !== "status") params.set("page", parts[1]);
      normalizeMonitorLinks(params);
      return params;
    }
    params.set("view", "invalid");
    return params;
  }
  for (const key of [...params.keys()]) if (structural(key)) params.delete(key);
  const requestedPage = parts[1] || "tasks";
  const page = ["retention", "probes"].includes(requestedPage)
    ? "settings"
    : requestedPage;
  params.set("view", "rtc");
  params.set("page", page);
  if (!rtcPages.has(page) || parts.some((part) => !part)) {
    params.set("view", "invalid");
    return params;
  }
  if (parts.length === 1) return params;
  if (page === "settings") {
    const setting =
      parts[2] || (requestedPage === "retention" ? "retention" : "servers");
    if (parts.length > 3 || !["servers", "retention"].includes(setting))
      params.set("view", "invalid");
    else params.set("rtc_setting", setting);
    return params;
  }
  let index = 2;
  if (page === "targets" && parts[index]) {
    params.set("rtc_target", parts[index++]);
  } else if (page === "tasks" && parts[index]) {
    params.set("rtc_task", parts[index++]);
    if (parts[index] === "coverage" && parts[index + 1]) {
      params.set("rtc_phase", parts[index + 1].toUpperCase());
      index += 2;
    }
    if (parts[index] === "runs" && parts[index + 1]) {
      params.set("rtc_run", parts[index + 1]);
      index += 2;
    } else if (parts[index] === "paths" && parts[index + 1]) {
      params.set("rtc_run", parts[index + 1]);
      params.set("rtc_case", "1");
      index += 2;
    }
  } else if (page === "history" && parts[index])
    params.set("rtc_run", parts[index++]);
  if (params.has("rtc_run") && parts[index] === "paths") {
    index++;
    params.set("rtc_paths", "1");
    if (parts[index]) {
      params.set("rtc_parent", params.get("rtc_run")!);
      params.set("rtc_run", parts[index++]);
    }
  }
  if (params.has("rtc_run") && parts[index] === "logs") {
    params.set("rtc_tab", "logs");
    index++;
  }
  if (index !== parts.length) params.set("view", "invalid");
  return params;
}

export function rtcHref(
  page: string,
  values: Record<string, string> = {},
  location: RouteLocation = currentLocation(),
): string {
  const params = new URLSearchParams();
  params.set("view", "rtc");
  params.set(
    "page",
    ["retention", "probes"].includes(page) ? "settings" : page,
  );
  if (["retention", "probes"].includes(page))
    params.set("rtc_setting", page === "probes" ? "servers" : "retention");
  for (const [key, value] of Object.entries(values))
    if (value) params.set(`rtc_${key}`, value);
  inheritLegacy(params);
  const task = params.get("rtc_task"),
    run = params.get("rtc_run"),
    parent = params.get("rtc_parent"),
    target = params.get("rtc_target");
  let path = `/rtc/${params.get("page")}`;
  const taskScope = task && page === "tasks";
  const taskCase =
    taskScope &&
    run &&
    ((parent && parent !== run) || params.get("rtc_case") === "1");
  if (params.get("page") === "settings")
    path = "/rtc/settings/" + encode(params.get("rtc_setting") || "servers");
  else if (target) path = `/rtc/targets/${encode(target)}`;
  else if (taskScope) {
    const phase = params.get("rtc_phase") || "AP";
    path += `/${encode(task)}/coverage/${encode(phase.toLowerCase())}`;
    if (taskCase) path += `/paths/${encode(run!)}`;
    else if (run) path += `/runs/${encode(run)}`;
  } else if (run)
    path = `/rtc/history/${encode(parent && parent !== run ? parent : run)}`;
  if (!taskScope && run && parent && parent !== run)
    path += `/paths/${encode(run)}`;
  else if (
    !taskCase &&
    run &&
    params.get("rtc_paths") === "1" &&
    params.get("rtc_tab") !== "logs"
  )
    path += "/paths";
  if (run && params.get("rtc_tab") === "logs") path += "/logs";
  const filters = new URLSearchParams(location.search);
  for (const key of [...filters.keys()])
    if (structural(key)) filters.delete(key);
  const search = filters.toString();
  return path + (search ? `?${search}` : "");
}

export function workspaceHref(
  route: { view: string; page: string },
  location: RouteLocation = currentLocation(),
): string {
  if (route.view === "rtc") return rtcHref(route.page || "tasks", {}, location);
  const bases: Record<string, string> = {
    status: "/monitor/containers",
    overview: "/overview",
    operations: "/operations",
    capacity: "/capacity",
    access: "/access",
  };
  const path =
    route.view === "inventory"
      ? route.page === "baseline"
        ? "/monitor/containers"
        : "/monitor/discovery"
      : bases[route.view] || `/${route.view}/${encode(route.page)}`;
  const filters = new URLSearchParams(location.search);
  for (const key of [...filters.keys()])
    if (structural(key)) filters.delete(key);
  return path + (filters.size ? `?${filters}` : "") + (location.hash || "");
}

export function canonicalHref(
  route: { view: string; page: string },
  location: RouteLocation = currentLocation(),
): string {
  const params = readLocationParams(location);
  if (
    route.view === "rtc" &&
    params.get("view") === "rtc" &&
    route.page === params.get("page")
  ) {
    const values: Record<string, string> = {};
    for (const [key, value] of params)
      if (key.startsWith("rtc_")) values[key.slice(4)] = value;
    return rtcHref(route.page, values, location);
  }
  return workspaceHref(route, location);
}

export function rtcRunHref(
  id: string,
  parentID?: string,
  location: RouteLocation = currentLocation(),
): string {
  const params = readLocationParams(location),
    values: Record<string, string> = { run: id };
  if (params.get("page") === "tasks") {
    if (params.get("rtc_task")) values.task = params.get("rtc_task")!;
    values.phase = params.get("rtc_phase") || "AP";
    if (params.get("rtc_case") === "1" && params.get("rtc_run") === id)
      values.case = "1";
  }
  const parent =
    parentID ||
    params.get("rtc_parent") ||
    (params.get("rtc_run") !== id ? params.get("rtc_run") : "");
  if (parent && parent !== id) values.parent = parent;
  return rtcHref(params.get("page") || "history", values, location);
}
