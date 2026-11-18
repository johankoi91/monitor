// Isolated native-configuration UI test; no RTC connection is made.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {openPage,checkCurrent}=require('./navigation.cjs');
const {chromium}=require(process.env.AVOPS_PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 assert.equal(process.env.AVOPS_ISOLATED_UI_TEST,'1');
 const url=new URL(process.env.AVOPS_UI_URL);assert.ok(['127.0.0.1','localhost'].includes(url.hostname));
 const browser=await chromium.launch({headless:true,executablePath:process.env.AVOPS_CHROME_PATH||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1050}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(url.toString());await page.getByLabel('账号',{exact:true}).fill('fixture');await page.getByLabel('密码',{exact:true}).fill('fixture123');await page.getByRole('button',{name:'登录',exact:true}).click();
  await openPage(page,'RTC 业务探测','探测任务');
  assert.equal(await page.getByRole('tab',{name:'状态与操作审计',exact:true}).count(),0);
  await openPage(page,'RTC 业务探测','目标服务');await page.getByRole('button',{name:'创建目标服务',exact:true}).click();
  await page.getByLabel('服务名称',{exact:true}).fill('UI 配置契约验证');
  await page.getByLabel('Agora AppID',{exact:true}).fill('a'.repeat(32));await page.getByLabel('App Certificate',{exact:true}).fill('c'.repeat(32));
  await page.getByLabel('连接私有化部署',{exact:true}).uncheck();
  const targetSaved=page.waitForResponse(r=>r.url().endsWith('/api/v1/rtc/config')&&r.request().method()==='POST'&&r.ok());await page.getByRole('button',{name:'保存配置',exact:true}).click();const targetResponse=await (await targetSaved).json();const createdTarget=targetResponse.targets.find(t=>t.name==='UI 配置契约验证');assert.ok(createdTarget.target_id);
  await openPage(page,'RTC 业务探测','探测任务');await page.getByRole('button',{name:'创建任务',exact:true}).click();
  const dialog=page.getByRole('dialog');await dialog.getByLabel('任务标识',{exact:true}).fill('ui-native-task');
  async function select(label,value){await dialog.getByLabel(label,{exact:true}).click();await page.locator('.ant-select-item-option-content').getByText(value,{exact:true}).last().click()}
  await select('目标服务','UI 配置契约验证');await select('执行探针','模拟位置 A · probe-a');
  await dialog.getByText('来自目标服务的接入配置',{exact:true}).waitFor();
  assert.equal(await dialog.getByLabel('Agora AppID',{exact:true}).count(),0);assert.equal(await dialog.getByLabel('App Certificate',{exact:true}).count(),0);
  assert.equal(await dialog.getByLabel('启用任务',{exact:true}).count(),0);
  assert.equal(await dialog.getByLabel('模拟场景',{exact:true}).count(),0);assert.equal(await dialog.getByLabel('视频分辨率',{exact:true}).count(),0);
  const taskSaved=page.waitForResponse(r=>r.url().endsWith('/api/v1/rtc/config')&&r.request().method()==='POST'&&r.ok());await dialog.getByRole('button',{name:'保存配置',exact:true}).click();const saved=await (await taskSaved).json();
  const task=saved.tasks.find(t=>t.task_id==='ui-native-task');assert.equal(task.enabled,false);assert.equal(task.probe_id,'probe-a');assert.equal(task.sender_probe_id,undefined);assert.equal(task.receiver_probe_id,undefined);assert.equal(task.rtc,undefined);assert.equal(saved.targets.find(t=>t.target_id===createdTarget.target_id).has_certificate,true);assert.ok(!JSON.stringify(saved).includes('c'.repeat(32)));
  const row=page.getByRole('row').filter({hasText:'ui-native-task'});await row.getByRole('button',{name:'详情',exact:true}).waitFor();await row.getByRole('button',{name:'启用',exact:true}).waitFor();
  assert.equal(await page.getByRole('columnheader',{name:'双端路径',exact:true}).count(),0);assert.equal(await page.getByRole('columnheader',{name:'业务状态',exact:true}).count(),1);
  if(process.env.AVOPS_RTC_SCREENSHOT_DIR){fs.mkdirSync(process.env.AVOPS_RTC_SCREENSHOT_DIR,{recursive:true});await page.screenshot({path:path.join(process.env.AVOPS_RTC_SCREENSHOT_DIR,'rtc-native-desktop.png'),fullPage:true})}
  await openPage(page,'RTC 业务探测','探针'); await checkCurrent(page,'探针');
  await openPage(page,'RTC 业务探测','执行记录'); await checkCurrent(page,'执行记录');
  await page.reload(); await page.locator('.workspace-page-heading').getByRole('heading',{name:'执行记录',exact:true}).waitFor();
  await checkCurrent(page,'执行记录');
  await openPage(page,'RTC 业务探测','历史与容量'); await checkCurrent(page,'历史与容量');
  await openPage(page,'RTC 业务探测','探测任务');
  await page.setViewportSize({width:390,height:844});assert.ok(!await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1));assert.deepEqual(errors,[]);
  console.log('PASS: native task settings, fixed demo media, single probe/two connections, write-only credentials and mobile layout; no RTC execution');
 }finally{await browser.close()}
})().catch(e=>{console.error(e.message);process.exitCode=1});
