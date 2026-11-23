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
const {navigationSections}=require(path.join(root,'src/app/navigation.ts'));
const {StoragePanel}=require(path.join(root,'src/features/admin/StorageStatus.tsx'));
const html=(props)=>renderToStaticMarkup(React.createElement(StoragePanel,{onRefresh(){},...props}));
const text=value=>value.replace(/<[^>]+>/g,' ').replace(/\s+/g,' ');
const usage={backend:'postgres',database_bytes:2147483648,warning_bytes:1073741824,disk_capacity_bytes:107374182400,disk_available_bytes:85899345920,warnings:['DATABASE_SIZE_WARNING'],tables:[{name:'resource_samples',bytes:104857600},{name:'documents',bytes:104857600}],sessions:{checked_at:'2026-11-20T12:00:00Z',deleted_sessions:3,error:''}};
test('navigation consolidates discovery into monitor and server management into settings',()=>{
 const sections=navigationSections(['monitor.read','inventory.read','baseline.read'],false);
 const monitor=sections.find(section=>section.id==='monitor');assert.deepEqual(monitor.items.map(item=>item.label),['容器服务','部署服务容器发现','日志拉取']);
 assert.ok(!sections.some(section=>section.id==='inventory'));
 const probes=sections.find(section=>section.id==='rtc');assert.equal(probes.label,'部署服务可用性探测');assert.ok(!probes.items.some(item=>item.route.page==='probes'));
 assert.ok(probes.items.some(item=>item.label==='运行设置'));
 const readonly=navigationSections(['monitor.read'],false);assert.equal(readonly.find(section=>section.id==='monitor').items.length,2);
});
test('capacity keeps database warning separate from physical disk utilization',()=>{
 const copy=text(html({usage}));assert.match(copy,/20\.0% 已使用/);assert.match(copy,/80\.0%/);assert.match(copy,/数据库占用达到提醒阈值/);assert.match(copy,/容量提醒阈值不设硬配额/);assert.doesNotMatch(copy,/200%/);
});
test('capacity unavailable disk and failed refresh do not display healthy observations',()=>{
 const copy=text(html({usage:{...usage,disk_capacity_bytes:0,disk_available_bytes:0},error:'读取失败'}));assert.match(copy,/容量暂不可读|无法读取数据盘容量/);assert.match(copy,/显示最近一次读取的数据/);assert.doesNotMatch(copy,/运行正常|0\.0% 已使用/);
 const empty=text(html({usage:{...usage,tables:[]}}));assert.match(empty,/暂无数据表统计/);assert.doesNotMatch(empty,/NaN|Infinity/);
});

const Operations=require(path.join(root,'src/features/operations/Operations.tsx')).default;
test('operations keeps one heading without the repeated description',()=>{
 const output=renderToStaticMarkup(React.createElement(Operations,{operations:[],services:[],canAudit:true,canResolve:false,onOpen(){},onResolve(){},onRefresh(){}}));
 assert.equal((output.match(/操作与审计/g)||[]).length,1);
 assert.doesNotMatch(output,/追踪容器重启结果与执行过程|当前最近|读取于/);
});
