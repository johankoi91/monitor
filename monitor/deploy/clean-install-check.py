"""Install the extracted bundle into a fresh, isolated Docker fixture.
No production config/database is imported. Private TLS inputs stay outside bundle.
"""
import argparse, http.client, json, os, pathlib, secrets, shutil
import socket, ssl, subprocess, time, urllib.parse, base64, hashlib, hmac

parser = argparse.ArgumentParser()
parser.add_argument("--bundle", required=True)
parser.add_argument("--work", required=True)
parser.add_argument("--cert", required=True)
parser.add_argument("--key", required=True)
parser.add_argument("--tls-name", default="edge.rtcdevelopers.com")
args = parser.parse_args()
bundle = pathlib.Path(args.bundle).resolve()
work = pathlib.Path(args.work).resolve()
assert not work.exists(), "fresh directory required"
assert os.geteuid() == 0
os.umask(0o077)
work.mkdir(mode=0o700)
prefix = "avops-offline-" + secrets.token_hex(4)
containers = []
proof = {"fixture": prefix, "steps": []}
def run(*cmd, input=None):
    return subprocess.check_output(cmd, input=input, stderr=subprocess.STDOUT)
def docker(*cmd, input=None):
    return run("docker", *cmd, input=input)
def mark(name):
    proof["steps"].append(name)
    print(name, flush=True)
def wait(fn, seconds=60):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        try:
            value=fn()
            if value:return value
        except Exception:pass
        time.sleep(1)
    raise RuntimeError("bounded verification timed out")
def env(path):
    return dict(line.split("=",1) for line in path.read_text().splitlines() if "=" in line)
def write_env(path, values):
    path.write_text("".join(k+"="+v+"\n" for k,v in values.items()))
def launch(name, *options):
    containers.append(name)
    docker("run","-d","--pull=never","--name",name,*options)
class Center(http.client.HTTPSConnection):
    def connect(self):
        self.sock=ssl.create_default_context().wrap_socket(socket.create_connection(("127.0.0.1",29444),timeout=10),server_hostname=args.tls_name)
def api(method,path,body=None,auth=""):
    c=Center(args.tls_name,29444,timeout=12)
    headers={"Content-Type":"application/json"}
    if auth:headers["Authorization"]=auth
    c.request(method,path,None if body is None else json.dumps(body).encode(),headers)
    r=c.getresponse();b=r.read();status=r.status;c.close()
    return status,json.loads(b) if b else {}
def sql(q):
    return docker("exec",prefix+"-pg","psql","-U","avops_owner","-d","avops_monitor","-Atc",q).decode().strip()

