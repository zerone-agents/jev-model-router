import unittest
from decision_common.variants import d3_model_id

class VariantTests(unittest.TestCase):
    def test_exact_variant_mapping(self):
        for variant, expected in {'edge':'vllm-sr/d3-edge', 'lite':'vllm-sr/d3-lite', 'nano':'vllm-sr/d3-nano', 'mini':'vllm-sr/d3-mini', 'flash':'vllm-sr/d3-flash', 'd3':'vllm-sr/d3'}.items():
            self.assertEqual(d3_model_id(variant), expected)

    def test_rejects_unknown_variants(self):
        for variant in ('Nano', '../nano', '', 'other'):
            with self.assertRaises(ValueError): d3_model_id(variant)

class SelectionTests(unittest.TestCase):
    def test_default_and_variant_manifest_selection(self):
        import os
        from unittest.mock import patch
        from pathlib import Path
        from decision_common.variants import selected_manifest
        with patch.dict(os.environ, {'D3_MANIFEST_DIR':'/manifests'}, clear=True):
            self.assertEqual(selected_manifest('d3'), (Path('/manifests/nano.json'), 'vllm-sr/d3-nano'))
            os.environ['D3_VARIANT'] = 'flash'
            self.assertEqual(selected_manifest('d3'), (Path('/manifests/flash.json'), 'vllm-sr/d3-flash'))
            os.environ['D3_VARIANT'] = '../nano'
            with self.assertRaises(ValueError): selected_manifest('d3')

    def test_legacy_nano_manifest(self):
        import os
        from unittest.mock import patch
        from pathlib import Path
        from decision_common.variants import selected_manifest
        with patch.dict(os.environ, {'DECISION_MANIFEST':'/app/model-manifest.json'}, clear=True):
            self.assertEqual(selected_manifest('d3'), (Path('/app/model-manifest.json'), 'vllm-sr/d3-nano'))

    def test_legacy_cli_manifest_without_environment(self):
        import os
        from unittest.mock import patch
        from pathlib import Path
        from decision_common.variants import selected_manifest
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(selected_manifest('d3', '/nano.json'), (Path('/nano.json'), 'vllm-sr/d3-nano'))
            self.assertEqual(selected_manifest('decider', '/decider.json'), (Path('/decider.json'), 'Mapika/decider-2b'))

class ManifestTests(unittest.TestCase):
    def test_bundled_manifests_are_complete_and_pinned(self):
        import json
        from pathlib import Path
        from decision_common.variants import D3_MODELS
        root = Path(__file__).resolve().parents[2] / 'd3' / 'manifests'
        for variant, model in D3_MODELS.items():
            with self.subTest(variant=variant):
                manifest = json.loads((root / (variant + '.json')).read_text())
                self.assertEqual(manifest['model'], model)
                self.assertRegex(manifest['model_revision'], r'^[a-f0-9]{40}$')
                entries = manifest['artifacts']
                self.assertTrue(any(a['path'].endswith('.safetensors') for a in entries))
                self.assertTrue(any(a['path'].endswith('.py') for a in entries))
                self.assertTrue(all(a['size'] > 0 and len(a['sha256']) == 64 for a in entries))
