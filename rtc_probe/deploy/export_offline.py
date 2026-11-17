#!/usr/bin/env python3
"""Export a local, already-built image and public install files; no credentials."""
import argparse, gzip, hashlib, json, pathlib, shutil, subprocess
p=argparse.ArgumentParser()
p.add_argument('component',choices=['center','probe'])
p.add_argument('output',type=pathlib.Path)
p.add_argument('--release',required=True)
a=p.parse_args()
image=('avops-center:' if a.component=='center' else 'rtc-probe:sdk-')+a.release
info=json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]
a.output.mkdir(parents=True,exist_ok=False)
root=pathlib.Path(__file__).resolve().parent
shutil.copyfile(root.parent/'README.md',a.output/'README.md')
shutil.copyfile(root/'upgrade.py',a.output/'upgrade.py')
if a.component=='center':
 schema=root/'rtc_history.sql'
 if not schema.exists():schema=root.parent.parent/'monitor/internal/postgres/rtc_history.sql'
 shutil.copyfile(schema,a.output/'rtc_history.sql')
path=a.output/'image.tar.gz'
with path.open('wb') as raw:
 with gzip.GzipFile(fileobj=raw,mode='wb',mtime=0) as zipped:
  proc=subprocess.Popen(['docker','save',image],stdout=subprocess.PIPE)
  shutil.copyfileobj(proc.stdout,zipped)
  if proc.wait()!=0:raise RuntimeError('docker save failed')
h=hashlib.sha256()
with path.open('rb') as f:
 for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
manifest={'component':a.component,'image':image,'image_id':info['Id'],'platform':info['Os']+'/'+info['Architecture'],'sha256':h.hexdigest(),'bytes':path.stat().st_size,'contains_secrets':False}
(a.output/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
checks=[]
for file in sorted(a.output.iterdir()):
 if file.is_file() and file.name!='SHA256SUMS':
  digest=hashlib.sha256()
  with file.open('rb') as f:
   for chunk in iter(lambda:f.read(1024*1024),b''):digest.update(chunk)
  checks.append(digest.hexdigest()+'  '+file.name)
(a.output/'SHA256SUMS').write_text('\n'.join(checks)+'\n')
print(json.dumps(manifest))
