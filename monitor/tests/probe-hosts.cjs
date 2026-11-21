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
const ProbeHosts = require(path.join(root, 'src/features/rtc/ProbeHosts.tsx')).default;
const { taskRuntime } = require(path.join(root, 'src/features/rtc/RTCModel.tsx'));
const runtime = (id, host, service, client) => ({runtime_id: id, host_id: host, host_name: host, service_type: service, client_type: client, online: true, enabled: true});
const native = runtime('n', 'server-a', 'rtc', 'native'), web = runtime('w', 'server-a', 'rtc', 'web');
const render = hosts => renderToStaticMarkup(React.createElement(ProbeHosts, {hosts, canManage: true, busy: '', operate() {assert.fail('render must not operate a client');}}));
test('Native and Web share one server row and retain independent status', () => {
  const html = render([{host_id:'server-a', host_name:'Server Alpha', host_address:'192.0.2.1', online:true, services:[{service_type:'rtc', clients:[native, {...web, enabled:false}]}]}]);
  assert.equal((html.match(/<tbody>/g)||[]).length, 1);
  const body=html.split('<tbody>')[1].split('</tbody>')[0];
  assert.equal((body.match(/<tr/g)||[]).length, 1);
  const text=body.replace(/<[^>]+>/g,' ');
  assert.match(text, /Server Alpha/);assert.match(text,/Native.*在线/);assert.match(text,/Web.*已停用/);
  assert.doesNotMatch(html,/登记探针|生成凭证|probe_id|runtime_secret|host_secret/);
});
test('future service and client advertisements remain visible', () => {
  const html=render([{host_id:'server-a',host_name:'Server Alpha',online:false,services:[{service_type:'rtm',clients:[runtime('p','server-a','rtm','python')]}]}]);
  assert.match(html,/RTM/);assert.match(html,/python/);
});
test('task selection requires the exact server, service and client combination', () => {
  const other=runtime('other','server-b','rtc','native'), future=runtime('future','server-a','rtm','native');
  const snapshot={runtimes:[other,future,web,native]};
  assert.equal(taskRuntime({host_id:'server-a',service_type:'rtc',client_type:'native'},snapshot),native);
  assert.equal(taskRuntime({host_id:'server-a',service_type:'rtc',client_type:'web'},snapshot),web);
  assert.equal(taskRuntime({host_id:'server-b',service_type:'rtc',client_type:'web'},snapshot),undefined);
  assert.equal(taskRuntime({host_id:'server-a',service_type:'rtc',client_type:''},snapshot),undefined);
});
const {rtcServerOptions,rtcTargetSupports}=require(path.join(root,'src/features/rtc/RTCModel.tsx'));
test('task creation lists only matching RTC clients and explains unavailable servers',()=>{
 const hosts=[{host_id:'server-a',host_name:'Alpha',host_address:'192.0.2.1',services:[{service_type:'rtc',clients:[native,{...web,enabled:false}]}]},{host_id:'server-b',host_name:'Beta',services:[{service_type:'rtm',clients:[runtime('x','server-b','rtm','native')]}]}];
 assert.equal(rtcServerOptions(hosts,'native').length,1);
 assert.equal(rtcServerOptions(hosts,'web')[0].disabled,true);
 assert.match(rtcServerOptions(hosts,'web')[0].label,/已停用/);
 assert.equal(rtcServerOptions(hosts,'web','server-a')[0].disabled,false);
});
test('client selection excludes targets with only the other capability',()=>{
 const nativeOnly={driver:'agora',rtc:{native_coverage:{}}},webOnly={driver:'agora',rtc:{web_access:{}}};
 assert.equal(rtcTargetSupports(nativeOnly,'native'),true);
 assert.equal(rtcTargetSupports(nativeOnly,'web'),false);
 assert.equal(rtcTargetSupports(webOnly,'native'),false);
 assert.equal(rtcTargetSupports(webOnly,'web'),true);
});

