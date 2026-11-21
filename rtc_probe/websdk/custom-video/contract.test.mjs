import {test} from 'node:test';
import assert from 'node:assert/strict';
import {sdkParameters,signalingRoute,edgeEndpoint,browserFailureReason} from './contract.mjs';
const rtc={access_ips:['183.131.59.146'],web_access:{port:443,report_port:6443,log_port:6444}};
const route={case_kind:'CROSS_EDGE_MEDIA',ap_client_ips:rtc.access_ips,sender:{endpoint:'183.131.59.146:4501'},receiver:{endpoint:'183.131.59.146:4504'}};
test('browser execution failures remain distinct from RTC publish failures',()=>{
 assert.equal(browserFailureReason('page.evaluate: Execution context was destroyed, most likely because of a navigation.'),'BROWSER_CONTEXT_LOST');
 assert.equal(browserFailureReason('Target page, context or browser has been closed'),'BROWSER_CONTEXT_LOST');
 assert.equal(browserFailureReason('page crashed'),'BROWSER_PAGE_CRASHED');
 assert.equal(browserFailureReason('AgoraRTCException: publish failed'), '');
});
test('private parameters precede join and isolate endpoint gateway addresses',()=>{
 const sender=sdkParameters(rtc,route,'sender'),receiver=sdkParameters(rtc,route,'receiver');
 assert.deepEqual(sender.GATEWAY_ADDRESS,[{ip:'183.131.59.146',port:4501}]);assert.deepEqual(receiver.GATEWAY_ADDRESS,[{ip:'183.131.59.146',port:4504}]);
 assert.equal(typeof receiver.GATEWAY_ADDRESS[0].port,'number');assert.deepEqual(sender.WEBCS_DOMAIN,['183.131.59.146:443']);assert.equal(sender.JOIN_WITH_FALLBACK_MEDIA_PROXY,false);assert.equal(sender.JOIN_WITH_FALLBACK_SIGNAL_PROXY,false);
});
test('closed requested socket cannot hide an active same-IP 443 fallback',()=>{
 const active=[{endpoint:'183.131.59.146:4501',connected:true,closed:true},{endpoint:'183.131.59.146:443',connected:true,closed:false}];
 const result=signalingRoute(route,'sender',active,true);assert.equal(result.route.status,'MISMATCH');assert.equal(result.fallback,true);
});
test('only the selected live WSS endpoint verifies the case',()=>{
 assert.equal(signalingRoute(route,'sender',[{endpoint:'183.131.59.146:4501',connected:true}],true).route.status,'VERIFIED');
 assert.equal(signalingRoute(route,'sender',[{endpoint:'183.131.59.146:4501',connected:false}],true).route.status,'MISMATCH');
});

test('AP assigned endpoint may be outside local inventory, explicit gateway stays strict',()=>{
 const assigned=[{endpoint:'183.131.59.232:4501',connected:true}];
 const ap={case_kind:'AP_JOIN',candidates:[{endpoint:'183.131.59.146:4501'}]};
 assert.equal(signalingRoute(ap,'sender',assigned,true).route.status,'VERIFIED');
 assert.equal(signalingRoute(ap,'sender',assigned,true).inventoryKnown,false);
 assert.equal(signalingRoute(ap,'sender',assigned,false).route.status,'MISMATCH');
 assert.equal(signalingRoute(route,'sender',assigned,true).route.status,'MISMATCH');
});

test('three-field Edge addresses preserve gateway selection and AP routing',()=>{
 const sender={edge_type:'web',ip_for_client:'183.131.59.146',user_port:4501};
 const receiver={edge_type:'web',ip_for_client:'183.131.59.146',user_port:4504};
 const compact={...route,sender,receiver};
 assert.deepEqual(sdkParameters(rtc,compact,'sender'),sdkParameters(rtc,route,'sender'));
 assert.deepEqual(sdkParameters(rtc,compact,'receiver').GATEWAY_ADDRESS,[{ip:'183.131.59.146',port:4504}]);
 assert.equal(signalingRoute(compact,'sender',[{endpoint:'183.131.59.146:443',connected:true}],true).route.status,'MISMATCH');
 const ap={case_kind:'AP_JOIN',sender:{edge_type:'web',ip_for_client:'',user_port:0},candidates:[sender]};
 assert.equal(sdkParameters(rtc,ap,'sender').GATEWAY_ADDRESS,undefined);
 assert.equal(signalingRoute(ap,'sender',[{endpoint:'183.131.59.146:4501',connected:true}],true).inventoryKnown,true);
 assert.equal(edgeEndpoint({edge_type:'web',ip_for_client:'2001:db8::1',user_port:4501}),'[2001:db8::1]:4501');
 for(const bad of [{...sender,user_port:'4501'},{...sender,user_port:0},{...sender,agent_id:'not-allowed'},{...sender,ip_for_client:'bad'}])assert.throws(()=>edgeEndpoint(bad));
});
