#!/usr/bin/env python3
"""Prepare a reproducible Linux amd64 Web probe image context, without credentials."""
import argparse,hashlib,os,shutil,subprocess,urllib.request
from pathlib import Path

ASSETS={
 'node-linux.tar.xz':('https://nodejs.org/dist/v22.16.0/node-v22.16.0-linux-x64.tar.xz','f4cb75bb036f0d0eddf6b79d9596df1aaab9ddccd6a20bf489be5abe9467e84e'),
 'chromium-headless-linux.zip':('https://cdn.playwright.dev/dbazure/download/playwright/builds/chromium/1194/chromium-headless-shell-linux.zip','d745c7210cc88b73c7991a22c9624819960d84bc3de77ac7336d2de841a8c91f'),
}
def digest(file):
 h=hashlib.sha256()
 with file.open('rb') as source:
  for block in iter(lambda:source.read(1<<20),b''):h.update(block)
 return h.hexdigest()
def main():
 parser=argparse.ArgumentParser();parser.add_argument('--output',required=True,type=Path);parser.add_argument('--cache',type=Path);parser.add_argument('--go',default='go');args=parser.parse_args()
 args.output=args.output.resolve()
 root=Path(__file__).resolve().parents[2]/'websdk';cache=args.cache or root/'build/downloads';cache.mkdir(parents=True,exist_ok=True)
 for name,(url,expected) in ASSETS.items():
  file=cache/name
  if not file.exists() or digest(file)!=expected:
   temporary=file.with_suffix(file.suffix+'.partial')
   with urllib.request.urlopen(url,timeout=120) as response,temporary.open('wb') as output:shutil.copyfileobj(response,output)
   if digest(temporary)!=expected:raise RuntimeError('SHA256 mismatch: '+name)
   temporary.replace(file)
 args.output.mkdir(parents=True,exist_ok=False);build=args.output/'build';build.mkdir()
 for name in ASSETS:shutil.copyfile(cache/name,build/name)
 for name in ['Dockerfile','.dockerignore']:shutil.copyfile(root/name,args.output/name)
 runtime=args.output/'custom-video';runtime.mkdir()
 for name in ['package.json','package-lock.json','worker.mjs','server.mjs','contract.mjs','remote-stats.mjs']:shutil.copyfile(root/'custom-video'/name,runtime/name)
 for name in ['browser','sdk','fixtures']:shutil.copytree(root/'custom-video'/name,runtime/name)
 env=dict(os.environ,GOOS='linux',GOARCH='amd64',CGO_ENABLED='0',GOTOOLCHAIN='local')
 subprocess.run([args.go,'-C',str(root.parent),'build','-trimpath','-ldflags','-s -w','-o',str(build/'rtc-web-probe'),'./cmd/rtc-web-probe'],env=env,check=True)
 print('Image context prepared: '+str(args.output))
if __name__=='__main__':main()
