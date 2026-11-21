#!/usr/bin/env python3
"""Install one v2 server container supervising RTC Native and Web clients."""
import argparse, fcntl, ipaddress, json, math, os, pathlib, re, secrets, socket, subprocess, tempfile, urllib.parse

def prepare_host(directory, name, address='', location='', uid=None, gid=None):
    if not directory.is_absolute():
        raise RuntimeError('absolute identity directory required')
    if not name or len(name) > 200:
        raise RuntimeError('valid hostname required')
    if address:
        ipaddress.ip_address(address)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory.chmod(0o700)
    with (directory / 'identity.lock').open('a+b') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        target = directory / 'server-identity-v2.json'
        if target.exists():
            value = json.loads(target.read_text())
            if value.get('version') != 2 or not value.get('host', {}).get('host_id') or len(value.get('secret', '')) < 24:
                raise RuntimeError('invalid server identity; preserve for inspection')
        else:
            value = {'version': 2, 'host': {'host_id': 'server-' + secrets.token_hex(16)}, 'secret': secrets.token_hex(32)}
        value['host'].update(host_name=name, host_address=address, location=location)
        descriptor, temporary = tempfile.mkstemp(dir=directory, prefix='.identity-')
        try:
            with os.fdopen(descriptor, 'w') as stream:
                json.dump(value, stream)
                stream.flush()
                os.fsync(stream.fileno())
            os.chmod(temporary, 0o600)
            if uid is not None or gid is not None:
                os.chown(temporary, uid if uid is not None else -1, gid if gid is not None else -1)
            os.replace(temporary, target)
            directory_fd = os.open(directory, os.O_RDONLY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
        if uid is not None or gid is not None:
            os.chown(directory, uid if uid is not None else -1, gid if gid is not None else -1)
            os.chown(lock.name, uid if uid is not None else -1, gid if gid is not None else -1)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--image',required=True)
    p.add_argument('--validate-only',action='store_true')
    p.add_argument('--environment',type=pathlib.Path,required=True);p.add_argument('--host-directory',type=pathlib.Path,required=True);p.add_argument('--data-directory',type=pathlib.Path,required=True)
    p.add_argument('--host-name', default=None)
    p.add_argument('--host-address', default='')
    p.add_argument('--center-ip',help='Optional private DNS mapping; certificate hostname is still verified')
    p.add_argument('--name');p.add_argument('--memory-mib',type=int);p.add_argument('--cpus',type=float,default=2)
    a=p.parse_args()
    if os.geteuid()!=0:raise RuntimeError('root required')
    memory=a.memory_mib if a.memory_mib is not None else 2048
    if a.center_ip: a.center_ip=str(ipaddress.ip_address(a.center_ip))
    if memory<512 or not math.isfinite(a.cpus) or not 0<a.cpus<=64:raise RuntimeError('invalid resource allocation')
    name=a.name or 'avops-probe'
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}',name):raise RuntimeError('invalid container name')
    for path in [a.environment,a.host_directory,a.data_directory]:
        if not path.is_absolute():raise RuntimeError('absolute paths required')
    host_path=a.host_directory.resolve();data_path=a.data_directory.resolve()
    if host_path==data_path or host_path in data_path.parents or data_path in host_path.parents:raise RuntimeError('shared identity and client data must be separate directories')
    if any(a.environment.resolve()==directory or directory in a.environment.resolve().parents for directory in [host_path,data_path]):raise RuntimeError('keep deployment credentials outside mounted runtime directories')
    if any(':' in str(path) or ',' in str(path) for path in [a.host_directory,a.data_directory]):raise RuntimeError('unsupported mount path')
    if not a.environment.is_file():raise RuntimeError('environment file required')
    if a.environment.stat().st_mode&0o077:raise RuntimeError('environment file must be private (0600)')
    env={}
    for raw in a.environment.read_text().splitlines():
        line=raw.strip()
        if not line or line.startswith('#'):continue
        key,sep,value=line.partition('=')
        if not sep or key in env:raise RuntimeError('invalid or duplicate environment field')
        env[key]=value
    allowed={'AVOPS_PROBE_CENTER_URL','AVOPS_PROBE_ENROLL_TOKEN','AVOPS_PROBE_HOST_DIR','AVOPS_PROBE_DATA_DIR','AVOPS_PROBE_NATIVE_CAPACITY','AVOPS_PROBE_WEB_CAPACITY','AVOPS_PROBE_NAME','AVOPS_PROBE_HOST_NAME','AVOPS_PROBE_HOST_ADDRESS','AVOPS_PROBE_LOCATION','AVOPS_PROBE_ROUTE_CAPTURE'}
    if set(env)-allowed:raise RuntimeError('unsupported probe environment field')
    endpoint=urllib.parse.urlsplit(env.get('AVOPS_PROBE_CENTER_URL',''))
    if endpoint.scheme!='wss' or not endpoint.hostname or endpoint.path!='/api/v1/probes/connect' or endpoint.username or endpoint.query or endpoint.fragment:raise RuntimeError('v2 center WSS URL required')
    host_name=a.host_name or env.get('AVOPS_PROBE_HOST_NAME') or socket.gethostname()
    host_address=a.host_address or env.get('AVOPS_PROBE_HOST_ADDRESS','')
    if not host_name or len(host_name)>200: raise RuntimeError('valid hostname required')
    if host_address: host_address=str(ipaddress.ip_address(host_address))
    if len(env.get('AVOPS_PROBE_ENROLL_TOKEN',''))<24:raise RuntimeError('site enrollment credential required')
    capacities=[]
    for kind in ['NATIVE','WEB']:
        value=env.get('AVOPS_PROBE_'+kind+'_CAPACITY','1')
        if not value.isdecimal() or not 1<=int(value)<=64:raise RuntimeError('invalid client capacity')
        capacities.append(int(value))
    required_memory=512+capacities[0]*256+capacities[1]*512
    if sum(capacities)>a.cpus or required_memory>memory:raise RuntimeError('combined CPU/memory budget does not cover both clients')
    if subprocess.check_output(['docker','ps','-aq','--filter','name=^/'+name+'$'],universal_newlines=True).strip():raise RuntimeError('container already exists')
    image=json.loads(subprocess.check_output(['docker','image','inspect',a.image]))[0];labels=image['Config'].get('Labels') or {}
    if labels.get('avops.probe.schema')!='2' or labels.get('avops.probe.clients')!='native,web' or labels.get('avops.probe.service')!='rtc':raise RuntimeError('combined v2 RTC server image required')
    if a.data_directory.exists() and (not a.data_directory.is_dir() or any(a.data_directory.iterdir())):raise RuntimeError('fresh combined server data directory required')
    if a.validate_only:
        print('Probe image, environment and shared resource budget validated.');return
    prepare_host(a.host_directory,host_name,host_address,env.get('AVOPS_PROBE_LOCATION',''),uid=65532,gid=65532)
    a.data_directory.mkdir(mode=0o700,parents=True,exist_ok=True)
    a.data_directory.chmod(0o700)
    os.chown(a.data_directory,65532,65532)
    command=['docker','run','-d','--pull=never','--init','--name',name,'--network','host','--restart','unless-stopped','--stop-timeout','30','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--memory',str(memory)+'m','--cpus',str(a.cpus),'--pids-limit','2048','--shm-size','512m','--tmpfs','/tmp:size=512m,mode=1777','--env-file',str(a.environment),'-e','AVOPS_PROBE_DATA_DIR=/data','-e','AVOPS_PROBE_HOST_DIR=/host','-v',str(a.host_directory)+':/host:rw','-v',str(a.data_directory)+':/data:rw','--log-opt','max-size=10m','--log-opt','max-file=3',a.image]
    if a.center_ip:command[-1:-1]=['--add-host',endpoint.hostname+':'+a.center_ip]
    subprocess.run(command,check=True,stdout=subprocess.DEVNULL)
    print(json.dumps({'container':name,'service_type':'rtc','clients':['native','web'],'schema':2}))

if __name__=='__main__':main()