test('interoperability requires both clients and uses their shared capacity',()=>{
 const both=[{host_id:'server-a',host_name:'Alpha',services:[{service_type:'rtc',clients:[{...native,capacity:8},{...web,capacity:2}]}]}];
 assert.equal(rtcServerOptions(both,'interop').length,1);
 assert.equal(rtcServerOptions([{...both[0],services:[{service_type:'rtc',clients:[native]}]}],'interop').length,0);
 const combined=taskRuntime({host_id:'server-a',service_type:'rtc',client_type:'interop'},{runtimes:both[0].services[0].clients});
 assert.equal(combined.capacity,2);assert.equal(combined.online,true);
 assert.equal(rtcTargetSupports({driver:'agora',rtc:{native_coverage:{},web_access:{coverage:{}}}},'interop'),true);
 assert.equal(rtcTargetSupports({driver:'agora',rtc:{native_coverage:{}}},'interop'),false);
});

test('combined interoperability requires both transport capabilities and one task label',()=>{
 const capabilities=['native_web_duplex_v1','native_web_interop_v1','native_aut_web_interop_v1'];
 const clients=[{...native,capacity:8,capabilities},{...web,capacity:2,capabilities}];
 const hosts=[{host_id:'server-a',host_name:'Alpha',services:[{service_type:'rtc',clients}]}];
 assert.equal(rtcServerOptions(hosts,'interop')[0].disabled,false);
 assert.match(rtcServerOptions(hosts,'interop')[0].label,/Native ↔ Web/);
 assert.equal(taskRuntime({host_id:'server-a',service_type:'rtc',client_type:'interop'},{runtimes:clients}).capacity,2);
 hosts[0].services[0].clients=[clients[0],{...web,capabilities:['native_web_interop_v1']}];
 assert.equal(rtcServerOptions(hosts,'interop')[0].disabled,true);
 assert.match(rtcServerOptions(hosts,'interop')[0].label,/需升级/);
});

test('one interop path list clearly names UDP and AUT in both directions',()=>{
 const {casePath}=require(path.join(root,'src/features/rtc/RTCNativeCoverage.tsx'));
 const labels=[];
 for(const transport of ['udp','aut']) for(const sender of ['native','web']){
  labels.push(casePath({client_type:transport==='aut'?'interop_aut':'interop',case_kind:'AP_JOIN',sender_client:sender,receiver_client:sender==='native'?'web':'native',sender:{transport:sender==='native'?transport:'web'},receiver:{transport:sender==='native'?'web':transport},ap_entrypoint:'192.0.2.1'}));
 }
 assert.equal(new Set(labels).size,4);
 for(const label of ['Native UDP → Web','Web → Native UDP','Native AUT → Web','Web → Native AUT']) assert.ok(labels.some(value=>value.startsWith(label)));
});

test('duplex interop shows two protocol conclusions and both receive summaries',()=>{
 const {casePath,NativeCases}=require(path.join(root,'src/features/rtc/RTCNativeCoverage.tsx'));
 const cases=['udp','aut'].map(transport=>({case:{duplex:true,client_type:transport==='aut'?'interop_aut':'interop',case_key:transport,case_kind:'AP_JOIN',sender_client:'native',receiver_client:'web',sender:{transport},receiver:{transport:'web'},ap_entrypoint:'192.0.2.1'},status:'SUCCESS',sender_evidence:{media_duration_ms:1200,received_video_frames:20},receiver_evidence:{media_duration_ms:1300,received_video_frames:21}}));
 assert.match(casePath(cases[0].case),/Native UDP ↔ Web/);assert.match(casePath(cases[1].case),/Native AUT ↔ Web/);
 const html=renderToStaticMarkup(React.createElement(NativeCases,{run:{run_id:'duplex',cases}}));
 assert.equal((html.match(/data-label="验证路径"/g)||[]).length,2);assert.match(html,/Native 收/);assert.match(html,/Web 收/);
});
