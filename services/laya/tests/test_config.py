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
