"""Check offline image tags and embedded binary hashes without a Docker daemon."""
import hashlib, io, json, pathlib, sys, tarfile
root=pathlib.Path(sys.argv[1]);version=(root/'VERSION').read_text().strip()
manifest=json.loads((root/'images/image-manifest.json').read_text())
with tarfile.open(root/'images/avops-images-linux-amd64.tar') as archive:
    images=json.load(archive.extractfile('manifest.json'))
    expected={'avops-'+name+':'+version:name for name in ['center','agent','notification-receiver','cadvisor']}
    assert {tag for image in images for tag in image['RepoTags']}==set(expected)
    for image in images:
        name=expected[image['RepoTags'][0]]
        wanted=hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()
        found=None
        for layer in image['Layers']:
            with tarfile.open(fileobj=io.BytesIO(archive.extractfile(layer).read())) as data:
                for entry in data:
                    path=entry.name.lstrip('./')
                    if path==name and entry.isfile():found=hashlib.sha256(data.extractfile(entry).read()).hexdigest()
                    assert not (path.endswith(('.pem','.key','.env')) or path.startswith('state/')), 'private image content'
        assert found==wanted, 'image binary does not match delivery binary: '+name
with tarfile.open(root/'images/avops-postgres-17-linux-amd64.tar') as archive:
    pg=json.load(archive.extractfile('manifest.json'))[0]
    config=json.load(archive.extractfile(pg['Config']))
    env=dict(e.split('=',1) for e in config['config']['Env'])
    assert env['PG_VERSION'].split('-')[0]=='17.11'
    assert config['architecture']=='amd64' and config['os']=='linux'
    manifest['external_images']=[{'image':pg['RepoTags'][0],'id':'sha256:'+pg['Config'].split('/')[-1].removesuffix('.json'),'version':'17.11','platform':'linux/amd64','archive':'avops-postgres-17-linux-amd64.tar'}]
(root/'images/image-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print('Verified all five service images, PostgreSQL 17.11 and exact application binaries')
