import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import readline from 'node:readline';
import {fileURLToPath} from 'node:url';
import {randomBytes,createHash} from 'node:crypto';
import {chromium} from 'playwright';
import {startRuntimeServer} from './server.mjs';
import {sdkParameters,signalingRoute,browserFailureReason,publishWithEvidence} from './contract.mjs';
import {remoteStatsSample,evaluateRemoteStats} from './remote-stats.mjs';

const here=path.dirname(fileURLToPath(import.meta.url)), id=()=>randomBytes(16).toString('hex');
const input=readline.createInterface({input:process.stdin,crlfDelay:Infinity});
let job,started=false,startResolve;
const begin=new Promise(resolve=>startResolve=resolve),abort=new AbortController();
const stop=reason=>{if(!abort.signal.aborted)abort.abort(new Error(reason));};
input.on('line',line=>{
 try{const message=JSON.parse(line);if(message.protocol_version!==2)return;
  if(!job){if(message.type!=='prepare_run'||!message.job)throw new Error('initial assignment required');job=message.job;run().catch(error=>{process.stderr.write(String(error.stack||error)+'\n');process.exitCode=1;input.close();});return;}
  if(message.run_id!==job.run_id||message.config_revision!==job.config_revision)return;
  if(message.type==='start_media'){started=true;startResolve();}else if(message.type==='cancel_run')stop('CANCELLED');
 }catch(error){stop('INVALID_CONTROL');}
});
input.on('close',()=>stop('CONTROL_CLOSED'));
process.on('SIGTERM',()=>stop('CANCELLED'));process.on('SIGINT',()=>stop('CANCELLED'));

