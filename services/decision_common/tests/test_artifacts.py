import hashlib
import tempfile
import unittest
from pathlib import Path

from decision_common.artifacts import ArtifactError, verify_snapshot


class SnapshotVerificationTests(unittest.TestCase):
    def test_accepts_original_and_rejects_changed_or_missing_files(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            data = b'original'
            (root / 'model').write_bytes(data)
            manifest = {'artifacts': [{'path': 'model', 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}]}
            verify_snapshot(root, manifest)
            (root / 'model').write_bytes(b'tampered')
            with self.assertRaises(ArtifactError):
                verify_snapshot(root, manifest)
            (root / 'model').unlink()
            with self.assertRaises(ArtifactError):
                verify_snapshot(root, manifest)

    def test_rejects_traversal_and_symlink(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'original').write_bytes(b'a')
            (root / 'link').symlink_to(root / 'original')
            for name in ('../original', 'link'):
                manifest = {'artifacts': [{'path': name, 'size': 1, 'sha256': hashlib.sha256(b'a').hexdigest()}]}
                with self.subTest(name=name), self.assertRaises(ArtifactError):
                    verify_snapshot(root, manifest)

    def test_unlisted_model_code_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'model').write_bytes(b'a')
            manifest = {'artifacts': [{'path': 'model', 'size': 1, 'sha256': hashlib.sha256(b'a').hexdigest()}]}
            for name in ('extra.py', 'cached.pyc'):
                code = root / name
                code.write_bytes(b'code')
                with self.subTest(name=name), self.assertRaises(ArtifactError):
                    verify_snapshot(root, manifest)
                code.unlink()
