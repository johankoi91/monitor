// Deterministic archived evidence exercises drill-down without starting any SDK.
const assert = require("node:assert/strict");
const { openPage } = require("./navigation.cjs");
module.exports = async function checkDetails(page, shot) {
  const data = await page.evaluate(async () => {
    const auth = localStorage.getItem("avops-monitor-authorization");
    return (
      await fetch("/api/v1/rtc/status", { headers: { Authorization: auth } })
    ).json();
  });
  const task = data.tasks.find((t) => t.client_type !== "web"),
    stamp = "2026-11-19T00:10:32+08:00";
  task.enabled = true;
  const nativeCase = {
    case_key: "fixture-path",
    case_kind: "AP_JOIN",
    ap_group: "172.16.80.25",
    ap_entrypoint: "127.0.0.1",
    sender: { transport: "udp", endpoint: "" },
    receiver: { transport: "udp", endpoint: "" },
  };
  const route = {
    status: "VERIFIED",
    observed: [
      { endpoint: "127.0.0.1:4001", sent_bytes: 100, received_bytes: 100 },
    ],
    ap_observed: true,
  };
  const result = {
    outcome: "SUCCESS",
    reason: "MEDIA_VERIFIED",
    cleanup: "SUCCESS",
    evidence: {
      connected: true,
      media_duration_ms: 4640,
      received_video_frames: 73,
      video_width: 352,
      video_height: 288,
    },
    route,
    stages: [{ stage: "JOIN", started_elapsed_ms: 0, duration_ms: 380 }],
  };
  const child = {
    run_id: "fixture-case",
    parent_run_id: "fixture-round",
    task_id: task.task_id,
    target_id: task.target_id,
    driver: "agora",
    status: "SUCCESS",
    reason: "MEDIA_VERIFIED",
    created_at: stamp,
    finished_at: "2026-11-19T00:10:40+08:00",
    config_revision: data.config_revision,
    endpoints: Object.fromEntries(
      ["sender", "receiver"].map((role) => [
        role,
        {
          ready: true,
          job: {
            runtime_id: data.runtimes.find(r => r.host_id===task.host_id && r.service_type===task.service_type && r.client_type===task.client_type).runtime_id,
            native_case: nativeCase,
            sdk_version: "Go SDK fixture",
            channel: "isolated-fixture",
          },
          result,
          events: [
            {
              kind: "api_call_end",
              name: "SetParameters",
              fields: { args: { secret: "must_not_render" } },
            },
          ],
        },
      ]),
    ),
    logs: {},
  };
  const parent = {
    ...child,
    run_id: "fixture-round",
    parent_run_id: undefined,
    kind: "NATIVE_COVERAGE",
    native_plan: { phase: "AP" },
    cases: [{ case: nativeCase, run_id: child.run_id, status: "SUCCESS" }],
    endpoints: {},
  };
  data.runs = [parent, child];
  data.task_health[task.task_id] = { state: "HEALTHY", last_success_at: stamp };
  data.native_coverage = {
    [task.task_id]: {
      coverage_version: "fixture",
      expected: [],
      issues: [],
      phases: {
        AP: {
          health: { state: "HEALTHY", last_run_id: parent.run_id },
          case_count: 1,
          round: 1,
          next_at: stamp,
        },
      },
    },
  };
  const makeRound = (phase, id, from, to) => {
    const item = { ...nativeCase, case_kind: phase === "CROSS" ? "CROSS_EDGE_MEDIA" : "DIRECT_JOIN", sender: { transport: "udp", endpoint: from }, receiver: { transport: "udp", endpoint: to } };
    const leaf = { ...child, run_id: id + "-case", parent_run_id: id + "-round", native_case: item, endpoints: Object.fromEntries(Object.entries(child.endpoints).map(([role, endpoint]) => [role, { ...endpoint, job: { ...endpoint.job, native_case: item } }])) };
    const root = { ...parent, run_id: id + "-round", native_plan: { phase }, cases: [{ case: item, run_id: leaf.run_id, status: "SUCCESS" }] };
    return [root, leaf];
  };
  const [directParent, directChild] = makeRound("DIRECT", "fixture-direct", "127.0.0.1:4001", "127.0.0.1:4001");
  const [historyParent, historyChild] = makeRound("CROSS", "fixture-cross", "127.0.0.1:4001", "127.0.0.1:4002");
  const historyPathLabel = "127.0.0.1:4001 → 127.0.0.1:4002";
  data.runs.push(directParent, directChild, historyParent, historyChild);
  for (const [phase, root] of [["DIRECT", directParent], ["CROSS", historyParent]]) {
    data.native_coverage[task.task_id].phases[phase] = { health: { state: "HEALTHY", last_run_id: root.run_id }, case_count: 1, round: 1, next_at: stamp };
  }
  const network = page.context();
  let historyRequests = 0;
  let releaseHistory;
  const historyGate = new Promise(resolve => { releaseHistory = resolve; });
  const heldDetails = new Map();
  const detailReads = [];
  const missingDetails = new Set();
  function holdDetail(id) {
    let release;
    const gate = new Promise(resolve => { release = resolve; });
    heldDetails.set(id, gate);
    return () => { heldDetails.delete(id); release(); };
  }
  await network.route("**/api/v1/rtc/status*", (r) => r.fulfill({ json:
    new URL(r.request().url()).searchParams.get("view") === "history"
      ? { ...data, runs: [], native_coverage: undefined }
      : { ...data, runs: [], task_activity: Object.fromEntries(data.tasks.map(task => [task.task_id, { running: data.runs.some(run => run.task_id === task.task_id && ['RUNNING','PREPARING'].includes(run.status)) }])) },
  }));
  await network.route("**/api/v1/rtc/runs/*", async (r) => {
    const id = decodeURIComponent(new URL(r.request().url()).pathname.split("/").pop());
    detailReads.push(id);
    assert.equal(new URL(r.request().url()).searchParams.get("view"), "detail", "UI fetched complete SDK events");
    if (heldDetails.has(id)) await heldDetails.get(id);
    if (missingDetails.has(id)) return r.fulfill({ status: 404, json: { error: "执行记录不存在或已过期" } });
    return r.fulfill({ json: data.runs.find(run => run.run_id === id) || parent });
  });
  await network.route("**/api/v1/rtc/history?*", async (r) => {
    if (++historyRequests === 1) await historyGate;
    return r.fulfill({ json: { runs: [historyParent], total: 1 } });
  });
  await page.reload();
  await openPage(page, "部署服务可用性探测", "探测点任务配置");
  assert.equal(detailReads.length,0,"task list fetched execution details before opening a task");
  await page
    .getByRole("row")
    .filter({ hasText: task.name })
    .getByRole("button", { name: "详情", exact: true })
    .click();
  await page.getByRole("button", { name: "AP 接入", exact: false }).waitFor();
  await page.getByRole("link", { name: "查看", exact: true }).waitFor();
  assert.ok(!detailReads.includes(directParent.run_id) && !detailReads.includes(historyParent.run_id),"opening AP coverage fetched unselected phases");
  assert.equal(
    await page
      .getByRole("columnheader", { name: "接收证据", exact: true })
      .count(),
    0,
  );
  assert.equal(
    await page
      .getByRole("columnheader", { name: "实际连接", exact: true })
      .count(),
    0,
  );
  await page.getByRole("link", { name: "查看", exact: true }).click();
  await page.getByText("4.64 秒", { exact: true }).waitFor();
  await page.getByText("73 帧", { exact: true }).waitFor();
  await page.getByText("352 × 288", { exact: true }).waitFor();
  assert.equal(
    await page.getByRole("tab", { name: "调用与回调", exact: true }).count(),
    0,
  );
  assert.equal(
    await page.getByText("must_not_render", { exact: true }).count(),
    0,
  );
  assert.equal(await page.locator(".ant-drawer:visible").count(), 0);
  await page.getByText("执行时间", { exact: true }).waitFor();
  await shot("case-detail");
  await page.getByRole("tab", { name: "诊断日志", exact: true }).click();
  await page.getByRole("button", { name: "返回上一级", exact: true }).click();
  assert.equal(new URL(page.url()).pathname, `/rtc/tasks/${encodeURIComponent(task.task_id)}/coverage/ap`);
  await page.getByRole("button", { name: "Edge 直连", exact: false }).click();
  await page.getByRole("link", { name: "查看", exact: true }).click();
  await page.getByRole("heading", { name: "127.0.0.1:4001 → 127.0.0.1:4001", exact: true }).waitFor();
  assert.equal(new URL(page.url()).pathname, `/rtc/tasks/${encodeURIComponent(task.task_id)}/coverage/direct/paths/fixture-direct-case`);
  assert.ok(!(await page.locator('.ant-breadcrumb').innerText()).includes('Edge 直连执行'));
  assert.ok(!(await page.locator('.ant-breadcrumb').innerText()).includes('验证路径'));
  let statusPolls = 0;
  const twoStatusPolls = page.waitForResponse(response =>
    new URL(response.url()).pathname === "/api/v1/rtc/status" &&
    new URL(response.url()).searchParams.get("view") === "history" &&
    ++statusPolls === 2,
  );
  await openPage(page, "部署服务可用性探测", "执行记录");
  await twoStatusPolls;
  assert.equal(historyRequests, 1, "status polling restarted an unfinished history query");
  releaseHistory();
  await page.getByRole("table", { name: "执行记录清单", exact: true }).waitFor();
  const releaseParent = holdDetail(historyParent.run_id);
  await page.getByRole("link", { name: "查看执行", exact: true }).click();
  await page.getByRole("heading", { name: "跨 Edge 互通执行", exact: true }).waitFor();
  // The parent response remains held: the already loaded list row must paint
  // the complete parent view without waiting for another network round trip.
  assert.equal(new URL(page.url()).pathname, "/rtc/history/fixture-cross-round");
  assert.equal(await page.getByRole("tab", { name: "诊断日志", exact: true }).count(), 0);
  const pathLink = page.getByRole("link", { name: "查看", exact: true });
  assert.equal(await pathLink.getAttribute("href"), "/rtc/history/fixture-cross-round/paths/fixture-cross-case");
  await pathLink.click();
  const crumb = page.locator(".ant-breadcrumb");
  await crumb.getByText(historyPathLabel, {exact:true}).waitFor();
  releaseParent();
  for (const label of ["工作台", "部署服务可用性探测", "执行记录", "跨 Edge 互通执行", "验证路径"]) assert.ok((await crumb.innerText()).includes(label));
  assert.equal(await crumb.getByRole("link", {name:"验证路径",exact:true}).getAttribute("href"), "/rtc/history/fixture-cross-round/paths");
  await page.getByRole("tab", {name:"诊断日志",exact:true}).click();
  assert.equal(new URL(page.url()).pathname,"/rtc/history/fixture-cross-round/paths/fixture-cross-case/logs");
  await page.goBack();
  await page.getByRole("tab", {name:"结果与路径",exact:true}).waitFor();
  assert.equal(new URL(page.url()).pathname,"/rtc/history/fixture-cross-round/paths/fixture-cross-case");
  await page.goForward();
  await page.getByRole("tab", {name:"诊断日志",exact:true}).waitFor();
  await page.getByRole("button", { name: "返回上一级", exact: true }).click();
  await page.getByRole("heading", { name: "跨 Edge 互通执行", exact: true }).waitFor();
  const releaseCachedChild = holdDetail(historyChild.run_id);
  await page.getByRole("link", { name: "查看", exact: true }).click();
  await page.getByText("4.64 秒", { exact: true }).waitFor();
  await crumb.getByText(historyPathLabel, { exact: true }).waitFor();
  // The cached child must remain readable while its background update waits.
  missingDetails.add(historyChild.run_id);
  releaseCachedChild();
  await page.getByText("执行记录不存在或已过期", { exact: true }).waitFor();
  assert.equal(await page.getByText("4.64 秒", { exact: true }).count(), 0, "expired execution remained visible from cache");
  missingDetails.delete(historyChild.run_id);
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await page.getByText("4.64 秒", { exact: true }).waitFor();
  for (const [href, heading] of [
    ["/rtc/history", "执行记录"],
    ["/rtc/history/fixture-cross-round", "跨 Edge 互通执行"],
    ["/rtc/history/fixture-cross-round/paths", "跨 Edge 互通执行"],
    ["/rtc/history/fixture-cross-round/paths/fixture-cross-case", historyPathLabel],
    ["/rtc/history/fixture-cross-round/paths/fixture-cross-case/logs", historyPathLabel],
  ]) {
    const fresh = await network.newPage();
    try {
      const response = await fresh.goto(new URL(href,page.url()).toString());
      assert.equal(response.status(),200);
      await fresh.getByRole("heading",{name:heading,exact:true}).waitFor();
      if (href.endsWith("/logs")) assert.equal(await fresh.getByRole("tab",{name:"诊断日志",exact:true}).getAttribute("aria-selected"),"true");
      await fresh.reload();
      await fresh.getByRole("heading",{name:heading,exact:true}).waitFor();
      assert.equal(new URL(fresh.url()).pathname,href);
    } finally { await fresh.close(); }
  }
  await page.getByRole("button", { name: "返回上一级", exact: true }).click();
  await page
    .getByRole("heading", { name: "跨 Edge 互通执行", exact: true })
    .waitFor();
  await page.reload();
  await page
    .getByRole("heading", { name: "跨 Edge 互通执行", exact: true })
    .waitFor();
  for (const width of [375, 844, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    assert.ok(
      !(await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      )),
      "run overflow " + width,
    );
  }
  await page.goto(new URL("/rtc/history/fixture-cross-round/logs", page.url()).toString());
  await page.waitForURL("**/rtc/history/fixture-cross-round");
  assert.equal(await page.getByRole("tab", { name: "诊断日志", exact: true }).count(), 0);
  await network.unroute("**/api/v1/rtc/status*");
  await network.unroute("**/api/v1/rtc/runs/*");
  await network.unroute("**/api/v1/rtc/history?*");
  console.log(
    "PASS: cached list-to-detail and back navigation with blocked detail responses, compact detail requests, archived media metrics, contextual parent return and deep-link reload",
  );

};