try:
    for port in [25433,29444,28085,28089,28443]:
        s=socket.socket();s.bind(("127.0.0.1",port));s.close()
    version=(bundle/"VERSION").read_text().strip()
    run("sha256sum","--check","--quiet",str(bundle/"SHA256SUMS")) if pathlib.Path.cwd()==bundle else subprocess.check_call(["sha256sum","--check","--quiet","SHA256SUMS"],cwd=bundle)
    docker("load","-i",str(bundle/"images/avops-images-linux-amd64.tar"))
    docker("load","-i",str(bundle/"images/avops-postgres-17-linux-amd64.tar"))
    mark("offline image load and all-file checksums passed")
    private=work/"private"
    run(str(bundle/"bin/pgprovision"),"-out",str(private))
    pg=work/"postgres";pg.mkdir();os.chown(pg,999,999)
    owner=env(private/"owner.env");runtime=env(private/"database.env")
    owner["AVOPS_DATABASE_URL"]=owner["AVOPS_DATABASE_URL"].replace(":5432/",":25433/")
    runtime["AVOPS_DATABASE_URL"]=runtime["AVOPS_DATABASE_URL"].replace(":5432/",":25433/")
    write_env(private/"owner.env",owner);write_env(private/"database.env",runtime)
    launch(prefix+"-pg","--network","bridge","-p","127.0.0.1:25433:5432","--env-file",str(private/"postgres.env"),"-v",str(pg)+":/var/lib/postgresql/data","--memory","512m","--cpus","0.75","docker.m.daocloud.io/library/postgres:17","-c","shared_buffers=64MB")
    wait(lambda:sql("SELECT 1")=="1")
    docker("exec","-i",prefix+"-pg","psql","-v","ON_ERROR_STOP=1","-U","avops_owner","-d","avops_monitor",input=(private/"init.sql").read_bytes())
    for flag in ["-compact-operations","-remove-key-scopes"]:
        subprocess.check_call([str(bundle/"bin/dbmigrate"),flag],env={**os.environ,**owner},stdout=subprocess.DEVNULL)
    assert sql("SELECT count(*) FROM avops.users")=="0"
    mark("fresh PostgreSQL data directory, restricted runtime and owner migrations passed")
    config=work/"config";config.mkdir(mode=0o700)
    shutil.copyfile(args.cert,config/"server.pem");shutil.copyfile(args.key,config/"server-key.pem")
    admin,agent_secret=secrets.token_hex(32),secrets.token_hex(32)
    canary=prefix+"-canary"
    docker("build","--network","none","-f",str(bundle/"source/monitor/deploy/Dockerfile.canary"),"-t",prefix+":canary",str(bundle/"bin"))
    launch(canary,"--network","bridge","--memory","32m","--cpus","0.1",prefix+":canary")
    rule={"agent_id":"offline-agent","container_name":canary,"service_code":"offline-canary","image":prefix+":canary","approved":True,"approval_ref":"isolated-offline-test"}
    (config/"center.yaml").write_text(json.dumps({"site":{"code":"offline","name":"Offline QA"},"agents":[{"id":"offline-agent","secret":agent_secret,"host_name":"offline-host","host_address":"127.0.0.1"}],"restart_rules":[rule],"notifications":{"enabled":False}}))
    (config/"restart-rules.yaml").write_text(json.dumps([rule]))
    write_env(config/"database.env",runtime)
    write_env(config/"center.env",{"AVOPS_ADMIN_ID":"offline-admin","AVOPS_ADMIN_SECRET":admin,"AVOPS_CENTER_ADDR":"127.0.0.1:29444","AVOPS_CENTER_LOCAL_ADDR":"127.0.0.1:28085","AVOPS_ALLOWED_IPS":"127.0.0.1"})
    write_env(config/"agent.env",{"AVOPS_AGENT_ID":"offline-agent","AVOPS_AGENT_SECRET":agent_secret,"AVOPS_CENTER_URL":"wss://127.0.0.1:29444/api/v1/agent/connect","AVOPS_TLS_SERVER_NAME":args.tls_name,"AVOPS_COLLECT_SECONDS":"10","AVOPS_RAW_STARTUP":"1"})
    installer=str(bundle/"deploy/docker-install.sh")
    center_name,agent_name=prefix+"-center",prefix+"-agent"
    containers.extend([center_name,agent_name])
    run("bash",installer,"center",str(config),str(work/"center-data"),center_name)
    wait(lambda:api("POST","/api/v1/auth/login",{"username":"admin","password":runtime["AVOPS_BOOTSTRAP_PASSWORD"]})[0]==200)
    status,login=api("POST","/api/v1/auth/login",{"username":"admin","password":runtime["AVOPS_BOOTSTRAP_PASSWORD"]})
    token="Bearer "+login["token"]
    assert login["user"]["username"]=="admin"
    mark("fresh account bootstrap and verified HTTPS login passed")
    docker_root=json.loads(docker('info','--format','{{json .DockerRootDir}}'))
    launch(prefix+'-cadvisor','--privileged','--device','/dev/kmsg','--network','bridge','-p','127.0.0.1:28089:8080','-v','/:/rootfs:ro','-v','/var/run:/var/run:ro','-v','/sys:/sys:ro','-v',docker_root+':'+docker_root+':ro','--memory','256m','--cpus','0.5','avops-cadvisor:'+version,'--docker_only=true','--housekeeping_interval=5s','--allow_dynamic_housekeeping=false','--store_container_labels=false','--enable_metrics=cpu,memory,network,disk,diskIO,oom_event')
    def cadvisor_ready():
        c=http.client.HTTPConnection('127.0.0.1',28089,timeout=5);c.request('GET','/api/v2.0/stats/?type=docker&count=2&recursive=true');r=c.getresponse();body=r.read();c.close()
        return r.status==200 and bool(json.loads(body))
    wait(cadvisor_ready)
    with (config/'agent.env').open('a') as f:f.write('AVOPS_CADVISOR_URL=http://127.0.0.1:28089\n')
    mark('offline cAdvisor image serves real Docker resource samples on loopback')
    receiver_secret,webhook_secret=secrets.token_hex(32),secrets.token_hex(32)
    write_env(config/'receiver.env',{'AVOPS_RECEIVER_ID':'offline-receiver','AVOPS_RECEIVER_SECRET':receiver_secret,'AVOPS_WEBHOOK_SECRET':webhook_secret,'AVOPS_RECEIVER_ADDR':'127.0.0.1:28443','AVOPS_RECEIVER_ALLOWED_IPS':'127.0.0.1'})
    receiver_name=prefix+'-receiver';containers.append(receiver_name)
    run('bash',installer,'receiver',str(config),str(work/'receiver-data'),receiver_name)
    def receiver_request(method,path,body=None,headers=None):
        c=Center(args.tls_name,28443,timeout=12)
        # Center.connect pins the center port; use the receiver port here.
        c.sock=ssl.create_default_context().wrap_socket(socket.create_connection(('127.0.0.1',28443),timeout=10),server_hostname=args.tls_name)
        c.request(method,path,body,headers or {});r=c.getresponse();body=r.read();code=r.status;c.close()
        return code,json.loads(body) if body else {}
    diagnostic={'Authorization':'Basic '+base64.b64encode(('offline-receiver:'+receiver_secret).encode()).decode()}
    wait(lambda:receiver_request('GET','/health/ready',headers=diagnostic)[0]==200)
    payload=json.dumps({'event_id':prefix+'-notification','event_type':'STATUS_SNAPSHOT','event_category':'NORMAL','occurred_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'product':'RTC','cluster':'offline','service_code':'offline-canary','node_id':'127.0.0.1-'+canary,'previous_status':'UNKNOWN','current_status':'HEALTHY','reason_code':'OK','reason':'isolated offline verification','collected_at':None,'stale':False}).encode()
    signatures={'Content-Type':'application/json','Agora-Signature':hmac.new(webhook_secret.encode(),payload,hashlib.sha1).hexdigest(),'Agora-Signature-V2':hmac.new(webhook_secret.encode(),payload,hashlib.sha256).hexdigest()}
    code,accepted_event=receiver_request('POST','/api/v1/notifications',payload,signatures)
    assert code==200 and accepted_event.get('persisted'),(code,accepted_event)
    assert receiver_request('POST','/api/v1/notifications',payload,signatures)[1].get('duplicate')
    mark('offline receiver Docker image, verified HTTPS, HMAC and duplicate handling passed')
    run("bash",installer,"agent",str(config),str(work/"agent-data"),agent_name)
    def candidate():
        code,d=api("GET","/api/v1/containers",auth=token)
        return next((c for c in d.get("containers",[]) if c["container_name"]==canary),None)
    c=wait(candidate)
    status,baseline=api("POST","/api/v1/baseline/selection",{"expected_revision":"","additions":[{"agent_id":"offline-agent","container_id":c["container_id"],"snapshot_id":c["snapshot_id"],"cluster_code":"offline","service_code":"offline-canary","service_name":"Offline Canary"}],"removals":[]},token)
    assert status==200,(status,baseline.get("code"))
    node_id="offline-agent/"+canary
    nodes=[n for s in api("GET","/api/v1/services/status",auth=token)[1].get("services",[]) for n in s["nodes"]]
    assert nodes and not nodes[0]["restart_enabled"]
    status,baseline=api("POST","/api/v1/baseline/restart-policy",{"expected_revision":baseline["revision"],"node_id":node_id,"enabled":True},token)
    assert status==200
    mark("new baseline defaults to restart denied; only isolated canary explicitly enabled")
    wait(lambda:any(n.get("restart_allowed") for s in api("GET","/api/v1/services/status",auth=token)[1].get("services",[]) for n in s["nodes"]))
    startup=api("GET","/api/v1/nodes/startup?node_id="+urllib.parse.quote("offline-agent/"+canary,safe=""),auth=token)
    assert startup[0]==200 and startup[1]["startup"] and startup[1]['source']=='agent_report' and startup[1]['reported_at']
    assert sql("SELECT count(*) FROM avops.containers WHERE payload ? 'startup'")=="0"
    assert sql("SELECT count(*) FROM avops.documents WHERE name='startup/offline-agent' AND payload IS NULL AND encrypted_payload IS NOT NULL")=="1"
    mark("Agent WSS discovery, YAML baseline and encrypted reported startup passed")
    def history_ready():
        code,d=api('GET','/api/v1/nodes/resource-history?node_id='+urllib.parse.quote(node_id,safe='')+'&range=15m',auth=token)
        return code==200 and len([s for s in d.get('samples',[]) if s.get('cpu') is not None])>=2
    wait(history_ready,90)
    docker('stop',agent_name)
    cached=api('GET','/api/v1/nodes/startup?node_id='+urllib.parse.quote(node_id,safe=''),auth=token)
    assert cached[0]==200 and cached[1]['startup'] and cached[1]['stale'] and cached[1]['reported_at']
    docker('start',agent_name)
    wait(lambda:any(n.get('restart_allowed') for s in api('GET','/api/v1/services/status',auth=token)[1].get('services',[]) for n in s['nodes']))
    mark('real cAdvisor history and startup cache while Agent offline passed')
    status,key=api("POST","/api/v1/access-keys",{"name":"Offline QA Key","purpose":"RESTART"},token)
    assert status==201 and key["key"]["owner_id"]==login["user"]["user_id"]
    assert key["token"].startswith("rt_") and key["key"]["expires_at"] and "secret" not in key
    restart_auth="Bearer "+key["token"]
    body={"node_id":"127.0.0.1-"+canary,"reason":"isolated clean install restart","request_key":prefix+"-once"}
    status,accepted=api("POST","/api/v1/operations/restart",body,restart_auth);assert status==202
    operation=accepted["operation"]["operation_id"]
    assert api("POST","/api/v1/operations/restart",body,restart_auth)[1]["operation"]["operation_id"]==operation
    wait(lambda:api("GET","/api/v1/operations/"+operation,auth=restart_auth)[1].get("operation",{}).get("status")=="SUCCESS",90)
    assert len(api("GET","/api/v1/operations/"+operation+"/audit",auth=token)[1]["records"])>=3
    mark("self-owned expiring Token, Bearer REST restart, idempotency and stage audit passed")
    docker("restart",center_name);time.sleep(2)
    wait(lambda:api("POST","/api/v1/auth/login",{"username":"admin","password":runtime["AVOPS_BOOTSTRAP_PASSWORD"]})[0]==200)
    assert api("POST","/api/v1/operations/restart",body,restart_auth)[1]["operation"]["operation_id"]==operation
    assert sql("SELECT count(*) FROM avops.nodes WHERE active")=="1"
    status,login=api('POST','/api/v1/auth/login',{'username':'admin','password':runtime['AVOPS_BOOTSTRAP_PASSWORD']})
    token='Bearer '+login['token']
    assert api('GET','/api/v1/nodes/startup?node_id='+urllib.parse.quote(node_id,safe=''),auth=token)[1]['startup']
    assert len(api('GET','/api/v1/nodes/resource-history?node_id='+urllib.parse.quote(node_id,safe='')+'&range=15m',auth=token)[1]['samples'])>=2
    mark('startup and resource history survive center restart')
    actual_version=sql("SHOW server_version")
    assert actual_version.split()[0]=="17.11", "PostgreSQL minor version mismatch"
    proof["postgresql_version"]=actual_version
    mark("center container restart recovered baseline, Key and original idempotency")
    c=Center(args.tls_name,29444,timeout=12);c.request("GET","/");response=c.getresponse();html=response.read().decode();c.close()
    assert response.status==200
    import re
    assets=re.findall(r'(?:src|href)="(/assets/[^\"]+)"',html)
    assert assets
    for asset in assets:
        c=Center(args.tls_name,29444,timeout=12);c.request("GET",asset);response=c.getresponse();content=response.read();c.close()
        assert response.status==200 and len(content)>100
    mark("embedded Web HTML and local bundled JS/CSS served over verified HTTPS")
    proof["passed"]=True
finally:
    for name in reversed(containers):
        try:docker("rm","-f",name)
        except Exception:pass
    (work/"proof.json").write_text(json.dumps(proof,ensure_ascii=False,indent=2))
    print("Isolated containers removed; private data and proof retained at "+str(work),flush=True)
