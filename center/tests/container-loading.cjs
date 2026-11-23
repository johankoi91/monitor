const assert = require('node:assert/strict');
const {openPage} = require('./navigation.cjs');

exports.list = async function(page) {
  let releaseBaseline;
  const baselineGate = new Promise(resolve => { releaseBaseline = resolve; });
  await page.route('**/api/v1/baseline', async route => {
    await baselineGate;
    await route.continue();
  });
  const metadataRead = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/baseline');
  await openPage(page, '监控', '容器服务');
  // The table must paint while the separate baseline request is still held.
  await page.getByRole('row').filter({hasText:'agora_local_ap'}).waitFor();
  releaseBaseline();
  await metadataRead;
  await page.unroute('**/api/v1/baseline');

  let releaseStatus, statusStarted;
  const statusGate = new Promise(resolve => { releaseStatus = resolve; });
  const started = new Promise(resolve => { statusStarted = resolve; });
  let calls = 0;
  await page.route('**/api/v1/services/status', async route => {
    calls++; statusStarted();
    await statusGate;
    const response = await route.fetch();
    const data = await response.json();
    data.services[0].nodes[0].container_name = 'agora_local_ap-slow-result';
    await route.fulfill({response,json:data});
  });
  await page.clock.install();
  await page.locator('.workspace-refresh button').click();
  await started;
  await page.clock.fastForward(16000);
  await page.getByRole('heading',{name:'容器服务',exact:true}).innerText();
  assert.equal(calls,1,'the 15-second poll duplicated an unfinished status request');
  releaseStatus();
  await page.getByRole('row').filter({hasText:'agora_local_ap-slow-result'}).waitFor();
  await page.unroute('**/api/v1/services/status');
  await page.clock.resume();

  await page.route('**/api/v1/baseline', route => route.fulfill({status:503,json:{message:'隔离测试：配置读取失败'}}));
  await page.locator('.workspace-refresh button').click();
  await page.getByText('监控配置暂时不可用，容器状态仍正常更新',{exact:true}).waitFor();
  const row = page.getByRole('row').filter({hasText:'agora_local_ap'});
  await row.waitFor();
  assert.equal(await row.getByRole('button',{name:'设置',exact:true}).isDisabled(),true);
  await page.unroute('**/api/v1/baseline');
  await page.getByRole('button',{name:'重试配置',exact:true}).click();
  await page.getByText('监控配置暂时不可用，容器状态仍正常更新',{exact:true}).waitFor({state:'hidden'});
  await row.getByRole('button',{name:'设置',exact:true}).click({trial:true});
};

exports.detail = async function(page, requests) {
  const historyPath = '**/api/v1/nodes/resource-history?*';
  assert.equal(requests.filter(path => path === '/api/v1/nodes/startup').length,0);
  await page.getByRole('button',{name:'查看详情',exact:true}).click();
  const toolbar = page.locator('.resource-trends-toolbar');
  await toolbar.getByText(/截至/).waitFor();
  assert.equal(requests.filter(path => path === '/api/v1/nodes/startup').length,0,'opening resource details fetched startup parameters');
  await page.getByRole('tab',{name:'启动参数',exact:true}).click();
  await page.getByText(/最新上报/).waitFor();
  assert.equal(requests.filter(path => path === '/api/v1/nodes/startup').length,1);
  let releaseHistory;
  const historyGate = new Promise(resolve => { releaseHistory = resolve; });
  let calls = 0;
  await page.route(historyPath, async route => { calls++; await historyGate; await route.continue(); });
  await page.getByRole('tab',{name:'资源详情',exact:true}).click();
  await toolbar.getByText(/截至/).waitFor();
  await page.getByRole('tab',{name:'启动参数',exact:true}).click();
  await page.getByText(/最新上报/).waitFor();
  assert.equal(requests.filter(path => path === '/api/v1/nodes/startup').length,1,'same snapshot was fetched again');
  await page.getByRole('tab',{name:'资源详情',exact:true}).click();
  await toolbar.getByText(/截至/).waitFor();
  await page.getByRole('button',{name:'关闭容器详情',exact:true}).click();
  await page.getByRole('dialog',{name:'容器详情',exact:true}).waitFor({state:'hidden'});
  await page.getByRole('button',{name:'查看详情',exact:true}).click();
  await toolbar.getByText(/截至/).waitFor();
  assert.equal(calls,1,'tab/close/reopen duplicated the same in-flight history read');
  const historyResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/nodes/resource-history');
  releaseHistory(); await historyResponse;
  await page.unroute(historyPath);

  await page.route('**/api/v1/services/status', async route => {
    const response = await route.fetch(), data = await response.json();
    data.services[0].nodes[0].startup_ref.snapshot_id = 'updated-startup-snapshot';
    await route.fulfill({response,json:data});
  });
  await page.route('**/api/v1/nodes/startup?*', async route => {
    const u = new URL(route.request().url());
    u.searchParams.set('snapshot_id','snapshot-agora_local_ap');
    const response = await route.fetch({url:u.toString()}), data = await response.json();
    data.startup.snapshot_id='updated-startup-snapshot';
    data.startup.config.cmd=['updated-startup-marker'];
    await route.fulfill({response,json:data});
  });
  const statusResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/services/status');
  await page.clock.fastForward(16000); await statusResponse;
  await page.getByRole('tab',{name:'启动参数',exact:true}).click();
  await page.getByText('["updated-startup-marker"]',{exact:true}).waitFor();
  assert.equal(requests.filter(path => path === '/api/v1/nodes/startup').length,2,'new snapshot did not invalidate the startup cache');
  await page.unroute('**/api/v1/services/status');
  await page.unroute('**/api/v1/nodes/startup?*');
  await page.getByRole('tab',{name:'资源详情',exact:true}).click();
  await page.locator('.resource-trend-canvas canvas').first().waitFor();
  assert.equal(await page.locator('.resource-trend-canvas canvas').count(),4);
  await page.getByText('24 小时',{exact:true}).click();
  assert.equal(new URL(page.url()).searchParams.get('range'),'24h');
  await page.getByRole('button',{name:'关闭容器详情',exact:true}).click();
  await page.getByRole('dialog',{name:'容器详情',exact:true}).waitFor({state:'hidden'});
};
