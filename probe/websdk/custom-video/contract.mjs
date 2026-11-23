import {isIP} from 'node:net';
export function edgeEndpoint(edge){
 if(edge?.edge_type===undefined)return edge?.endpoint||'';
 const keys=Object.keys(edge).sort();
 if(JSON.stringify(keys)!==JSON.stringify(['edge_type','ip_for_client','user_port'])||!['udp','aut','web'].includes(edge.edge_type))throw new Error('INVALID_EDGE_ADDRESS');
 const ip=edge.ip_for_client,port=edge.user_port;
 if(ip===''&&port===0)return '';
 if(typeof ip!=='string'||!isIP(ip)||!Number.isInteger(port)||port<1||port>65535)throw new Error('INVALID_EDGE_ADDRESS');
 return `${isIP(ip)===6?'['+ip+']':ip}:${port}`;
}
export function sdkParameters(rtc, routeCase, role){
 const ips=routeCase?.ap_client_ips||rtc.access_ips;
 if(!Array.isArray(ips)||!ips.length)throw new Error('WEB_AP_IP_REQUIRED');
 const web=rtc.web_access;
 const parameters={JOIN_WITH_FALLBACK_SIGNAL_PROXY:false,JOIN_WITH_FALLBACK_MEDIA_PROXY:false,CONNECT_GATEWAY_WITHOUT_DOMAIN:true,WEBCS_DOMAIN:ips.map(ip=>ip+':'+web.port),WEBCS_DOMAIN_BACKUP_LIST:ips.map(ip=>ip+':'+web.port),EVENT_REPORT_DOMAIN:ips[0]+':'+web.report_port,EVENT_REPORT_BACKUP_DOMAIN:ips[0]+':'+web.report_port,LOG_UPLOAD_SERVER:[ips[0]+':'+web.log_port]};
 const endpoint=edgeEndpoint(routeCase?.[role]);
 if(endpoint){const url=new URL('wss://'+endpoint);parameters.GATEWAY_ADDRESS=[{ip:url.hostname.replace(/^\[|\]$/g,''),port:Number(url.port||443)}];}
 return parameters;
}
export function signalingRoute(routeCase,role,active,apObserved,attempts=active){
 const expected=edgeEndpoint(routeCase?.[role]),candidates=(routeCase?.candidates||[]).map(edgeEndpoint);
 const observed=active.filter(value=>value.connected&&!value.closed);
 const verified=observed.length>0&&observed.every(value=>expected?value.endpoint===expected:true);
 const inventoryKnown=observed.every(value=>!candidates.length||candidates.includes(value.endpoint));
 const fallback=!!expected&&observed.some(value=>value.endpoint===new URL('wss://'+expected).hostname+':443')&&!verified;
 const fallbackAttempted=!!expected&&new URL('wss://'+expected).port!==''&&attempts.some(value=>value.endpoint===new URL('wss://'+expected).hostname+':443');
 return {fallback,fallbackAttempted,inventoryKnown,route:{status:verified&&(routeCase?.case_kind!=='AP_JOIN'||apObserved)?'VERIFIED':'MISMATCH',guarded:false,scope:routeCase?.case_kind==='AP_JOIN'?'web_ap_assigned_signaling':'web_wss_signaling',expected,observed:observed.map(value=>({endpoint:value.endpoint,protocol:'wss',sent_bytes:value.sent_bytes,received_bytes:value.received_bytes})),ap_observed:apObserved}};
}

export function browserFailureReason(message) {
 if (/page (?:has )?crashed/i.test(message)) return 'BROWSER_PAGE_CRASHED';
 if (/execution context was destroyed|cannot find context with specified id|target (?:page, context or browser has been closed|closed)/i.test(message)) return 'BROWSER_CONTEXT_LOST';
 return '';
}

// A CDP evaluation can lose its reply after publish has already completed.
// Confirm the same live publisher's state; never repeat the SDK publication.
export async function publishWithEvidence(publish, readSession, uid, canRecover=()=>true) {
 try { await publish(); return {recovered:false}; }
 catch(error) {
  if(browserFailureReason(error.message)!=='BROWSER_CONTEXT_LOST'||!canRecover())throw error;
  const state=await readSession().catch(()=>null);
  const kinds=new Set((state?.localTracks||[]).filter(track=>track.readyState==='live').map(track=>track.kind));
  if(!canRecover()||state?.state!=='connected'||String(state.uid)!==String(uid)||!state.events?.some(event=>event.type==='published')||!kinds.has('audio')||!kinds.has('video'))throw error;
  return {recovered:true,reason:'SDK_PUBLISH_ALREADY_COMPLETED'};
 }
}
