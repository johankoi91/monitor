"""Build-context checks use local fixtures; no downloads, Docker or RTC calls."""
import contextlib
import importlib.util
import io
from pathlib import Path
import re
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


class RuntimeAssets(unittest.TestCase):
    def test_web_context_contains_all_relative_worker_imports(self):
        spec = importlib.util.spec_from_file_location('build_web_runtime', ROOT / 'tools/build/build_web_runtime.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            args = ['build', '--output', str(root / 'context'), '--cache', str(root / 'cache')]
            with patch.object(sys, 'argv', args), patch.object(module, 'ASSETS', {}), patch.object(module.subprocess, 'run'), contextlib.redirect_stdout(io.StringIO()):
                module.main()
            runtime = root / 'context/custom-video'
            for name in re.findall(r"from ['\"](\./[^'\"]+)['\"]", (runtime / 'worker.mjs').read_text()):
                self.assertTrue((runtime / name).is_file(), 'missing worker dependency: ' + name)
            self.assertTrue((runtime / 'sdk/AgoraRTC_N.js').is_file())
            self.assertTrue((runtime / 'fixtures/probe.webm').is_file())


if __name__ == '__main__':
    unittest.main()
