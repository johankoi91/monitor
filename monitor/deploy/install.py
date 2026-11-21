#!/usr/bin/env python3
"""V1.0 offline installation. Private configuration and fresh persistent directories are mandatory."""
import argparse,hashlib,ipaddress,json,os,shutil,subprocess,time,urllib.parse
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--role',choices=['postgres','center','agent','receiver','cadvisor','probe'],required=True)
p.add_argument('--bundle',type=Path,default=Path(__file__).resolve().parent.parent)
p.add_argument('--config',type=Path,required=True);p.add_argument('--data',type=Path,required=True)
p.add_argument('--name');p.add_argument('--memory-mib',type=int);p.add_argument('--cpus',type=float)
p.add_argument('--center-ip');p.add_argument('--host-address',default='')
a=p.parse_args()
if os.geteuid()!=0:raise SystemExit('root required')
if not a.config.is_absolute() or not a.data.is_absolute() or a.data.is_symlink() or a.config.resolve()==a.data.resolve() or a.config.resolve() in a.data.resolve().parents:raise SystemExit('separate absolute private config and data directories required')
if a.data.exists() and any(a.data.iterdir()):raise SystemExit('fresh data directory required; existing deployments are never overwritten')
if a.center_ip:a.center_ip=str(ipaddress.ip_address(a.center_ip))
if a.host_address:a.host_address=str(ipaddress.ip_address(a.host_address))
root=a.bundle.resolve();version=(root/'VERSION').read_text().strip()
if version!='V1.0':raise SystemExit('V1.0 bundle required')
# Check every public delivery file before creating containers or identities.
for line in (root/'SHA256SUMS').read_text().splitlines():
 digest,relative=line.split(None,1);relative=relative.lstrip('*')
 file=root/relative
 if file.resolve()!=root and root not in file.resolve().parents:raise SystemExit('invalid manifest path')
 h=hashlib.sha256()
 with file.open('rb') as source:
  for block in iter(lambda:source.read(1024*1024),b''):h.update(block)
 if h.hexdigest()!=digest:raise SystemExit('bundle checksum mismatch: '+relative)
role_images={'postgres':'docker.m.daocloud.io/library/postgres:17','center':'avops-center:V1.0','agent':'avops-agent:V1.0','receiver':'avops-notification-receiver:V1.0','cadvisor':'avops-cadvisor:V1.0','probe':'avops-probe:V1.0'}
archives={'postgres':'avops-postgres-17-linux-amd64.tar','center':'avops-center-linux-amd64.tar','agent':'avops-agent-linux-amd64.tar','receiver':'avops-notification-receiver-linux-amd64.tar','cadvisor':'avops-cadvisor-linux-amd64.tar','probe':'avops-probe-linux-amd64.tar'}
names={'postgres':'avops-postgres','center':'avops-center','agent':'avops-agent','receiver':'avops-notification-receiver','cadvisor':'avops-cadvisor','probe':'avops-probe'}
name=a.name or names[a.role]
if not name or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-' for c in name):raise SystemExit('invalid container name')
if subprocess.check_output(['docker','ps','-aq','--filter','name=^/'+name+'$']).strip():raise SystemExit('container already exists')
subprocess.run(['docker','load','-i',str(root/'images'/archives[a.role])],check=True,stdout=subprocess.DEVNULL)
def env_file(filename):
 file=a.config/filename
 if file.stat().st_mode&0o077:raise SystemExit('private env files must have mode 0600')
 return file
def read_env(file):return dict(line.split('=',1) for line in file.read_text().splitlines() if line and not line.startswith('#'))
if a.role=='probe':
 host=a.data/'host';clients=a.data/'clients'
 command=['python3',str(root/'deploy/probe/install.py'),'--image',role_images[a.role],'--environment',str(env_file('probe-client.env')),'--host-directory',str(host),'--data-directory',str(clients),'--name',name,'--memory-mib',str(a.memory_mib or 2048),'--cpus',str(a.cpus or 2)]
 if a.center_ip:command+=['--center-ip',a.center_ip]
 if a.host_address:command+=['--host-address',a.host_address]
 subprocess.run(command,check=True);raise SystemExit(0)
