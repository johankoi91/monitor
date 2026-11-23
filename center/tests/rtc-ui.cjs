// Real management APIs in an isolated loopback fixture; no RTC SDK is connected.
const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  path = require("node:path");
const { openPage, checkCurrent } = require("./navigation.cjs");
const { chromium } = require(
  process.env.AVOPS_PLAYWRIGHT_MODULE || "playwright",
);
(async () => {
  assert.equal(process.env.AVOPS_ISOLATED_UI_TEST, "1");
  const url = new URL(process.env.AVOPS_UI_URL);
  assert.ok(["127.0.0.1", "localhost"].includes(url.hostname));
  const browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AVOPS_CHROME_PATH ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  const context = await browser.newContext({viewport: {width: 1440, height: 1050}});
  const page = await context.newPage(), errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const shot = async (name) => {
    if (process.env.AVOPS_RTC_SCREENSHOT_DIR) {
      fs.mkdirSync(process.env.AVOPS_RTC_SCREENSHOT_DIR, { recursive: true });
      await page.screenshot({
        path: path.join(process.env.AVOPS_RTC_SCREENSHOT_DIR, name + ".png"),
        fullPage: true,
        animations: "disabled",
      });
    }
  };
  const saved = () =>
    page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/rtc/config") &&
        r.request().method() === "POST" &&
        r.ok(),
    );
  const select = async (label) => {
    const input = page.getByRole("dialog").getByLabel(label, { exact: true });
    await input.press("ArrowDown");
    await input.press("Enter");
  };
  page.setDefaultTimeout(12000);
  try {
    await page.goto(url.toString());
    await page.getByLabel("账号", { exact: true }).fill("fixture");
    await page.getByLabel("密码", { exact: true }).fill("fixture123");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await openPage(page, "部署服务可用性探测", "探测点任务配置");
    await require("./rtc-editor-close.cjs")(page);
    await openPage(page, "部署服务可用性探测", "探测目标配置");
    await page
      .getByRole("button", { name: "创建探测目标配置", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("服务名称", { exact: true })
      .fill("UI 配置契约验证");
    await dialog
      .getByLabel("Agora AppID", { exact: true })
      .fill("a".repeat(32));
    await dialog
      .getByLabel("App Certificate", { exact: true })
      .fill("c".repeat(32));
    await dialog.getByLabel("AP 节点（自动发现）", { exact: true }).waitFor();
    assert.equal(
      await dialog
        .getByLabel("AP 客户端 IP（ip_for_client）", { exact: true })
        .count(),
      0,
    );
    assert.equal(
      await dialog.getByLabel("覆盖方式", { exact: true }).count(),
      0,
    );
    assert.equal(
      await dialog.getByLabel("起始 UDP 端口", { exact: true }).count(),
      0,
    );

    await dialog
      .getByRole("checkbox", { name: "Web · 无头浏览器", exact: true })
      .check();
    await dialog.getByRole("tab", { name: "Web 接入", exact: true }).click();
    await dialog
      .getByRole("checkbox", { name: "忽略浏览器证书校验", exact: true })
      .check();
    let response = saved();
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    const config = await (await response).json(),
      target = config.targets.find((t) => t.name === "UI 配置契约验证");
    assert.ok(target.target_id);
    assert.equal(target.rtc.web_access.ignore_certificates, true);
    assert.equal(target.has_certificate, true);
    assert.deepEqual(
      target.rtc.native_coverage.groups,
      target.rtc.web_access.coverage.groups,
    );
    assert.ok(!JSON.stringify(config).includes("c".repeat(32)));
    async function createTask(name, web) {
      await openPage(page, "部署服务可用性探测", "探测点任务配置");
      await page.getByRole("button", { name: "创建探测点任务", exact: true }).click();
      await dialog.getByLabel("任务名称", { exact: true }).fill(name);
      if (web)
        await dialog.getByText("Web · 无头浏览器", { exact: true }).click();
      await select("探测目标配置", "UI 配置契约验证");
      await select(
        "执行服务器",
        web ? "模拟服务器 · 127.0.0.1 · RTC / Web" : "模拟服务器 · 127.0.0.1 · RTC / Native",
      );
      assert.equal(
        await dialog.getByLabel("Agora AppID", { exact: true }).count(),
        0,
      );
      assert.equal(
        await dialog.getByLabel("启用任务", { exact: true }).count(),
        0,
      );
      assert.equal(
        await dialog.getByLabel("视频分辨率", { exact: true }).count(),
        0,
      );
      const wait = saved();
      await dialog
        .getByRole("button", { name: "保存配置", exact: true })
        .click();
      const plan = await (await wait).json();
      await dialog.waitFor({ state: "hidden" });
      return plan.tasks.find((t) => t.name === name);
    }
    const native = await createTask("Native 界面契约", false),
      web = await createTask("Web 界面契约", true);
    assert.ok(native.task_id);
    assert.equal(native.enabled, false);
    assert.ok(native.host_id);assert.equal(native.service_type,"rtc");assert.equal(native.client_type,"native");
    assert.equal(web.client_type, "web");
    assert.equal(web.host_id,native.host_id);
    assert.equal(native.interval_seconds, 600);
    assert.equal(native.sweep_interval_seconds, 900);
    await page
      .getByRole("columnheader", { name: "业务状态", exact: true })
      .waitFor();
    assert.equal(
      await page
        .getByRole("columnheader", { name: "业务状态", exact: true })
        .count(),
      1,
    );
    for (const name of ["双端路径", "执行服务器", "调度"])
      assert.equal(
        await page.getByRole("columnheader", { name, exact: true }).count(),
        0,
      );
    assert.equal(
      await page.getByText("管理探针 →", { exact: true }).count(),
      0,
    );
    await shot("tasks");
    await page
      .getByRole("row")
      .filter({ hasText: "Native 界面契约" })
      .getByRole("button", { name: "详情", exact: true })
      .click();
    await page
      .getByRole("heading", { name: "Native 界面契约", exact: true })
      .waitFor();
    assert.equal(
      await page.getByRole("tab", { name: "任务配置", exact: true }).count(),
      0,
    );
    const rules = page.getByRole("button", { name: "执行规则", exact: false });
    assert.equal(await rules.getAttribute("aria-expanded"), "false");
    await rules.click();
    assert.equal(await rules.getAttribute("aria-expanded"), "true");
    assert.equal(
      await page
        .locator("#rtc-task-rules button,#rtc-task-rules input")
        .count(),
      0,
    );
    await rules.click();
    await page.reload();
    await page
      .getByRole("heading", { name: "Native 界面契约", exact: true })
      .waitFor();
    await shot("task-detail");
    await openPage(page, "部署服务可用性探测", "探测目标配置");
    await page
      .getByRole("button", { name: "查看配置", exact: true })
      .first()
      .click();
    await page
      .getByRole("button", { name: "编辑接入配置", exact: true })
      .click();
    await dialog
      .getByLabel("服务名称", { exact: true })
      .fill("UI 配置契约验证 - 更新");
    response = saved();
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    const updated = await (await response).json();
    assert.equal(updated.targets[0].has_certificate, true);
    assert.deepEqual(
      updated.targets[0].rtc.web_access.coverage.groups,
      target.rtc.web_access.coverage.groups,
    );
    for (const name of ["执行记录", "运行设置"]) {
      await openPage(page, "部署服务可用性探测", name);
      await checkCurrent(page, name);
    }
    assert.equal(
      await page.getByRole("tab", { name: "探针", exact: true }).count(),
      0,
    );
    await page.getByRole("tab",{name:"数据保留",exact:true}).click();
    assert.equal(
      await page
        .getByLabel("历史记录预算（GiB）", { exact: true })
        .inputValue(),
      "2",
    );
    assert.equal(
      await page
        .getByLabel("成功日志保留（小时）", { exact: true })
        .inputValue(),
      "1",
    );
    await shot("settings");
    await openPage(page, "部署服务可用性探测", "运行设置");
    await page.getByRole("tab",{name:"探测服务器",exact:true}).click();
    await page
      .getByRole("button", { name: "详情", exact: true })
      .first()
      .click();
    assert.equal(await dialog.getByText("容量", { exact: true }).count(), 2);
    await dialog.getByRole("button", { name: "关闭", exact: true }).focus();
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    assert.equal(await page.getByRole("button", {name:"登记探针",exact:true}).count(),0);
    await page.getByRole("button",{name:"部署接入",exact:true}).click();
    await dialog.getByText("服务器启动后自动出现在列表中，共享服务器身份以合并多个客户端。",{exact:true}).waitFor();
    await dialog.getByRole("button",{name:"知道了",exact:true}).click();
    await shot("probes");
    await openPage(page, "部署服务可用性探测", "探测点任务配置");
    await page
      .getByRole("button", { name: "更多操作 Native 界面契约", exact: true })
      .click();
    await page.getByRole("menuitem", { name: "编辑任务", exact: true }).click();
    await dialog
      .getByLabel("任务名称", { exact: true })
      .fill("Native 改名验证");
    response = saved();
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    const renamed = await (await response).json();
    assert.equal(
      renamed.tasks.find((t) => t.task_id === native.task_id).name,
      "Native 改名验证",
    );
    assert.equal(
      renamed.tasks.find((t) => t.task_id === native.task_id).enabled,
      false,
    );
    await dialog.waitFor({ state: "hidden" });
    for (const width of [375, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 1, null, {timeout:3000}).catch(()=>{});
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) console.log('Overflow diagnostics', await page.evaluate(() => [...document.querySelectorAll('body *')].filter(e => {const r=e.getBoundingClientRect();return r.right>innerWidth+1 && r.width>0 && getComputedStyle(e).overflowX==='visible';}).map(e=>({tag:e.tagName,cls:String(e.className),right:e.getBoundingClientRect().right,width:e.getBoundingClientRect().width})).slice(0,18)));
      assert.ok(
        !(await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth + 1,
        )),
        "page overflow " + width,
      );
    }
    await shot("responsive");
    await require("./rtc-details.cjs")(page, shot);
    assert.deepEqual(errors, []);
    console.log(
      "PASS: R6 navigation, stable task IDs and display names, shared Native/Web configuration, write-only credentials, read-only rules, registration, retention units and responsive layout",
    );
  } catch (e) {
    await shot("failure");
    fs.writeFileSync("/tmp/rtc-r6-failure.html", await page.content());
    console.error(
      await page.locator(".ant-form-item-explain-error").allTextContents(),
    );
    throw e;
  } finally {
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
