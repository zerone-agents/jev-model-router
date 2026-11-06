import json
import os
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from decision_common import server

class SnapshotPinningTests(unittest.TestCase):
    def test_switch_during_verification_loads_verified_snapshot(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            first, second = root / 'first', root / 'second'
            first.mkdir(); second.mkdir()
            current = root / 'current'
            current.symlink_to(first)
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps({'model':'vllm-sr/d3-nano','model_revision':'r'}))
            settings = server.Settings(token='x', kind='d3', model_dir=str(current), alias='d3', revision='r', device='cuda', threads=1, context=8192, deadline=60)
            seen = []
            def verify(path, data):
                seen.append(('verify', path))
                current.unlink(); current.symlink_to(second)
            class Model:
                def to(self, device): return self
                def eval(self): return self
            def load(path, **kwargs):
                seen.append(('load', path)); return Model()
            torch = types.SimpleNamespace(set_num_threads=lambda n: None, cuda=types.SimpleNamespace(is_available=lambda: True))
            transformers = types.SimpleNamespace(AutoModel=types.SimpleNamespace(from_pretrained=load))
            with patch.dict(os.environ, {'DECISION_MANIFEST':str(manifest)}, clear=True), patch.dict(sys.modules, {'torch':torch, 'transformers':transformers}), patch.object(server, 'verify_snapshot', verify), patch.object(server, 'DecisionBackend', return_value=lambda _: None):
                server.Runtime(settings)
            self.assertEqual(seen, [('verify', str(first)), ('load', str(first))])

    def test_rejects_configured_variant_when_manifest_model_differs(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'mini.json').write_text(json.dumps({'model':'vllm-sr/d3-flash','model_revision':'r'}))
            env = {'DECISION_BACKEND':'d3', 'DECISION_API_KEY':'fake', 'DECISION_MODEL_DIR':str(root), 'D3_VARIANT':'mini', 'D3_MANIFEST_DIR':str(root)}
            with patch.dict(os.environ, env, clear=True), self.assertRaises(ValueError):
                server.read_settings()
