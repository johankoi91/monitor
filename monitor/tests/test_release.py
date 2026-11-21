"""Public source/package boundaries are verified without contacting Docker."""
import argparse
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import tarfile
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release', REPO / 'monitor/scripts/release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseAssembly(unittest.TestCase):
    def test_generated_trees_and_private_files_are_excluded_from_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'repo'; root.mkdir()
            for name in ['src/main.go', 'monitor/internal/webui/static/index.html', 'build/output', 'releases/duplicate/code.go', '.env', 'private.key', 'node_modules/dependency/index.js', 'admin-curl.conf']:
                file = root / name; file.parent.mkdir(parents=True, exist_ok=True); file.write_text('fixture')
            output = Path(temporary) / 'source'
            release.copy_public_source(root, output)
            self.assertEqual(sorted(str(file.relative_to(output)) for file in output.rglob('*') if file.is_file()), ['monitor/internal/webui/static/index.html', 'src/main.go'])

    def test_bundle_keeps_one_probe_installer_and_a_complete_checksum_manifest(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); repo = root / 'repo'; repo.mkdir()
            images = root / 'images'; images.mkdir(); binary = root / 'bin'; binary.mkdir()
            for name in release.DOCUMENTS: (repo / name).write_text('V1.0' if name == 'VERSION' else 'fixture document')
            for name in release.BINARIES: (binary / name).write_text('public fixture binary')
            for relative in ['monitor/deploy/install.py', 'monitor/deploy/backup-postgres.sh', 'rtc_probe/tools/deploy/install.py', 'rtc_probe/tools/deploy/upgrade.py']:
                file = repo / relative; file.parent.mkdir(parents=True, exist_ok=True); file.write_text('# fixture public tool')
            shutil.copyfile(REPO / 'monitor/deploy/verify-bundle-images.py', repo / 'monitor/deploy/verify-bundle-images.py')
            for archive in release.ARCHIVES:
                role = archive[:-len('-linux-amd64.tar')]
                postgres = 'postgres-17' in role
                tag = 'docker.m.daocloud.io/library/postgres:17' if postgres else role + ':V1.0'
                config = {'architecture': 'amd64', 'os': 'linux', 'config': {'Labels': {'org.opencontainers.image.version': 'V1.0'}, 'Env': ['PG_VERSION=17.11'] if postgres else []}}
                with tarfile.open(images / archive, 'w') as output:
                    for name, value in [('manifest.json', [{'Config': 'config.json', 'RepoTags': [tag]}]), ('config.json', config)]:
                        data = json.dumps(value).encode(); item = tarfile.TarInfo(name); item.size = len(data); output.addfile(item, io.BytesIO(data))
            with contextlib.redirect_stdout(io.StringIO()):
                release.assemble_bundle(argparse.Namespace(repo=repo, bin=binary, images=images, output=repo / 'delivery'))
            output = repo / 'delivery/avops-monitor-V1.0-linux-amd64'
            self.assertEqual(sorted(file.name for file in (output / 'deploy/probe').iterdir()), ['install.py', 'upgrade.py'])
            self.assertFalse((output / 'source/delivery/avops-monitor-V1.0-linux-amd64').exists())
            for line in (output / 'SHA256SUMS').read_text().splitlines():
                digest, name = line.split('  ', 1)
                self.assertEqual(release.digest(output / name), digest, name)
            self.assertTrue((output.parent / (output.name + '.tar.gz')).is_file())


if __name__ == '__main__':
    unittest.main()
