// URL round trips simulate fresh tabs: only pathname/search are supplied, with no browser state.
const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const req = Module.createRequire(path.resolve(__dirname, '../frontend/package.json'));
const ts = req('typescript');
const mod = new Module(path.resolve(__dirname, '../frontend/src/app/routes.ts'), module);
mod._compile(ts.transpileModule(fs.readFileSync(mod.id, 'utf8'), {compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText, mod.id);
const {readLocationParams,rtcHref,rtcRunHref,canonicalHref,workspaceHref} = mod.exports;
const location = href => new URL(href, 'https://monitor.example');
const empty = location('/');
const values = params => Object.fromEntries([...params].filter(([key])=>key.startsWith('rtc_')).map(([key,value])=>[key.slice(4),value]));

test('every history level is self-contained and round-trips in a fresh location', () => {
  for (const [href, expected] of [
    ['/rtc/history', {}],
    ['/rtc/history/round-1', {run:'round-1'}],
    ['/rtc/history/round-1/paths', {run:'round-1',paths:'1'}],
    ['/rtc/history/round-1/paths/case-1', {parent:'round-1',run:'case-1',paths:'1'}],
    ['/rtc/history/round-1/paths/case-1/logs', {parent:'round-1',run:'case-1',paths:'1',tab:'logs'}],
    ['/rtc/history/round-1/logs', {run:'round-1',tab:'logs'}],
  ]) {
    const params=readLocationParams(location(href));
    assert.equal(params.get('view'),'rtc'); assert.equal(params.get('page'),'history');
    assert.deepEqual(values(params),expected);
    assert.equal(canonicalHref({view:'rtc',page:'history'},location(href)),href);
  }
});

test('task paths open a case directly beneath its coverage category', () => {
  const href=rtcHref('tasks',{task:'audio-task',phase:'CROSS',parent:'round',run:'case',tab:'logs'},empty);
  assert.equal(href,'/rtc/tasks/audio-task/coverage/cross/paths/case/logs');
  const params=readLocationParams(location(href));
  assert.equal(params.get('rtc_task'),'audio-task');assert.equal(params.get('rtc_phase'),'CROSS');assert.equal(params.get('rtc_parent'),null);assert.equal(params.get('rtc_run'),'case');assert.equal(params.get('rtc_case'),'1');
  assert.equal(canonicalHref({view:'rtc',page:'tasks'},location(href)),href);
  assert.equal(rtcRunHref('case','round',location('/rtc/tasks/audio-task/coverage/cross')),'/rtc/tasks/audio-task/coverage/cross/paths/case');
});

test('native link hrefs enter children and return to parents without encoded source URLs', () => {
  const parent=location('/rtc/history/round');
  assert.equal(rtcRunHref('case',undefined,parent),'/rtc/history/round/paths/case');
  const child=location('/rtc/history/round/paths/case/logs');
  assert.equal(rtcRunHref('round',undefined,child),'/rtc/history/round');
  assert.equal(workspaceHref({view:'rtc',page:'history'},child),'/rtc/history');
});

test('legacy nested query links migrate with original history/task context', () => {
  const source='?view=rtc&page=history&rtc_run=round';
  const legacy=location('/?view=rtc&page=history&rtc_run=case&rtc_tab=logs&rtc_from='+encodeURIComponent(source));
  assert.equal(canonicalHref({view:'rtc',page:'history'},legacy),'/rtc/history/round/paths/case/logs');
  const taskSource='?view=rtc&page=tasks&rtc_task=audio&rtc_phase=DIRECT';
  const parent='?view=rtc&page=tasks&rtc_run=round&rtc_from='+encodeURIComponent(taskSource);
  const child=location('/?view=rtc&page=tasks&rtc_run=case&rtc_from='+encodeURIComponent(parent));
  assert.equal(canonicalHref({view:'rtc',page:'tasks'},child),'/rtc/tasks/audio/coverage/direct/paths/case');
});

test('IDs are encoded as individual path segments', () => {
  const href=rtcHref('history',{parent:'round /一',run:'case #?/'},empty);
  const params=readLocationParams(location(href));
  assert.equal(params.get('rtc_parent'),'round /一');assert.equal(params.get('rtc_run'),'case #?/');
  assert.ok(!href.includes('?'));assert.ok(!href.includes('#'));
});

test('workspace links and guarded fallbacks never leak a previous RTC detail', () => {
  const old=location('/?view=rtc&page=history&rtc_run=old&rtc_from=untrusted&range=24h');
  assert.equal(workspaceHref({view:'accounts',page:'users'},old),'/accounts/users?range=24h');
  assert.equal(canonicalHref({view:'overview',page:''},old),'/overview?range=24h');
  assert.equal(canonicalHref({view:'rtc',page:'settings'},location('/?view=rtc&page=retention')),'/rtc/settings/retention');
  for (const [href,view,page] of [['/overview','overview',''],['/monitor/containers','status',''],['/inventory/baseline','status',''],['/notifications/config','notifications','config'],['/accounts/roles','accounts','roles'],['/rtc/targets/service','rtc','targets']]) {
    const params=readLocationParams(location(href));assert.equal(params.get('view'),view);assert.equal(params.get('page')||'',page);
  }
  assert.equal(readLocationParams(location('/rtc')).get('page'),'tasks');
  assert.equal(readLocationParams(location('/rtc/history/run/unexpected')).get('view'),'invalid');
});

test('all coverage types canonicalize old long task paths without encoding a parent execution',()=>{
 for(const phase of ['ap','direct','cross']){
  const base='/rtc/tasks/audio/coverage/'+phase;
  for(const suffix of ['','/logs']){
   const short=base+'/paths/case'+suffix;
   assert.equal(canonicalHref({view:'rtc',page:'tasks'},location(base+'/runs/round/paths/case'+suffix)),short);
   assert.equal(canonicalHref({view:'rtc',page:'tasks'},location(short)),short);
   assert.equal(readLocationParams(location(short)).get('rtc_run'),'case');
   assert.equal(readLocationParams(location(short)).get('rtc_parent'),null);
  }
 }
});

test('monitor discovery and merged membership routes canonicalize without losing filters',()=>{
 for(const path of ['/monitor/discovery','/inventory/discovery']){
  const params=readLocationParams(location(path+'?name=agora'));
  assert.equal(params.get('view'),'inventory');assert.equal(params.get('page'),'discovery');
  assert.equal(canonicalHref({view:'inventory',page:'discovery'},location(path+'?name=agora')),'/monitor/discovery?name=agora');
 }
 assert.equal(canonicalHref({view:'status',page:''},location('/inventory/baseline')),'/monitor/containers');
});
test('probe server management lives beneath runtime settings in fresh tabs',()=>{
 for(const path of ['/rtc/probes','/rtc/settings','/rtc/settings/servers']){
  const params=readLocationParams(location(path));assert.equal(params.get('page'),'settings');
  assert.equal(canonicalHref({view:'rtc',page:'settings'},location(path)),'/rtc/settings/servers');
 }
 assert.equal(rtcHref('probes',{},empty),'/rtc/settings/servers');
 assert.equal(rtcHref('settings',{setting:'retention'},empty),'/rtc/settings/retention');
 assert.equal(readLocationParams(location('/rtc/settings/invalid')).get('view'),'invalid');
});
