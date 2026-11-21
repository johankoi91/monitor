"""Isolated deployment guards; never contacts Docker or launches a probe."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1] / 'tools/deploy'

def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

class DeploymentGuards(unittest.TestCase):
    def test_invalid_resources_do_not_touch_files_or_docker(self):
        module = load('install')
        for value in ['0', '-1']:
            with tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                args = ['install', '--image', 'v2', '--environment', str(root/'env'), '--host-directory', str(root/'host'), '--data-directory', str(root/'data'), '--memory-mib', value]
                with patch.object(sys, 'argv', args), patch.object(module.os, 'geteuid', return_value=0), patch.object(module.subprocess, 'check_output') as docker:
                    with self.assertRaisesRegex(RuntimeError, 'resource'):
                        module.main()
                    docker.assert_not_called()
                    self.assertEqual(list(root.iterdir()), [])

    def test_old_environment_is_rejected_before_docker(self):
        module = load('install')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env = root/'env'; env.write_text('RTC_PROBE_ID=old\n'); env.chmod(0o600)
            args = ['install', '--image', 'v2', '--environment', str(env), '--host-directory', str(root/'host'), '--data-directory', str(root/'data')]
            with patch.object(sys, 'argv', args), patch.object(module.os, 'geteuid', return_value=0), patch.object(module.subprocess, 'check_output') as docker:
                with self.assertRaisesRegex(RuntimeError, 'unsupported'):
                    module.main()
                docker.assert_not_called()
                self.assertFalse((root/'data').exists())

    def test_shared_budget_rejects_oversubscribed_clients_before_docker(self):
        module = load('install')
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            env=root/'env'
            env.write_text('AVOPS_PROBE_CENTER_URL=wss://center.example/api/v1/probes/connect\nAVOPS_PROBE_ENROLL_TOKEN='+('s'*32)+'\nAVOPS_PROBE_NATIVE_CAPACITY=8\nAVOPS_PROBE_WEB_CAPACITY=2\n')
            env.chmod(0o600)
            args=['install','--image','v2','--environment',str(env),'--host-directory',str(root/'host'),'--data-directory',str(root/'data'),'--cpus','2','--memory-mib','2048']
            with patch.object(sys,'argv',args),patch.object(module.os,'geteuid',return_value=0),patch.object(module.subprocess,'check_output') as docker:
                with self.assertRaisesRegex(RuntimeError,'budget'):module.main()
                docker.assert_not_called()
                self.assertFalse((root/'data').exists())

    def test_v1_upgrade_refuses_before_any_mutation(self):
        module = load('upgrade')
        with tempfile.TemporaryDirectory() as temporary:
            stage = Path(temporary)/'stage'
            responses = [{'Config':{'Labels':{'avops.probe.schema':'1'}}}, {'Config':{'Labels':{'avops.probe.schema':'2'}}}]
            with patch.object(sys, 'argv', ['upgrade','--container','probe','--image','new','--stage',str(stage)]), patch.object(module.os,'geteuid',return_value=0), patch.object(module,'api',side_effect=responses) as api:
                with self.assertRaisesRegex(RuntimeError, 'only v2'):
                    module.main()
                self.assertEqual([call.args[0] for call in api.call_args_list], ['GET','GET'])
                self.assertFalse(stage.exists())

    def test_install_prepares_identity_without_a_separate_command(self):
        module = load('install')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env = root / 'probe.env'
            env.write_text('AVOPS_PROBE_CENTER_URL=wss://center.example/api/v1/probes/connect\nAVOPS_PROBE_ENROLL_TOKEN=' + ('s' * 32) + '\n')
            env.chmod(0o600)
            args = ['install', '--image', 'fixture', '--environment', str(env), '--host-directory', str(root / 'host'), '--data-directory', str(root / 'clients'), '--host-name', 'fixture-host', '--host-address', '192.0.2.10']
            image = [{'Config': {'Labels': {'avops.probe.schema': '2', 'avops.probe.clients': 'native,web', 'avops.probe.service': 'rtc'}}}]
            with patch.object(sys, 'argv', args), patch.object(module.os, 'geteuid', return_value=0), patch.object(module.os, 'chown'), patch.object(module.subprocess, 'check_output', side_effect=['', json.dumps(image)]), patch.object(module.subprocess, 'run') as run, contextlib.redirect_stdout(io.StringIO()):
                module.main()
            identity = json.loads((root / 'host/server-identity-v2.json').read_text())
            self.assertEqual(identity['host']['host_address'], '192.0.2.10')
            self.assertEqual(identity['host']['host_name'], 'fixture-host')
            self.assertEqual((root / 'clients').stat().st_mode & 0o777, 0o700)
            self.assertEqual(run.call_args.args[0][:2], ['docker', 'run'])
            self.assertNotIn(identity['secret'], str(run.call_args))

    def test_validate_only_creates_no_identity_or_data(self):
        module = load('install')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env = root / 'probe.env'
            env.write_text('AVOPS_PROBE_CENTER_URL=wss://center.example/api/v1/probes/connect\nAVOPS_PROBE_ENROLL_TOKEN=' + ('s' * 32) + '\n')
            env.chmod(0o600)
            image = [{'Config': {'Labels': {'avops.probe.schema': '2', 'avops.probe.clients': 'native,web', 'avops.probe.service': 'rtc'}}}]
            args = ['install', '--validate-only', '--image', 'fixture', '--environment', str(env), '--host-directory', str(root / 'host'), '--data-directory', str(root / 'clients')]
            with patch.object(sys, 'argv', args), patch.object(module.os, 'geteuid', return_value=0), patch.object(module.subprocess, 'check_output', side_effect=['', json.dumps(image)]), patch.object(module.subprocess, 'run') as run, contextlib.redirect_stdout(io.StringIO()):
                module.main()
            run.assert_not_called()
            self.assertEqual([p.name for p in root.iterdir()], ['probe.env'])

    def test_prepared_server_identity_is_stable_and_private(self):
        module = load('install')
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary) / 'host'
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                module.prepare_host(directory, 'fixture-host', '192.0.2.10')
                identity = json.loads((directory / 'server-identity-v2.json').read_text())
                module.prepare_host(directory, 'fixture-host', '192.0.2.10')
            self.assertEqual(identity, json.loads((directory / 'server-identity-v2.json').read_text()))
            self.assertEqual(identity['host']['host_address'], '192.0.2.10')
            self.assertEqual((directory / 'server-identity-v2.json').stat().st_mode & 0o777, 0o600)
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
            self.assertNotIn(identity['secret'], output.getvalue())

if __name__ == '__main__':
    unittest.main()
