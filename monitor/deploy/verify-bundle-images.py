#!/usr/bin/env python3
"""Verify exact formal image names, version and architecture without loading them."""
import json,sys,tarfile
from pathlib import Path
root=Path(sys.argv[1]);assert (root/'VERSION').read_text().strip()=='V1.0'
expected={'avops-'+name+'-linux-amd64.tar':'avops-'+name+':V1.0' for name in ['center','agent','notification-receiver','cadvisor','probe']}
expected['avops-postgres-17-linux-amd64.tar']='docker.m.daocloud.io/library/postgres:17'
images=[]
for archive,tag in expected.items():
 with tarfile.open(root/'images'/archive) as file:
  entries=json.load(file.extractfile('manifest.json'));entry=next(value for value in entries if tag in (value.get('RepoTags')or[]));config=json.load(file.extractfile(entry['Config']))
  assert config['architecture']=='amd64' and config['os']=='linux',tag
  if archive!='avops-postgres-17-linux-amd64.tar':assert config['config']['Labels']['org.opencontainers.image.version']=='V1.0',tag
  else:assert any(value.startswith('PG_VERSION=17.11') for value in config['config']['Env'])
  images.append({'image':tag,'archive':archive,'id':'sha256:'+Path(entry['Config']).stem,'platform':'linux/amd64'})
(root/'images/verified-images.json').write_text(json.dumps({'version':'V1.0','images':images},indent=2)+'\n')
print('Six V1.0 delivery images verified.')
