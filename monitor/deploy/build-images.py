"""Build all application images from public binaries on an offline Docker host.
Never copies credentials, containers, database volumes or host configuration.
"""
import argparse, hashlib, json, pathlib, shutil, subprocess, tempfile

p=argparse.ArgumentParser()
p.add_argument('--bin', required=True)
p.add_argument('--ca', required=True)
p.add_argument('--version', required=True)
p.add_argument('--output', required=True)
a=p.parse_args()
assert all(c.isalnum() or c in '.-' for c in a.version)
out=pathlib.Path(a.output);out.mkdir(parents=True,exist_ok=True)
images=[]
for role,binary,user in [('center','center','65532:65532'),('agent','agent','0:0'),('notification-receiver','notification-receiver','65532:65532'),('cadvisor','cadvisor','0:0')]:
    with tempfile.TemporaryDirectory(prefix='avops-image-') as folder:
        root=pathlib.Path(folder)
        shutil.copyfile(pathlib.Path(a.bin)/binary,root/binary)
        (root/binary).chmod(0o755)
        shutil.copyfile(a.ca,root/'ca-bundle.crt');(root/'ca-bundle.crt').chmod(0o644)
        (root/'Dockerfile').write_text('FROM scratch\nCOPY ca-bundle.crt /etc/ssl/certs/ca-certificates.crt\nCOPY '+binary+' /'+binary+'\nUSER '+user+'\nENTRYPOINT ["/'+binary+'"]\n')
        tag='avops-'+role+':'+a.version
        subprocess.run(['docker','build','--network=none','-t',tag,str(root)],check=True,stdout=subprocess.DEVNULL)
        info=json.loads(subprocess.check_output(['docker','image','inspect',tag]))[0]
        assert info['Architecture']=='amd64' and info['Os']=='linux'
        images.append({'image':tag,'id':info['Id'],'platform':'linux/amd64','binary_sha256':hashlib.sha256((root/binary).read_bytes()).hexdigest()})
with (out/('avops-images-'+a.version+'-linux-amd64.tar')).open('wb') as f:
    subprocess.run(['docker','save']+[i['image'] for i in images],stdout=f,check=True)
(out/'image-manifest.json').write_text(json.dumps({'version':a.version,'cadvisor_version':'0.60.6','web':'embedded in center','images':images},indent=2)+'\n')
print('Built and exported '+str(len(images))+' offline application images')