resources={'postgres':(1024,1),'center':(2048,4),'agent':(256,0.5),'receiver':(256,0.5),'cadvisor':(256,0.5)}
memory,cpus=resources[a.role];memory=a.memory_mib or memory;cpus=a.cpus or cpus
if memory<64 or cpus<=0:raise SystemExit('invalid resource budget')
a.data.mkdir(mode=0o700,parents=True,exist_ok=True)
common=['docker','run','-d','--pull=never','--name',name,'--restart','unless-stopped','--read-only','--security-opt','no-new-privileges','--memory',str(memory)+'m','--cpus',str(cpus),'--pids-limit','1024','--log-opt','max-size=10m','--log-opt','max-file=3','--tmpfs','/tmp:size=64m,mode=1777']
command=common.copy()
if a.role=='postgres':
 os.chown(a.data,999,999);command+=['--user','999:999','--cap-drop','ALL','--tmpfs','/var/run/postgresql:uid=999,gid=999,mode=0755','-p','127.0.0.1:5432:5432','--env-file',str(env_file('postgres.env')),'-v',str(a.data)+':/var/lib/postgresql/data',role_images[a.role],'-c','shared_buffers=128MB']
elif a.role in ['center','receiver']:
 os.chown(a.data,65532,65532);runtime=a.data/'runtime-config';runtime.mkdir(mode=0o700);os.chown(runtime,65532,65532);runtime.chmod(0o700)
 files=['server.pem','server-key.pem']+(['center.yaml'] if a.role=='center' else [])
 for filename in files:
  shutil.copyfile(a.config/filename,runtime/filename);os.chown(runtime/filename,65532,65532);(runtime/filename).chmod(0o400)
 command+=['--network','host','--cap-drop','ALL','--env-file',str(env_file('center.env' if a.role=='center' else 'receiver.env'))]
 if a.role=='center':command+=['--env-file',str(env_file('database.env')),'-e','AVOPS_CENTER_CONFIG=/config/center.yaml','-e','AVOPS_DATA_DIR=/state','-e','AVOPS_TLS_CERT=/config/server.pem','-e','AVOPS_TLS_KEY=/config/server-key.pem','-e','AVOPS_STORAGE_DISK_PATH=/state']
 else:command+=['-e','AVOPS_RECEIVER_DATA_DIR=/state','-e','AVOPS_RECEIVER_TLS_CERT=/config/server.pem','-e','AVOPS_RECEIVER_TLS_KEY=/config/server-key.pem']
 command+=['-v',str(runtime)+':/config:ro','-v',str(a.data)+':/state',role_images[a.role]]
elif a.role=='agent':
 environment=env_file('agent.env');values=read_env(environment)
 command+=['--network','host','--hostname',socket.gethostname(),'--env-file',str(environment),'-e','AVOPS_AGENT_DATA_DIR=/state','-e','AVOPS_RESTART_RULES_FILE=/config/restart-rules.yaml','-v','/var/run/docker.sock:/var/run/docker.sock','-v',str(a.data)+':/state','-v',str(a.config)+':/config:ro']
 if a.center_ip:command+=['--add-host',urllib.parse.urlsplit(values['AVOPS_CENTER_URL']).hostname+':'+a.center_ip]
 command+=[role_images[a.role]]
else:
 docker_root=json.loads(subprocess.check_output(['docker','info','--format','{{json .DockerRootDir}}']))
 command+=['--privileged','--device','/dev/kmsg','--network','host','-v','/:/rootfs:ro','-v','/var/run:/var/run:ro','-v','/sys:/sys:ro','-v',docker_root+':'+docker_root+':ro',role_images[a.role],'--listen_ip=127.0.0.1','--port=18089','--docker_only=true','--housekeeping_interval=5s','--allow_dynamic_housekeeping=false','--store_container_labels=false','--enable_metrics=cpu,memory,network,disk,diskIO,oom_event']
subprocess.run(command,check=True,stdout=subprocess.DEVNULL)
if a.role=='postgres':
 for _ in range(60):
  if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','avops_owner','-d','avops_monitor'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise SystemExit('PostgreSQL did not become ready')
 result=subprocess.run(['docker','exec','-i',name,'psql','-X','-q','-v','ON_ERROR_STOP=1','-U','avops_owner','-d','avops_monitor'],input=(a.config/'init.sql').read_bytes(),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if result.returncode:raise SystemExit('database role initialization failed; private SQL retained for inspection')
 owner=read_env(env_file('owner.env'))
 subprocess.run([str(root/'bin/dbmigrate')],env={**os.environ,**owner},check=True)
print(json.dumps({'container':name,'role':a.role,'version':version}))
