import os
import unittest
from unittest.mock import patch
from jev_laya.config import Settings
from jev_laya.encoding import Limits

class ConfigTests(unittest.TestCase):
    def test_head_defaults_and_environment_override(self):
        with patch.dict(os.environ, {'LAYA_API_KEY': 'test'}, clear=True):
            self.assertEqual(Settings.from_env().head, 512)
            os.environ['LAYA_HEAD_TOKENS'] = '1024'
            self.assertEqual(Settings.from_env().head, 1024)
        for head in (0, -1, 8189):
            with self.subTest(head=head):
                with self.assertRaises(ValueError): Limits(head=head)
                with self.assertRaises(ValueError): Settings(token='test', model_dir='/unused', head=head)

    def test_device_default_override_and_invalid(self):
        with patch.dict(os.environ, {'LAYA_API_KEY': 'test'}, clear=True):
            self.assertEqual(Settings.from_env().device, 'cpu')
            os.environ['LAYA_DEVICE'] = 'mps'
            self.assertEqual(Settings.from_env().device, 'mps')
            os.environ['LAYA_DEVICE'] = 'cuda'
            with self.assertRaises(ValueError): Settings.from_env()

    def test_unavailable_mps_does_not_load_or_fallback(self):
        from jev_laya.model import LayaBackend
        with patch('torch.backends.mps.is_available', return_value=False), patch('laya.load') as load:
            with self.assertRaisesRegex(RuntimeError, 'MPS device is unavailable'):
                LayaBackend('/unused', device='mps')
            load.assert_not_called()

    def test_server_passes_device_and_head_to_worker(self):
        from jev_laya.server import application
        with patch.dict(os.environ, {'LAYA_API_KEY': 'test', 'LAYA_DEVICE': 'mps', 'LAYA_HEAD_TOKENS': '1024'}, clear=True), patch('jev_laya.server.Supervisor') as supervisor:
            application()
            settings = supervisor.call_args.args[0]
            self.assertEqual(settings.device, 'mps')
            self.assertEqual(settings.head, 1024)
