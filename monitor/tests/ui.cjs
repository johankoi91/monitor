// Browser verification. Save actions run only against the isolated Go test server.
const fs = require('fs');
const assert = require('assert/strict');
const { chromium } = require('playwright');
(async () => {
  const url = process.env.AVOPS_UI_URL || 'http://127.0.0.1:18084';
  let username = 'admin', password = 'z'.repeat(32);
  if (process.env.AVOPS_CURL_CONFIG) {
    const match = fs.readFileSync(process.env.AVOPS_CURL_CONFIG, 'utf8').match(/user\s*=\s*"([^:]+):([^"]+)"/);
    if (!match) throw Error('private curl configuration unavailable');
    [, username, password] = match;
  }
  const browser = await chromium.launch({ headless: true, executablePath: process.env.AVOPS_CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1050 } });
    const page = await context.newPage(); const errors = []; page.on('pageerror', err => errors.push(err.message));
    await page.goto(url); await page.getByLabel('接入 ID', { exact: true }).fill(username); await page.getByLabel('接入密钥', { exact: true }).fill(password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('button', { name: '容器与基准台账', exact: true }).click();
    await page.getByRole('table').waitFor();
    await page.getByLabel('容器名称', { exact: true }).fill('agora_local_ap');
    await Promise.all([page.waitForResponse(response => response.url().includes('name=agora_local_ap') && response.status() === 200), page.getByRole('button', { name: '筛选', exact: true }).click()]);
    await page.waitForFunction(() => document.querySelectorAll('tbody tr').length === 1 && document.querySelector('tbody').textContent.includes('agora_local_ap'));
    await page.getByRole('table').getByRole('button', { name: '查看', exact: true }).click(); await page.getByRole('dialog').waitFor();
    const detail = JSON.parse(await page.getByRole('dialog').locator('pre').textContent());
    assert.equal(detail.container.container_name, 'agora_local_ap'); assert.ok(detail.startup.snapshot_id);
    assert.ok((detail.startup.config.env || []).every(item => item.redacted || item.value !== '[REDACTED]'));
    await page.getByRole('button', { name: '关闭', exact: true }).click();
    if (process.env.AVOPS_ISOLATED_UI_TEST === '1') {
      // Go launches a fresh in-memory discovery feed and a temp durable store.
      await page.getByRole('checkbox', { name: '选择 agora_local_ap' }).check();
      await page.getByRole('button', { name: '加入待保存变更' }).click();
      await page.getByLabel('容器名称', { exact: true }).fill(''); await page.getByRole('button', { name: '筛选', exact: true }).click();
      await page.waitForFunction(() => document.querySelector('.pager').textContent.includes('55 个容器'));
      await page.getByRole('button', { name: '下一页' }).click(); await page.waitForFunction(() => document.querySelector('.pager').textContent.includes('第 2 页'));
      assert.ok((await page.locator('main').textContent()).includes('新增 / 重新归类：'));
      await page.getByRole('button', { name: '保存并生效', exact: true }).click();
      await page.getByRole('status').filter({ hasText: '基准已保存并生效' }).waitFor();
      await page.getByRole('button', { name: '下载 YAML', exact: true }).waitFor({ state: 'visible' }); assert.ok((await page.locator('main').textContent()).includes('rtc-a/agora_local_ap'));
      await page.getByLabel('容器名称', { exact: true }).fill('does-not-exist'); await page.getByRole('button', { name: '筛选', exact: true }).click();
      await page.waitForFunction(() => document.querySelector('tbody').textContent.includes('没有匹配容器'));
      assert.ok((await page.locator('main').textContent()).includes('rtc-a/agora_local_ap'));
    }
    if (process.env.AVOPS_UI_SCREENSHOT) await page.screenshot({ path: process.env.AVOPS_UI_SCREENSHOT, fullPage: true });
    assert.deepEqual(errors, []); console.log('Browser verified: discovery, filtering, startup detail' + (process.env.AVOPS_ISOLATED_UI_TEST === '1' ? ', pagination, selection, YAML save and filter-safe baseline.' : '. Pilot baseline unchanged.'));
    await context.close();
  } finally { await browser.close(); }
})().catch(err => { console.error(err.message); process.exitCode = 1; });
