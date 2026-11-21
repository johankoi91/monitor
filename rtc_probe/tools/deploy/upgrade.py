#!/usr/bin/env python3
"""Upgrade v2 containers only. No v1 environment, identity or binding migration."""
import argparse, http.client, json, os, pathlib, re, socket, subprocess, time, urllib.parse

class Engine(http.client.HTTPConnection):
    def connect(self):
        self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        self.sock.connect('/var/run/docker.sock')

def api(method,path,value=None,accepted=(200,201,204)):
    connection=Engine('localhost',timeout=30)
    data=json.dumps(value).encode() if value is not None else None
    connection.request(method,path,body=data,headers={'Content-Type':'application/json'})
    response=connection.getresponse();body=response.read();connection.close()
    if response.status not in accepted:raise RuntimeError('Docker operation failed: '+method+' '+path.split('?')[0]+' status '+str(response.status))
    return json.loads(body) if body else None

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--container',required=True);p.add_argument('--image',required=True);p.add_argument('--stage',type=pathlib.Path,required=True)
    a=p.parse_args()
    if os.geteuid()!=0:raise RuntimeError('root required')
    if not a.stage.is_absolute():raise RuntimeError('absolute stage directory required')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}',a.container):raise RuntimeError('invalid container name')
    old=api('GET','/containers/'+a.container+'/json')
    image=api('GET','/images/'+urllib.parse.quote(a.image,safe='')+'/json')
    old_labels=old['Config'].get('Labels') or {};new_labels=image['Config'].get('Labels') or {}
    if old_labels.get('avops.probe.schema')!='2' or new_labels.get('avops.probe.schema')!='2':raise RuntimeError('only v2 containers/images are accepted; create a fresh v2 installation')
    for key in ['avops.probe.service','avops.probe.clients']:
        if old_labels.get(key)!=new_labels.get(key):raise RuntimeError('service/client image mismatch')
    env=dict(entry.split('=',1) for entry in old['Config'].get('Env',[]) if '=' in entry)
    if any(key.startswith('RTC_PROBE_') or key in ['AVOPS_PROBE_ID','AVOPS_PROBE_SECRET'] for key in env):raise RuntimeError('old probe configuration is not supported')
    if 'AVOPS_PROBE_ENROLL_TOKEN' not in env:raise RuntimeError('v2 enrollment configuration missing')
    if old['HostConfig'].get('AutoRemove'):raise RuntimeError('auto-remove containers cannot be upgraded')
    if old['Image']==image['Id']:raise RuntimeError('image already installed')
    a.stage.mkdir(mode=0o700,parents=True,exist_ok=False);a.stage.chmod(0o700)
    snapshot=a.stage/'previous-container.json';snapshot.write_text(json.dumps(old));snapshot.chmod(0o600)
    previous=a.container+'-before-'+str(int(time.time()))
    config={key:old['Config'][key] for key in ['Hostname','Domainname','User','AttachStdin','AttachStdout','AttachStderr','ExposedPorts','Tty','OpenStdin','StdinOnce','Env','Cmd','Healthcheck','ArgsEscaped','Volumes','WorkingDir','Entrypoint','StopSignal','StopTimeout','Shell'] if key in old['Config']}
    config.update(Image=a.image,HostConfig=old['HostConfig'],Labels={**old_labels,**new_labels})
    api('POST','/containers/'+a.container+'/stop?t=15',accepted=(204,304))
    api('POST','/containers/'+a.container+'/rename?name='+urllib.parse.quote(previous))
    try:
        created=api('POST','/containers/create?name='+urllib.parse.quote(a.container),config)
        api('POST','/containers/'+created['Id']+'/start')
        time.sleep(2)
        if not api('GET','/containers/'+created['Id']+'/json')['State']['Running']:raise RuntimeError('new v2 container exited')
    except BaseException:
        # Only remove the new container. Private data and server identity stay intact.
        subprocess.run(['docker','rm','-f',a.container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        api('POST','/containers/'+previous+'/rename?name='+urllib.parse.quote(a.container))
        api('POST','/containers/'+a.container+'/start')
        raise
    print(json.dumps({'container':a.container,'image':a.image,'backup_container':previous,'schema':2}))

if __name__=='__main__':main()
