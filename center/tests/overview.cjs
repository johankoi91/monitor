const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const root = path.resolve(__dirname, '../frontend');
const req = Module.createRequire(path.join(root, 'package.json'));
const ts = req('typescript');
const original = Module._resolveFilename;
Module._resolveFilename = function (name, parent, ...rest) {
  return original.call(this, name.startsWith('antd/es/') ? name.replace('antd/es/', 'antd/lib/') : name, parent, ...rest);
};
for (const ext of ['.tsx', '.ts']) require.extensions[ext] = (module, filename) => module._compile(ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText, filename);
const React = req('react');
const { renderToStaticMarkup } = req('react-dom/server');
const Overview = require(path.join(root, 'src/features/dashboard/Overview.tsx')).default;
const { RTCOverviewPanel } = require(path.join(root, 'src/features/rtc/RTCOverview.tsx'));
const { OverviewLink } = require(path.join(root, 'src/features/dashboard/OverviewUI.tsx'));
global.window = { location: { pathname: '/overview', search: '', hash: '' } };
const render = (Component, props) => renderToStaticMarkup(React.createElement(Component, props));
const text = html => html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
const api = {}, onNavigate = () => {};
const overview = (extra = {}) => render(Overview, { api, onNavigate, services: [], notifications: { enabled: true, pending: 0, delivered: 12, failed: 7, dropped: 0 }, ...extra });
const rtc = (value, extra = {}) => render(RTCOverviewPanel, { value, onNavigate, onRetry() {}, ...extra });
const healthy = { status: 'HEALTHY', configured_tasks: 2, enabled_tasks: 2, storage_available: true, last_success_at: '2026-11-20T10:00:00Z' };

test('stale healthy observations count as unknown without double counting', () => {
  const nodes = [{ status: 'HEALTHY' }, { status: 'UNHEALTHY' }, { status: 'UNKNOWN' }, { status: 'HEALTHY', stale: true }];
  const html = overview({ services: [{ cluster_code: 'core', service_code: 'ap', service_name: 'AP', status: 'UNHEALTHY', expected_node_count: 4, nodes }] });
  const value = text(html);
  assert.match(value, /容器检查正常\s+1\s*\/\s*4/);
  assert.match(value, /待关注容器\s+3\s+1 异常 · 2 待确认/);
});
test('empty scope has an explicit empty state and no zero-denominator health claim', () => {
  const html = overview({ canManageInventory: true });
  assert.match(text(html), /暂无监控容器/);
  assert.doesNotMatch(text(html), /容器检查正常\s+0\s*\/\s*0/);
  assert.match(html, /href="\/monitor\/discovery"/);
});
test('users without configuration permissions see only permitted entry points', () => {
  const html = overview();
  assert.doesNotMatch(html, /href="\/monitor\/discovery"|href="\/notifications\/config"/);
  assert.match(html, /href="\/notifications\/delivery"/);
  assert.match(html, /href="\/monitor\/containers"/);
});
test('historical failures remain cumulative and read failures do not display healthy observations', () => {
  assert.match(text(overview()), /累计失败\s+7/);
  const degraded = text(overview({ snapshotUnavailable: true }));
  assert.match(degraded, /容器检查正常\s+—/);
  assert.match(degraded, /读取待确认/);
});
test('an unread first snapshot is not presented as an empty baseline', () => {
  const html = overview({ snapshotUnavailable: true, hasSnapshot: false, canManageInventory: true });
  assert.match(text(html), /容器监控数据暂不可读/);
  assert.match(text(html), /监控容器\s+—/);
  assert.match(text(html), /RTC 音视频业务/);
  assert.doesNotMatch(text(html), /暂无监控容器/);
  assert.doesNotMatch(html, /href="\/monitor\/discovery"/);
});
test('RTC storage failure uses a concise distinct status', () => {
  const html = rtc({ ...healthy, storage_available: false });
  assert.match(text(html), /存储不可用/);
  assert.doesNotMatch(text(html), /启用任务验证通过/);
});
test('RTC healthy, empty, disabled and unavailable results retain distinct meanings', () => {
  assert.match(rtc(healthy), /is-healthy/);
  assert.match(text(rtc(healthy)), /正常/);
  for (const value of [{ ...healthy, configured_tasks: 0 }, { ...healthy, enabled_tasks: 0 }, { ...healthy, storage_available: false }]) {
    assert.doesNotMatch(rtc(value), /overview-rtc-result is-healthy/);
  }
  const error = rtc(healthy, { error: '暂时无法读取' });
  assert.match(text(error), /读取失败/);
  assert.doesNotMatch(text(error), /启用任务验证通过|历史最近成功/);
});
test('RTC drill-down links preserve real navigable URLs', () => {
  const html = rtc(healthy);
  assert.match(html, /href="\/rtc\/tasks"/);
  assert.match(html, /href="\/rtc\/history"/);
});
test('overview links preserve modifier keys and use navigation on a primary click', () => {
  const routes = [], link = OverviewLink({ route: { view: 'rtc', page: 'tasks' }, children: '探测点任务配置', onNavigate: route => routes.push(route) });
  for (const props of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]) {
    link.props.onClick({ button: 0, ...props, preventDefault() { assert.fail('new-tab click was intercepted'); } });
  }
  assert.equal(routes.length, 0);
  let prevented = false;
  link.props.onClick({ button: 0, preventDefault() { prevented = true; } });
  assert.equal(prevented, true);
  assert.deepEqual(routes, [{ view: 'rtc', page: 'tasks' }]);
});

