// Non-browser checks for breadcrumb ancestry and native modified-click semantics.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),Module=require('node:module');
const {test}=require('node:test');
const req=Module.createRequire(path.resolve(__dirname,'../frontend/package.json')),ts=req('typescript');
const resolve=Module._resolveFilename;
Module._resolveFilename=function(name,parent,...rest){return resolve.call(this,name.startsWith('antd/es/')?name.replace('antd/es/','antd/lib/'):name,parent,...rest)};
for(const ext of ['.ts','.tsx'])require.extensions[ext]=(module,name)=>module._compile(ts.transpileModule(fs.readFileSync(name,'utf8'),{compilerOptions:{jsx:ts.JsxEmit.ReactJSX,module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText,name);
const {readLocationParams}=require('../frontend/src/app/routes.ts');
const {buildRTCBreadcrumbs,correctedRunHref}=require('../frontend/src/features/rtc/RTCBreadcrumbs.tsx');
const {NativeCases}=require('../frontend/src/features/rtc/RTCNativeCoverage.tsx');
const {followRTC,default:RTCLink}=require('../frontend/src/features/rtc/RTCLink.tsx');
const RTCRunDetails=require('../frontend/src/features/rtc/RTCRunDetails.tsx').default;
const React=req('react'),{renderToStaticMarkup}=req('react-dom/server');
const parent={run_id:'round',kind:'NATIVE_COVERAGE',native_plan:{phase:'AP'},endpoints:{}};
const child={run_id:'case',parent_run_id:'round',endpoints:{},native_case:{case_kind:'AP_JOIN',ap_entrypoint:'127.0.0.1',sender:{transport:'udp'},receiver:{transport:'udp'}}};
const data={tasks:[{task_id:'audio',name:'音视频探测'}],targets:[],runs:[]};
const at=path=>new URL(path,'https://monitor.example');

test('history execution, validation paths and child logs extend the breadcrumb in order',()=>{
 const loc=at('/rtc/history/round/paths/case/logs');
 const trail=buildRTCBreadcrumbs(readLocationParams(loc),data,child,parent,loc);
 assert.deepEqual(trail.map(i=>i.title),['AP 接入执行','验证路径','UDP · AP 127.0.0.1','诊断日志']);
 assert.deepEqual(trail.map(i=>i.href),['/rtc/history/round','/rtc/history/round/paths','/rtc/history/round/paths/case',undefined]);
});
test('task ancestry stays in its task/coverage scope and missing archived parents do not hide the leaf',()=>{
 const loc=at('/rtc/tasks/audio/coverage/ap/runs/round/paths/case');
 const trail=buildRTCBreadcrumbs(readLocationParams(loc),data,child,null,loc);
 assert.equal(trail[0].title,'音视频探测');assert.equal(trail[1].title,'AP 接入');assert.equal(trail.at(-1).title,'UDP · AP 127.0.0.1');
 assert.equal(trail.length,3);assert.equal(trail[2].href,undefined);assert.ok(!trail.some(i=>i.title==='验证路径'||i.title==='AP 接入执行'));
});
test('delayed normalization cannot move a history URL into a previously visited task or run',()=>{
 const oldLocation=at('/rtc/tasks/audio/coverage/direct/runs/old-round/paths/old-case');
 const oldParams=readLocationParams(oldLocation);
 const stale={...child,run_id:'old-case',parent_run_id:'old-round'};
 for(const href of ['/rtc/history','/rtc/history/cross-round','/rtc/history/cross-round/paths/cross-case','/rtc/targets/target','/overview']){
  assert.equal(correctedRunHref(oldParams,stale,at(href)),null,href);
  assert.equal(correctedRunHref(readLocationParams(at(href)),stale,at(href)),null,href);
 }
});
test('missing ancestry and retired parent log URLs are corrected only in their current scope',()=>{
 for(const phase of ['ap','direct','cross']){
  for(const prefix of ['/rtc/history','/rtc/tasks/audio/coverage/'+phase+'/runs']){
   const loc=at(prefix+'/case/logs');
   assert.equal(correctedRunHref(readLocationParams(loc),child,loc),prefix.startsWith('/rtc/tasks/')?'/rtc/tasks/audio/coverage/'+phase+'/paths/case/logs':prefix+'/round/paths/case/logs');
   const parentLogs=at(prefix+'/round/logs');
   assert.equal(correctedRunHref(readLocationParams(parentLogs),parent,parentLogs),prefix+'/round');
  }
 }
 const explicit=at('/rtc/history/requested-round/paths/case');
 assert.equal(correctedRunHref(readLocationParams(explicit),child,explicit),null);
});
test('history cross-edge links and breadcrumb labels retain their record context',()=>{
 const run={...parent,run_id:'cross-round',native_plan:{phase:'CROSS'}};
 const leaf={...child,run_id:'cross-case',parent_run_id:run.run_id};
 const loc=at('/rtc/history/cross-round/paths/cross-case/logs');
 const trail=buildRTCBreadcrumbs(readLocationParams(loc),data,leaf,run,loc);
 assert.equal(trail[0].title,'跨 Edge 互通执行');
 assert.ok(trail.every(i=>!i.href || i.href.startsWith('/rtc/history/')));
 assert.ok(!trail.some(i=>i.title==='音视频探测'||i.title==='Edge 直连'));
 const staleTrail=buildRTCBreadcrumbs(readLocationParams(loc),data,{...child,run_id:'old-case'},parent,loc);
 assert.ok(!staleTrail.some(i=>i.title==='AP 接入执行'));
});
test('case clicks follow the href rendered for history even if the live location changes',()=>{
 const savedWindow=global.window,savedDocument=global.document,savedEvent=global.PopStateEvent;
 try{
  global.window={location:at('/rtc/history/cross-round')};
  const tree=NativeCases({run:{...parent,run_id:'cross-round',cases:[{run_id:'cross-case',status:'SUCCESS',case:{...child.native_case,case_key:'cross'}}]}});
  let link;
  const walk=node=>{if(!React.isValidElement(node))return;if(node.type===RTCLink)link=node;React.Children.forEach(node.props.children,walk);};
  walk(tree);assert.ok(link);
  assert.equal(link.props.href,'/rtc/history/cross-round/paths/cross-case');
  assert.equal(link.props.onNavigate,undefined);
  let pushed;
  global.window={location:at('/rtc/tasks/audio/coverage/direct'),history:{pushState(_,__,href){pushed=href;}},dispatchEvent(){},scrollTo(){},requestAnimationFrame(fn){fn();}};
  global.document={getElementById(){return null;}};global.PopStateEvent=class {};
  RTCLink(link.props).props.onClick({button:0,preventDefault(){}});
  assert.equal(pushed,'/rtc/history/cross-round/paths/cross-case');
 }finally{global.window=savedWindow;global.document=savedDocument;global.PopStateEvent=savedEvent;}
});
test('parent executions show validation paths without diagnostic tabs, including old log URLs',()=>{
 for(const kind of ['NATIVE_COVERAGE','EDGE_SWEEP']){
  const run={...parent,kind,status:'SUCCESS',cases:[],edges:[],logs:{}};
  const loc=at('/rtc/history/round/logs');
  global.window={location:loc};
  const html=renderToStaticMarkup(React.createElement(RTCRunDetails,{run,data,api:{},onBack(){},onOpen(){},operate:async()=>{},busy:'',canManage:true,canDiagnose:true,tab:'logs',onTab(){}}));
  assert.match(html,/验证路径/);
  assert.doesNotMatch(html,/诊断日志|本次诊断包|回捞各用例日志|role="tab"/);
  const trail=buildRTCBreadcrumbs(readLocationParams(loc),data,run,null,loc);
  assert.deepEqual(trail.map(i=>i.title),['AP 接入执行']);
 }
});
test('individual validation paths retain diagnostic tabs and log retrieval',()=>{
 global.window={location:at('/rtc/history/round/paths/case/logs')};
 const run={...child,status:'SUCCESS',logs:{}};
 const html=renderToStaticMarkup(React.createElement(RTCRunDetails,{run,data,api:{},onBack(){},onOpen(){},operate:async()=>{},busy:'',canManage:true,canDiagnose:true,tab:'logs',onTab(){}}));
 assert.match(html,/诊断日志/);
 assert.match(html,/回捞诊断日志/);
 assert.match(html,/本次诊断包/);
});
test('navigation controls render real hrefs for browser open-in-new-tab behavior',()=>{
 const html=renderToStaticMarkup(React.createElement(RTCLink,{href:'/rtc/history/round/paths/case'},'查看'));
 assert.match(html,/<a[^>]+href="\/rtc\/history\/round\/paths\/case"/);
 const disabled=renderToStaticMarkup(React.createElement(RTCLink,{disabled:true,href:'/rtc/history/invalid'},'查看'));
 assert.ok(!disabled.includes('href='));assert.ok(disabled.includes('disabled'));
});
test('Ctrl, Command, Shift, Alt and middle click keep native browser behavior',()=>{
 for(const modifiers of [{ctrlKey:true},{metaKey:true},{shiftKey:true},{altKey:true},{button:1}]){
  let intercepted=false,navigated=false;
  followRTC({button:0,...modifiers,preventDefault(){intercepted=true}},'/rtc/history/round',()=>{navigated=true});
  assert.equal(intercepted,false);assert.equal(navigated,false);
 }
 let intercepted=false,navigated=false;
 followRTC({button:0,preventDefault(){intercepted=true}},'/rtc/history/round',()=>{navigated=true});
 assert.equal(intercepted,true);assert.equal(navigated,true);
});

test('task breadcrumbs go from category directly to the selected path and its logs',()=>{
 for(const phase of ['AP','DIRECT','CROSS']){
  const loc=at('/rtc/tasks/audio/coverage/'+phase.toLowerCase()+'/paths/case/logs');
  const root={...parent,native_plan:{phase}};
  const trail=buildRTCBreadcrumbs(readLocationParams(loc),data,child,root,loc);
  assert.deepEqual(trail.map(i=>i.title),['音视频探测',({AP:'AP 接入',DIRECT:'Edge 直连',CROSS:'跨 Edge 互通'})[phase],'UDP · AP 127.0.0.1','诊断日志']);
  assert.equal(trail[1].href,'/rtc/tasks/audio/coverage/'+phase.toLowerCase());
  assert.equal(trail[2].href,'/rtc/tasks/audio/coverage/'+phase.toLowerCase()+'/paths/case');
  assert.equal(correctedRunHref(readLocationParams(loc),child,loc),null);
 }
});
