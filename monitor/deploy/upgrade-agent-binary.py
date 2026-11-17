#!/usr/bin/env python3
"""Replace only the monitoring Agent binary, preserving its runtime and rollback."""
import json,os,pathlib,subprocess,sys,time
stage=pathlib.Path(sys.argv[1]).resolve()
if os.geteuid()!=0:raise RuntimeError('root required')
stage.mkdir(parents=True,exist_ok=True,mode=0o700);stage.chmod(0o700)
def docker(*args):return subprocess.check_output(['docker',*args],text=True).strip()
name='avops-agent';backup='avops-agent-before-docker-api-20261117';image='avops-agent:docker-api-20261117'
old=json.loads(docker('inspect',name))[0]
if old['Config']['Entrypoint']!=['/agent']:raise RuntimeError('unexpected Agent entrypoint')
if subprocess.run(['docker','inspect',backup],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:raise RuntimeError('backup already exists')
for filename,text in [('previous-container.json',json.dumps(old)),('runtime.env','\n'.join(old['Config']['Env'])+'\n')]:
 p=stage/filename;p.write_text(text);p.chmod(0o600)
(stage/'Dockerfile').write_text('FROM '+old['Config']['Image']+'\nCOPY agent /agent\n')
(stage/'.dockerignore').write_text('*\n!Dockerfile\n!agent\n')
print(docker('build','-q','-t',image,str(stage)))
h=old['HostConfig'];args=['run','-d','--name',name,'--restart',h['RestartPolicy']['Name'],'--network',h['NetworkMode'],'--env-file',str(stage/'runtime.env')]
for bind in h.get('Binds') or []:args+=['-v',bind]
for value in h.get('SecurityOpt') or []:args+=['--security-opt',value]
for value in h.get('CapDrop') or []:args+=['--cap-drop',value]
for value in h.get('CapAdd') or []:args+=['--cap-add',value]
for value in h.get('GroupAdd') or []:args+=['--group-add',value]
for value in h.get('ExtraHosts') or []:args+=['--add-host',value]
for key,value in (h.get('Tmpfs') or {}).items():args+=['--tmpfs',key+':'+value]
if h.get('ReadonlyRootfs'):args+=['--read-only']
if h.get('PidMode'):args+=['--pid',h['PidMode']]
if h.get('Privileged'):args+=['--privileged']
if h.get('Memory'):args+=['--memory',str(h['Memory'])]
if h.get('NanoCpus'):args+=['--cpus',str(h['NanoCpus']/1e9)]
if h.get('PidsLimit'):args+=['--pids-limit',str(h['PidsLimit'])]
args+=[image]+(old['Config'].get('Cmd') or [])
print(docker('stop','--time','15',name));print(docker('rename',name,backup))
try:
 print(docker(*args));time.sleep(2)
 if not json.loads(docker('inspect',name))[0]['State']['Running']:raise RuntimeError('new Agent exited')
except BaseException:
 subprocess.run(['docker','logs',name],stdout=(stage/'failed-startup.log').open('w'),stderr=subprocess.STDOUT)
 subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 docker('rename',backup,name);docker('start',name);raise
print(json.dumps({'image':image,'container':name,'backup':backup}))
