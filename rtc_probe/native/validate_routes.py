#!/usr/bin/env python3
"""Run explicitly requested real SDK routing cases, keeping artifacts private."""
import argparse,datetime,json,os,pathlib,subprocess,threading,time
p=argparse.ArgumentParser()
p.add_argument('--binary',required=True);p.add_argument('--capture',required=True);p.add_argument('--output',type=pathlib.Path,required=True)
p.add_argument('--case',required=True,choices=['udp-ap','aut-ap','udp-direct','aut-direct','udp-cross','aut-cross','udp-aut','aut-udp'])
p.add_argument('--guard',action='store_true')
p.add_argument('--scope',choices=['service','connection'],default='connection')
a=p.parse_args()
app=os.environ.get('RTC_NATIVE_QA_APP_ID','')
if len(app)!=32:raise RuntimeError('RTC_NATIVE_QA_APP_ID required')
a.output.mkdir(mode=0o700,parents=True,exist_ok=False)
route=lambda kind,port:{'transport':kind,'endpoint':'111.230.192.59:'+str(port)}
candidates=[route('udp',port) for port in [4001,4002,4003]]+[route('aut',port) for port in [4701,4702,4703]]
sender,receiver=route('udp',4001),route('udp',4001)
kind='DIRECT_JOIN'
if a.case=='udp-ap':sender=receiver={'transport':'udp','endpoint':''};kind='AP_JOIN'
elif a.case=='aut-ap':sender=receiver={'transport':'aut','endpoint':''};kind='AP_JOIN'
elif a.case=='aut-direct':sender=receiver=route('aut',4701)
elif a.case=='udp-cross':receiver=route('udp',4002);kind='CROSS_EDGE_MEDIA'
elif a.case=='aut-cross':sender=route('aut',4701);receiver=route('aut',4702);kind='CROSS_EDGE_MEDIA'
elif a.case=='udp-aut':receiver=route('aut',4701);kind='CROSS_EDGE_MEDIA'
elif a.case=='aut-udp':sender=route('aut',4701);kind='CROSS_EDGE_MEDIA'
run_id=os.urandom(16).hex()
job={'run_id':run_id,'task_id':'route-qa','target':{'target_id':'route-qa','driver':'agora'},'config_revision':'route-qa','probe_id':'route-qa','endpoint_role':'pair','channel':'qa_'+run_id,'uid':'10001','publisher_uid':'10001','deadline':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=18)).isoformat().replace('+00:00','Z'),'media_duration_ms':5000,'work_dir':str(a.output.resolve()),'rtc':{'app_id':app,'auth_mode':'none','private_deployment':True,'access_ips':['111.230.192.59'],'access_domains':['111.230.192.59'],'verify_domain':'ap.1145326.agora.local','proxy_mode':1,'disable_aut':False},'native_case':{'case_key':a.case,'case_kind':kind,'coverage_version':'qa','ap_group':'172.16.80.25','ap_entrypoint':'111.230.192.59','sender':sender,'receiver':receiver,'candidates':candidates,'parameter_scope':a.scope}}
read_fd,write_fd=os.pipe()
if read_fd!=3:raise RuntimeError('run QA with descriptors 3+ free')
env=dict(os.environ,LD_PRELOAD=str(pathlib.Path(a.capture).resolve()),RTC_PROBE_ROUTE_LOG=str(a.output.resolve()/'route-capture.jsonl'))
if a.guard:
 allow=[edge['endpoint'] for edge in [sender,receiver] if edge.get('endpoint')]
 if kind=='AP_JOIN':allow=[edge['endpoint'] for edge in candidates if edge['transport']==sender['transport']]
 allow+=['111.230.192.59:8003','111.230.192.59:8443']
 env['RTC_PROBE_ROUTE_ALLOW_ENDPOINTS']=';'.join(sorted(set(allow)))
with (a.output/'worker.log').open('wb') as log:
 child=subprocess.Popen([a.binary,'worker'],stdin=subprocess.PIPE,stdout=log,stderr=subprocess.STDOUT,pass_fds=(read_fd,write_fd),preexec_fn=lambda:os.dup2(write_fd,3),env=env)
os.close(write_fd)
child.stdin.write((json.dumps({'protocol_version':1,'type':'prepare_run','job':job})+'\n').encode());child.stdin.flush()
ready=set();results={};messages=[]
with os.fdopen(read_fd) as stream:
 for line in stream:
  value=json.loads(line);messages.append(value)
  if value['type']=='run_ready':
   ready.add(value['endpoint_role'])
   if len(ready)==2:
    child.stdin.write((json.dumps({'protocol_version':1,'type':'start_media','run_id':run_id,'config_revision':'route-qa','endpoint_role':'pair'})+'\n').encode());child.stdin.flush()
  if value['type']=='run_result':results[value['endpoint_role']]=value['result']
child.wait(timeout=15);child.stdin.close()
(a.output/'evidence.json').write_text(json.dumps({'case':a.case,'scope':a.scope,'results':results,'messages':messages},indent=2))
print(json.dumps({'case':a.case,'scope':a.scope,'exit_code':child.returncode,'results':{role:{'outcome':r['outcome'],'reason':r['reason'],'media':r['evidence'],'route':r.get('route')} for role,r in results.items()}}),flush=True)
