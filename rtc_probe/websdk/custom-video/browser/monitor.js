(() => {
  const raw = new WeakMap(), proxies = new WeakMap(), pending = new Set(), last = new Map(), observers = new Map();
  let sequence = 0, failures = 0;
  const start = performance.now();
  const unwrap = value => Array.isArray(value) ? value.map(unwrap) : raw.get(value) || value;
  const describe = (value, depth = 0) => {
    value = unwrap(value);
    if (value == null || ['number','boolean','string'].includes(typeof value)) return value;
    if (typeof value === 'function') return '[callback]';
    if (depth > 6) return '[object]';
    if (ArrayBuffer.isView(value)) return {bytes:value.byteLength};
    if (value instanceof MediaStreamTrack) return {kind:value.kind,id:value.id,readyState:value.readyState};
    if (typeof value.join==='function' && typeof value.publish==='function') return {rtc_client:true};
    if (typeof value.getMediaStreamTrack === 'function') return {track:describe(value.getMediaStreamTrack(),depth+1)};
    if (value instanceof Error) return {name:value.name,code:value.code,message:value.message};
    if ('uid' in value && ('hasAudio' in value || 'hasVideo' in value)) return {uid:value.uid,hasAudio:value.hasAudio,hasVideo:value.hasVideo};
    if (Array.isArray(value)) return value.slice(0,128).map(v=>describe(v,depth+1));
    const out={};
    for (const [key,item] of Object.entries(value).slice(0,128)) out[key]=/token|password|ticket|credential|icepwd|sdp/i.test(key)?'[REDACTED]':describe(item,depth+1);
    return out;
  };
  const emit = (kind,name,fields={},extra={}) => {
    const event={kind,name,fields:describe(fields),source_seq:++sequence,occurred_at:new Date().toISOString(),monotonic_elapsed_ms:Math.round(performance.now()-start),...extra};
    const request=Promise.resolve(window.__emitProbeEvent?.(event)).catch(()=>{failures++;});
    pending.add(request);request.finally(()=>pending.delete(request));
  };
  function invoke(name,args,fn,mapResult) {
    const callId=crypto.randomUUID().replaceAll('-',''), at=performance.now();
    last.set(name,callId);
    const clean=name==='join'?{app_id:args[0],channel:args[1],token_present:!!args[2],uid:args[3]}:{arguments:args};
    emit('api_call_begin',name,{args:clean},{call_id:callId});
    const done=value=>{emit('api_call_end',name,{result:describe(value)},{call_id:callId,return_kind:'javascript',duration_ms:Math.round(performance.now()-at)});return mapResult?mapResult(value):value;};
    const failed=error=>{emit('api_call_end',name,{error:describe(error)},{call_id:callId,return_kind:'error',duration_ms:Math.round(performance.now()-at)});throw error;};
    try {const value=fn();return value&&typeof value.then==='function'?value.then(done,failed):done(value);}catch(error){return failed(error);}
  }
  function wrapTrack(track) {
    if (!track || proxies.has(track)) return proxies.get(track)||track;
    const proxy=new Proxy(track,{get(target,key){const value=Reflect.get(target,key,target);if(typeof value!=='function')return value;
      if(['play','stop','close','getMediaStreamTrack'].includes(key))return(...args)=>invoke(String(key),args,()=>value.apply(target,args.map(unwrap)));
      return value.bind(target);
    }});
    raw.set(proxy,track);proxies.set(track,proxy);return proxy;
  }
  function wrapClient(client) {
    const callbacks=new WeakMap();
    return new Proxy(client,{get(target,key){const value=Reflect.get(target,key,target);if(typeof value!=='function')return value;
      if(key==='on')return(event,callback)=>{const wrapped=(...args)=>{emit('sdk_callback',event,{parameters:describe(args)},{related_call_id:last.get(event==='user-published'?'join':'subscribe')});return callback(...args);};callbacks.set(callback,wrapped);return invoke('on',[event],()=>value.call(target,event,wrapped));};
      if(key==='off')return(event,callback)=>invoke('off',[event],()=>value.call(target,event,callbacks.get(callback)||callback));
      if(['join','publish','subscribe','leave','setClientRole','setLocalAccessPointsV2','getRemoteAudioStats','getRemoteVideoStats'].includes(key))return(...args)=>invoke(String(key),args,()=>value.apply(target,args.map(unwrap)));
      return value.bind(target);
    }});
  }
  async function observe(user,type,root) {
    const key=String(user.uid)+'/'+type;
    if(observers.has(key))return;
    const track=user[`${type}Track`].getMediaStreamTrack();
    const stats={samples:0,sampleRate:0,frames:0,width:0,height:0};observers.set(key,{stats,close:async()=>{}});
    if(type==='audio'){
      const context=new AudioContext(),source=context.createMediaStreamSource(new MediaStream([track])),processor=context.createScriptProcessor(1024,1,1),gain=context.createGain();gain.gain.value=0;
      processor.onaudioprocess=event=>{const channel=event.inputBuffer.getChannelData(0);if(channel.length&&channel.every(Number.isFinite)&&channel.some(value=>Math.abs(value)>1e-7)){stats.samples+=channel.length;stats.sampleRate=event.inputBuffer.sampleRate;}};
      source.connect(processor);processor.connect(gain);gain.connect(context.destination);await context.resume();
      observers.get(key).close=async()=>{processor.onaudioprocess=null;source.disconnect();processor.disconnect();gain.disconnect();await context.close();};
    }else{
      let video=root?.querySelector('video');if(!video){video=document.createElement('video');video.muted=true;video.playsInline=true;video.srcObject=new MediaStream([track]);root.append(video);await video.play();}
      let active=true,request;
      const frame=()=>{if(!active)return;if(video.videoWidth>0&&video.videoHeight>0){stats.frames++;stats.width=video.videoWidth;stats.height=video.videoHeight;}request=video.requestVideoFrameCallback(frame);};request=video.requestVideoFrameCallback(frame);
      observers.get(key).close=async()=>{active=false;video.cancelVideoFrameCallback(request);};
    }
  }
  async function networkStats(){
    const out={outboundAudioPackets:0,outboundVideoFrames:0,inboundVideoFrames:0,inboundAudioPackets:0,peers:[]};
    for(const connection of window.__probePeerConnections||[]){
      if(connection.connectionState==='closed')continue;
      const stats=await connection.getStats();
      for(const value of stats.values()){
        if(value.type==='outbound-rtp'&&!value.isRemote){if(value.kind==='audio'||value.mediaType==='audio')out.outboundAudioPackets+=value.packetsSent||0;else if(value.kind==='video'||value.mediaType==='video')out.outboundVideoFrames+=value.framesEncoded||value.framesSent||0;}
        if(value.type==='inbound-rtp'&&(value.kind==='audio'||value.mediaType==='audio'))out.inboundAudioPackets+=value.packetsReceived||0;
        if(value.type==='inbound-rtp'&&(value.kind==='video'||value.mediaType==='video'))out.inboundVideoFrames+=value.framesDecoded||0;
        if(value.type==='candidate-pair'&&value.state==='succeeded'&&(value.nominated||value.selected)){
          const peer=stats.get(value.remoteCandidateId);if(peer)out.peers.push({ip:peer.address||peer.ip,port:peer.port,protocol:peer.protocol,type:peer.candidateType,sent_bytes:value.bytesSent||0,received_bytes:value.bytesReceived||0});
        }
      }
    }
    return out;
  }
  window.FileProbeMonitor={emit,wrapTrack,observe,networkStats,async drain(){await Promise.allSettled([...pending]);return failures===0;},async cleanup(){for(const value of observers.values())await value.close();},snapshot(){const out={};for(const [key,value]of observers)out[key]=structuredClone(value.stats);return out;}};
  window.ProbeSDK={
    setParameter:(name,value)=>invoke('setParameter',[name,value],()=>AgoraRTC.setParameter(name,value)),
    createClient:options=>invoke('createClient',[options],()=>AgoraRTC.createClient(options),wrapClient),
    createCustomAudioTrack:options=>invoke('createCustomAudioTrack',[options],()=>AgoraRTC.createCustomAudioTrack(options),wrapTrack),
    createCustomVideoTrack:options=>invoke('createCustomVideoTrack',[options],()=>AgoraRTC.createCustomVideoTrack(options),wrapTrack),
  };
})();