test('overview removes explanatory copy and individual delivery rows', () => {
  const html = overview({notifications:{enabled:true,pending:2,delivered:12,failed:7,dropped:1,recent:[{status:'DELIVERED',event:{event_id:'sample',node_id:'111.230.192.59-avops-v1-canary',occurred_at:'2026-11-20T00:23:02Z'}}]}});
  const copy = text(html)+text(rtc(healthy));
  for(const removed of ['尚未建立纳管范围','纳管后展示检查结果','等待或正在投递','启用任务的有效音视频证据通过当前判定','结论以当前覆盖与有效媒体证据为准','失败与丢弃为累计统计','111.230.192.59-avops-v1-canary','最近投递','容器检查、RTC 音视频证据与通知投递分别判定']) assert.ok(!copy.includes(removed),removed);
  assert.match(copy,/累计失败\s+7/);
});
test('record and configuration actions sit in their own cards with real URLs', () => {
  const html = overview({canConfigureNotifications:true});
  const actions = html.split('aria-label="通知投递操作"')[1].split('</section>')[0];
  assert.match(actions,/投递记录/);assert.match(actions,/接收配置/);assert.match(actions,/href="\/notifications\/config"/);
  const rtcActions = rtc(healthy).split('aria-label="RTC 业务操作"')[1].split('</section>')[0];
  assert.match(rtcActions,/探测点任务配置/);assert.match(rtcActions,/执行记录/);assert.match(rtcActions,/href="\/rtc\/history"/);
  assert.doesNotMatch(html+rtc(healthy),/overview-panel-footer/);
});
test('container monitoring leads with service and uses explicit count columns', () => {
  const html=overview({services:[{cluster_code:'primary',group_name:'核心集群',service_code:'ap',service_name:'接入服务',expected_node_count:2,nodes:[{status:'HEALTHY'},{status:'UNKNOWN'}]}]});
  const copy=text(html);
  assert.match(copy,/容器监控/);assert.match(copy,/服务 \/ 分组/);assert.match(copy,/容器数量/);assert.ok(copy.indexOf('接入服务')<copy.lastIndexOf('核心集群'));
  assert.doesNotMatch(copy,/纳管范围|服务状态|检查分布/);
});
