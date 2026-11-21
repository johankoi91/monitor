#!/usr/bin/env python3
"""Build a minimal, credential-free Native/Web server image context from pinned runtimes."""
import argparse,json,os,re,shutil,subprocess,tempfile
from pathlib import Path
p=argparse.ArgumentParser()
p.add_argument('--native-context',type=Path,required=True);p.add_argument('--web-image',required=True)
p.add_argument('--supervisor',type=Path,required=True);p.add_argument('--web-binary',type=Path,required=True)
p.add_argument('--runtime-directory',type=Path,default=Path(__file__).resolve().parents[2]/'websdk/custom-video')
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_./:@-]*',a.web_image):raise SystemExit('valid Web runtime image required')
a.output.mkdir(parents=True,exist_ok=False)
container=subprocess.check_output(['docker','create',a.web_image]).decode().strip()
system=a.output/'system';system.mkdir()
def copy_container(source,target=None,optional=False):
 destination=system/(target or source).lstrip('/');destination.parent.mkdir(parents=True,exist_ok=True)
 result=subprocess.run(['docker','cp','-L',container+':'+source,str(destination)],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
 if result.returncode and not optional:raise RuntimeError('runtime asset unavailable: '+source)
try:
 node='/usr/local/bin/node';chrome='/ms-playwright/chromium_headless_shell-1194/chrome-linux/headless_shell'
 linked=subprocess.check_output(['docker','run','--rm','--network','none','--read-only','--entrypoint','/usr/bin/ldd',a.web_image,node,chrome],universal_newlines=True)
 # NSS loads its crypto providers at the first TLS request, outside Chromium's
 # static dependency graph. Include their own closure, including SQLite.
 nss_providers=['/lib/x86_64-linux-gnu/'+name for name in ['libsoftokn3.so','libfreebl3.so','libfreeblpriv3.so','libnssckbi.so']]
 linked+='\n'+subprocess.check_output(['docker','run','--rm','--network','none','--read-only','--entrypoint','/usr/bin/ldd',a.web_image,*nss_providers],universal_newlines=True)
 if 'not found' in linked:raise RuntimeError('Web runtime has missing dependencies')
 native_linked=subprocess.check_output(['docker','run','--rm','--network','none','--read-only','-e','LD_LIBRARY_PATH=/opt/agora_sdk:/opt/rtc-probe/native-libs','--entrypoint','/usr/bin/ldd',a.web_image,'/rtc-probe'],universal_newlines=True)
 libraries=set(re.findall(r'(?:=>\s*)?(/[^\s:]+)',linked+'\n'+native_linked))
 for library in libraries:
  if library.endswith(':') or library in [node,chrome,'/rtc-probe'] or library.startswith(('/opt/agora_sdk/','/opt/rtc-probe/native-libs/')):continue
  copy_container(library)
 for library in nss_providers:copy_container(library)
 for path in [node,'/ms-playwright','/opt/rtc-probe/websdk/custom-video/node_modules','/etc/ssl/certs/ca-certificates.crt','/usr/share/fonts','/etc/fonts','/usr/share/zoneinfo','/etc/os-release']:
  copy_container(path)
 for name in ['libpthread.so.0','libdl.so.2','librt.so.1','libresolv.so.2','libnss_dns.so.2','libnss_files.so.2']:
  copy_container('/lib/x86_64-linux-gnu/'+name,optional=True)
 # Preserve installed runtime license metadata without copying compiler/tooling trees.
 copy_container('/usr/share/doc',optional=True)
finally:subprocess.run(['docker','rm',container],check=True,stdout=subprocess.DEVNULL)
for source,name in [(a.native_context/'rtc-probe','rtc-probe'),(a.supervisor,'probe-server'),(a.web_binary,'rtc-web-probe')]:
 shutil.copyfile(source,system/name);(system/name).chmod(0o755)
for source,target in [(a.native_context/'sdk','opt/agora_sdk'),(a.native_context/'test_data','opt/rtc-probe/test_data')]:shutil.copytree(source,system/target)
shutil.copyfile(a.native_context/'system/opt/rtc-probe/libroute_capture.so',system/'opt/rtc-probe/libroute_capture.so')
lib=system/'opt/rtc-probe/native-libs';lib.mkdir(parents=True,exist_ok=True)
for pattern in ['libav*.so.*','libswresample.so.*']:
 for source in (a.native_context/'system/opt/rtc-probe/runtime-libs').glob(pattern):shutil.copyfile(source,lib/source.name)
runtime=system/'opt/rtc-probe/websdk/custom-video';runtime.mkdir(parents=True,exist_ok=True)
for name in ['worker.mjs','server.mjs','contract.mjs','remote-stats.mjs','package.json','package-lock.json']:shutil.copyfile(a.runtime_directory/name,runtime/name)
for name in ['browser','sdk','fixtures']:shutil.copytree(a.runtime_directory/name,runtime/name)
(system/'etc/passwd').write_text('root:x:0:0:root:/root:/bin/false\nprobe:x:65532:65532:probe:/tmp:/bin/false\n')
(system/'etc/group').write_text('root:x:0:\nprobe:x:65532:\n')
(system/'etc/nsswitch.conf').write_text('passwd: files\ngroup: files\nhosts: files dns\n')
(a.output/'Dockerfile').write_text('''FROM scratch
COPY system/ /
WORKDIR /opt/rtc-probe/websdk/custom-video
ENV PATH=/usr/local/bin:/usr/bin:/bin HOME=/tmp
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 PLAYWRIGHT_BROWSERS_PATH=/ms-playwright PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=1
ENV CHROME_PATH=/ms-playwright/chromium_headless_shell-1194/chrome-linux/headless_shell
ENV AVOPS_PROBE_WEB_RUNTIME=/opt/rtc-probe/websdk/custom-video
ENV AVOPS_PROBE_HOST_DIR=/host AVOPS_PROBE_DATA_DIR=/data AVOPS_PROBE_NATIVE_CAPACITY=1 AVOPS_PROBE_WEB_CAPACITY=1
LABEL avops.probe.schema="2" avops.probe.service="rtc" avops.probe.clients="native,web" org.opencontainers.image.version="V1.0"
USER 65532:65532
ENTRYPOINT ["/probe-server"]
''')
print('Minimal V1.0 Native/Web server context prepared; compiler and development runtimes excluded.')