async function run(){
 const worker=id(),at=Date.now(),work=job.work_dir,rtc=job.rtc;
 if(!work||!path.isAbsolute(work)||!(job.endpoint_role==='pair'||['interop','interop_aut'].includes(job.native_case?.client_type)&&['sender','receiver'].includes(job.endpoint_role))||job.target?.driver!=='agora-web'||!/^[0-9a-f]{32}$/i.test(rtc?.app_id||''))throw new Error('invalid Web assignment');
 const duplex=job.native_case?.duplex===true;
 const roles=job.endpoint_role==='pair'?['sender','receiver']:[job.endpoint_role];
 const deadline=Date.parse(job.deadline),timeout=setTimeout(()=>stop('DEADLINE'),Math.max(1,deadline-Date.now()));
 const sdkDir=path.join(work,'sdk');await fsp.mkdir(sdkDir,{recursive:true,mode:0o700});
 const tmp=path.join(work,'browser-tmp');await fsp.mkdir(tmp,{recursive:true,mode:0o700});process.env.TMPDIR=tmp;
 for(const role of roles)await fsp.mkdir(path.join(work,role),{recursive:true,mode:0o700});
 const secrets=[job.credentials?.sender_token,job.credentials?.receiver_token].filter(Boolean);
 const redact=text=>{
  let value=String(text);for(const secret of secrets)value=value.replaceAll(secret,'[REDACTED]');
  value=value.replace(/([?&](?:token|ticket|key|icePwd)=)[^&\s]+/gi,'$1[REDACTED]');
  return value.replace(/(["']?(?:token|ticket|icePwd|credential|password|key)["']?\s*[:=]\s*)(["'])(.*?)\2/gi,'$1"[REDACTED]"');
 };
 let seq=0,traceComplete=true,phase='BROWSER_SETUP',browser,server,browserServer,cleanup='SUCCESS',before={},sockets={sender:[],receiver:[]},requests={sender:[],receiver:[]},browserVersion='';
 const stages=[],phaseAt={value:at},sdkSamples=Object.fromEntries(roles.map(role=>[role,[]])),sdkBaseline={};
 const peerUID=role=>role==='sender'?'10002':'10001';
 const advance=next=>{stages.push({stage:phase,started_elapsed_ms:phaseAt.value-at,duration_ms:Date.now()-phaseAt.value});phase=next;phaseAt.value=Date.now();};
 const output=message=>fs.writeSync(3,JSON.stringify({protocol_version:2,message_id:id(),run_id:job.run_id,config_revision:job.config_revision,...message})+'\n');
 function record(role,entry){
  const event={...entry,event_id:id(),worker_id:worker,source_seq:++seq,run_id:job.run_id,parent_run_id:job.parent_run_id||'',edge_endpoint:job.edge_endpoint||'',task_id:job.task_id,target_id:job.target.target_id,runtime_id:job.runtime_id,config_revision:job.config_revision,endpoint_role:role,occurred_at:entry.occurred_at||new Date().toISOString(),monotonic_elapsed_ms:Date.now()-at,stage:phase};
  if(phase==='MEDIA' && entry.kind==='sdk_callback' && (entry.name==='user-unpublished'||entry.name==='connection-state-change'&&entry.fields?.parameters?.[0]==='DISCONNECTED'))stop('WEB_MEDIA_INTERRUPTED');
  const clean=JSON.parse(redact(JSON.stringify(event)));
  try{fs.appendFileSync(path.join(work,role,'sdk-api.jsonl'),JSON.stringify(clean)+'\n',{mode:0o600});output({type:'telemetry_batch',endpoint_role:role,events:[clean]});}catch(error){traceComplete=false;throw error;}
 }
 const bounded=promise=>Promise.race([promise,new Promise((_,reject)=>{if(abort.signal.aborted)return reject(abort.signal.reason);abort.signal.addEventListener('abort',()=>reject(abort.signal.reason),{once:true});})]);
 const delay=ms=>bounded(new Promise(resolve=>setTimeout(resolve,ms)));
 const receiveBaseline={};
 const endpoints={};let outcome='SUCCESS',reason='MEDIA_VERIFIED';
 async function open(role){
  const context=await browser.newContext({viewport:{width:640,height:480},timezoneId:'Asia/Shanghai',ignoreHTTPSErrors:!!rtc.web_access.ignore_certificates});
  endpoints[role]={context};
  await context.addInitScript(()=>{
   window.__deviceCaptureCalls=0;window.__probePeerConnections=[];
   for(const name of ['getUserMedia','getDisplayMedia'])if(navigator.mediaDevices&&name in navigator.mediaDevices)navigator.mediaDevices[name]=async()=>{window.__deviceCaptureCalls++;throw new Error('Device capture is disabled');};
   const Original=window.RTCPeerConnection;window.RTCPeerConnection=class extends Original{constructor(...args){super(...args);window.__probePeerConnections.push(this);}};
  });
  await context.exposeBinding('__emitProbeEvent',(_source,event)=>record(role,event));
  const page=await context.newPage();endpoints[role].page=page;
  let mainNavigations=0,runtimeDocumentLoaded=false;
  page.on('framenavigated',frame=>{if(frame!==page.mainFrame())return;mainNavigations++;const url=new URL(frame.url());record(role,{kind:'stage_event',name:'browser_navigation',fields:{navigation_count:mainNavigations,origin:url.origin,expected_runtime:url.origin===new URL(server.url).origin}});if(runtimeDocumentLoaded&&phase!=='CLEANUP'&&phase!=='DONE')stop('BROWSER_CONTEXT_LOST');});
  page.on('crash',()=>{record(role,{kind:'stage_event',name:'browser_page_crashed'});stop('BROWSER_PAGE_CRASHED');});
  page.on('close',()=>{if(phase!=='CLEANUP'&&phase!=='DONE')stop('BROWSER_CONTEXT_LOST');});
  page.on('console',message=>fs.appendFileSync(path.join(sdkDir,'agora-web.log'),JSON.stringify({at:new Date().toISOString(),role,type:message.type(),text:redact(message.text())})+'\n',{mode:0o600}));
  page.on('pageerror',error=>record(role,{kind:'stage_event',name:'browser_error',fields:{message:redact(error.message)}}));
  page.on('requestfailed',request=>record(role,{kind:'stage_event',name:'request_failed',fields:{origin:new URL(request.url()).origin,error:request.failure()?.errorText}}));
  page.on('requestfinished',request=>{const url=new URL(request.url());if(url.protocol==='https:'&&(job.native_case?.ap_client_ips||rtc.access_ips).includes(url.hostname)&&Number(url.port||443)===rtc.web_access.port){requests[role].push(url.origin);record(role,{kind:'stage_event',name:'private_ap_request',fields:{origin:url.origin,path:url.pathname}});}});
  page.on('websocket',socket=>{
   const url=new URL(socket.url()),entry={endpoint:url.hostname+':'+(url.port||443),sent_bytes:0,received_bytes:0,connected:false};sockets[role].push(entry);
   for(const [event,key] of [['framesent','sent_bytes'],['framereceived','received_bytes']])socket.on(event,frame=>{entry.connected=true;entry[key]+=typeof frame.payload==='string'?Buffer.byteLength(frame.payload):frame.payload.length;});
   socket.on('close',()=>{entry.closed=true;});
   socket.on('socketerror',message=>record(role,{kind:'stage_event',name:'websocket_error',fields:{endpoint:entry.endpoint,message}}));
  });
  await page.goto(server.url,{waitUntil:'load',timeout:10000});runtimeDocumentLoaded=true;await page.addScriptTag({url:server.url+'monitor.js'});
  const parameters=sdkParameters(rtc,job.native_case,role);
  await page.evaluate(options=>{if(AgoraRTC.VERSION!=='4.24.9')throw new Error('WEB_SDK_VERSION_MISMATCH');AgoraRTC.setLogLevel(1);window.session=new RTCFileSession({...options,observeMedia:true,onEvent:event=>FileProbeMonitor.emit('stage_event',event.type,event)});},{parameters,mode:['interop','interop_aut'].includes(job.native_case?.client_type)?'live':'rtc',timeoutMs:Math.max(1000,deadline-Date.now())});
  return page;
 }
 async function snapshot(role){const value=await endpoints[role].page.evaluate(async()=>({session:session.snapshot(),network:await FileProbeMonitor.networkStats()}));return {...value,signaling:sockets[role].filter(s=>s.connected&&!s.closed).map(s=>({...s}))};}
 function received(role,state){
  const peer=role==='sender'?'10002':'10001',decoded=state?.session?.decodedMedia||{},base=receiveBaseline[role],prior=base?.session?.decodedMedia||{};
  const rawAudio=decoded[peer+'/audio'],rawVideo=decoded[peer+'/video'];
  const audio=rawAudio?{...rawAudio,samples:Math.max(0,rawAudio.samples-(prior[peer+'/audio']?.samples||0))}:undefined;
  const video=rawVideo?{...rawVideo,frames:Math.max(0,rawVideo.frames-(prior[peer+'/video']?.frames||0))}:undefined;
  return {audio,video,videoFrames:Math.max(video?.frames||0,(state?.network?.inboundVideoFrames||0)-(base?.network?.inboundVideoFrames||0)),audioPackets:Math.max(0,(state?.network?.inboundAudioPackets||0)-(base?.network?.inboundAudioPackets||0))};
 }

 try{
  const media=path.join(here,'fixtures/probe.webm');
  if(createHash('sha256').update(fs.readFileSync(path.join(here,'sdk/AgoraRTC_N.js'))).digest('hex')!=='63a04a823e6e589a6dd4384e2c175a8e6f70c313f6f58aa88fd92e436ced55fe')throw new Error('WEB_SDK_CHECKSUM_MISMATCH');
  record(roles[0],{kind:'worker_event',name:'browser_worker_started',fields:{sdk_version:'4.24.9',sdk_sha256:createHash('sha256').update(fs.readFileSync(path.join(here,'sdk/AgoraRTC_N.js'))).digest('hex'),media_sha256:createHash('sha256').update(fs.readFileSync(media)).digest('hex')}});
  server=await startRuntimeServer({mediaPath:media});

  browserServer=await bounded(chromium.launchServer({headless:true,timeout:Math.max(1000,Math.min(15000,deadline-Date.now())),executablePath:process.env.CHROME_PATH||undefined,env:{...process.env,TMPDIR:tmp},args:['--no-sandbox','--no-proxy-server','--autoplay-policy=no-user-gesture-required','--disable-background-networking','--disable-renderer-backgrounding','--disable-background-timer-throttling','--disable-backgrounding-occluded-windows',...(rtc.web_access.ignore_certificates?['--ignore-certificate-errors']:[])]}));
  await fsp.writeFile(path.join(work,'browser-process.json'),JSON.stringify({run_id:job.run_id,pid:browserServer.process().pid,profile_root:tmp}),{mode:0o600});
  browser=await chromium.connect(browserServer.wsEndpoint());browserVersion=browser.version();
  browser.on('disconnected',()=>{if(phase!=='CLEANUP')stop('BROWSER_DISCONNECTED');});
  record(roles[0],{kind:'worker_event',name:'browser_ready',fields:{browser_version:browser.version()}});
  advance('JOIN');
  for(const role of roles){
   const page=await bounded(open(role));
   await bounded(page.evaluate(options=>session.start(options),{appId:rtc.app_id,channel:job.channel,uid:role==='sender'?10001:10002,role:duplex||role==='sender'?'publisher':'subscriber',deferPublish:duplex||role==='sender',expectedPeerUID:role==='sender'?10002:10001,token:role==='sender'?job.credentials?.sender_token||'':job.credentials?.receiver_token||''}));
  }
  for(const role of roles)output({type:'run_ready',endpoint_role:role});
  advance('WAIT_START');await bounded(begin);
  if(deadline-Date.now()<rtc.media_duration_ms+(duplex?4000:1000))throw new Error('CONTROL_START_TOO_LATE');
  for(const role of roles)sdkBaseline[role]=remoteStatsSample(await bounded(snapshot(role)),peerUID(role),Date.now()-at);
  advance('PUBLISH');for(const role of roles)if(duplex||role==='sender'){
   const proof=await publishWithEvidence(()=>bounded(endpoints[role].page.evaluate(()=>session.publish())),async()=>(await bounded(snapshot(role))).session,role==='sender'?10001:10002,()=>!abort.signal.aborted);
   if(proof.recovered)record(role,{kind:'stage_event',name:'browser_publish_evaluation_recovered',fields:proof});
  }
  if(duplex)for(const role of roles){receiveBaseline[role]=await bounded(snapshot(role));sdkBaseline[role]=remoteStatsSample(receiveBaseline[role],peerUID(role),Date.now()-at);}
  advance('MEDIA');const beginMedia=Date.now();let lastSample=0;
  while(Date.now()-beginMedia<rtc.media_duration_ms){
   for(const role of roles){before[role]=await bounded(snapshot(role));sdkSamples[role].push(remoteStatsSample(before[role],peerUID(role),Date.now()-at));}
   if(Date.now()-lastSample>=1000){for(const role of roles){record(role,{kind:'media_summary',name:'browser_media',fields:before[role]});if(duplex||role==='receiver')record(role,{kind:'media_summary',name:'sdk_remote_media_stats',fields:sdkSamples[role].at(-1)});}lastSample=Date.now();}
   await delay(200);
  }
  for(const role of roles){before[role]=await bounded(snapshot(role));sdkSamples[role].push(remoteStatsSample(before[role],peerUID(role),Date.now()-at));}
  for(const role of roles)if(before[role].session.deviceCaptureCalls)throw new Error('DEVICE_CAPTURE_ATTEMPTED');
  for(const role of roles){
   const state=before[role],receives=duplex||role==='receiver',sends=duplex||role==='sender',peer=role==='sender'?'10002':'10001';
   if(receives){
    const verdict=evaluateRemoteStats(sdkSamples[role],peerUID(role),sdkBaseline[role]);
    record(role,{kind:'media_summary',name:'sdk_remote_media_verdict',fields:verdict});
    if(!verdict.accepted)throw new Error(verdict.reason);
    const {audio,videoFrames,audioPackets}=received(role,state),audioMS=(audio?.samples||0)*1000/(audio?.sampleRate||48000);
    if(audioMS<rtc.min_audio_ms||videoFrames<rtc.min_video_frames||audioPackets<1)throw new Error('WEB_MEDIA_RECEIVE_TIMEOUT');
   }
   if(sends&&(state.network.outboundAudioPackets<1||state.network.outboundVideoFrames<1))throw new Error('WEB_PUBLISH_INCOMPLETE');
  }
  if(duplex||job.endpoint_role==='sender'){advance('MEDIA_HOLD');await delay(3000);}

 }catch(error){
  const message=abort.signal.aborted?abort.signal.reason.message:error.message;
  reason=message==='DEADLINE'?(phase==='WAIT_START'?'CONTROL_START_TIMEOUT':phase==='JOIN'?'WEB_JOIN_TIMEOUT':'WEB_EXECUTION_TIMEOUT'):message;
  if(!/^[A-Z0-9_]+$/.test(reason))reason='WEB_'+phase+'_FAILED';
  if(!abort.signal.aborted)reason=browserFailureReason(error.message)||reason;
  if(error.name==='NotSupportedError'||/captureStream|Media file must/.test(error.message))reason='WEB_LOCAL_MEDIA_UNAVAILABLE';
  outcome=reason==='CANCELLED'||reason==='CONTROL_CLOSED'?'CANCELLED':['JOIN','MEDIA','PUBLISH'].includes(phase)&&!['BROWSER_DISCONNECTED','BROWSER_CONTEXT_LOST','BROWSER_PAGE_CRASHED','CONTROL_START_TOO_LATE','DEVICE_CAPTURE_ATTEMPTED','WEB_LOCAL_MEDIA_UNAVAILABLE'].includes(reason)?'FAILED':'ERROR';
  for(const role of roles)record(role,{kind:'stage_event',name:'stage_failed',fields:{reason,detail:redact(error.message)}});
  for(const role of roles)if(endpoints[role]?.page&&!before[role])before[role]=await Promise.race([snapshot(role).catch(()=>null),new Promise(resolve=>setTimeout(()=>resolve(null),500))]);
 }finally{
  advance('CLEANUP');
  for(const role of roles){
   const endpoint=endpoints[role];if(!endpoint)continue;
   try{await Promise.race([endpoint.page?.evaluate(async()=>{await session.destroy();return FileProbeMonitor.drain();}).then(ok=>{if(!ok)traceComplete=false;}),new Promise((_,reject)=>setTimeout(()=>reject(new Error('cleanup timeout')),2500))]);}catch{cleanup='FAILED';}
   await endpoint.context.close().catch(()=>{cleanup='FAILED';});
  }
  await browser?.close().catch(()=>{cleanup='FAILED';});await browserServer?.close().catch(()=>{cleanup='FAILED';});await server?.close();
  advance('DONE');clearTimeout(timeout);
 }
 if(cleanup!=='SUCCESS'||!traceComplete){outcome='ERROR';reason=cleanup!=='SUCCESS'?'BROWSER_CLEANUP_FAILED':'TRACE_INCOMPLETE';}
 for(const role of roles){
  const state=before[role],receiver=duplex||role==='receiver',sender=duplex||role==='sender',connected=state?.session?.state==='connected',{audio,video,videoFrames}=received(role,state);
  const {route,fallback,fallbackAttempted,inventoryKnown}=signalingRoute(job.native_case,role,state?.signaling||[],requests[role].length>0,sockets[role]);
  let roleOutcome=outcome,roleReason=reason;
  if(outcome==='SUCCESS'&&route.status!=='VERIFIED'){roleOutcome='ERROR';roleReason=fallback?'WEB_SIGNAL_PORT_FALLBACK':'WEB_SIGNAL_ROUTE_UNVERIFIED';}
  const evidence={connected, published:sender&&!!state?.network?.outboundAudioPackets,subscribed:receiver&&!!audio,video_published:sender&&!!state?.network?.outboundVideoFrames,video_subscribed:receiver&&!!video,sent_frames:!sender?0:state?.network?.outboundAudioPackets||0,sent_video_frames:!sender?0:state?.network?.outboundVideoFrames||0,valid_samples:receiver?audio?.samples||0:0,media_duration_ms:receiver?Math.floor((audio?.samples||0)*1000/(audio?.sampleRate||48000)):0,received_video_frames:receiver?videoFrames:0,video_width:video?.width||0,video_height:video?.height||0,browser_media_validated:receiver&&outcome==='SUCCESS'};
  output({type:'run_result',endpoint_role:role,result:{result_id:id(),worker_id:worker,driver:'agora-web',outcome:roleOutcome,reason:roleReason,cleanup,evidence,route,browser:{sdk_version:'4.24.9',browser_version:browserVersion,device_capture_calls:state?.session?.deviceCaptureCalls||0,ice_peers:state?.network?.peers||[],signaling_fallback:fallback,signaling_fallback_attempted:fallbackAttempted,signaling_inventory_known:inventoryKnown},finished_at:new Date().toISOString(),trace_complete:traceComplete,trace_dropped:0,stages}});
 }
 input.close();
}
