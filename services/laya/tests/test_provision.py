import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from jev_laya.provision import provision, verify, ArtifactError


class ProvisionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.data = b'verified artifact'
        self.manifest = {'model':'convaiinnovations/laya-multilingual', 'model_revision':'a'*40,
            'artifacts':[{'path':'weights.bin','size':len(self.data),'sha256':hashlib.sha256(self.data).hexdigest()}]}
    def tearDown(self): self.temp.cleanup()
    def download(self, url, target): target.write_bytes(self.data)

    def test_verified_atomic_cache_needs_no_network(self):
        current = provision(self.root, self.manifest, self.download)
        verify(current, self.manifest)
        again = provision(self.root, self.manifest, lambda *args: self.fail('network on cached model'))
        self.assertEqual(current.resolve(), again.resolve())

    def test_interruption_never_marks_partial_artifacts_ready(self):
        def interrupted(url, target):
            target.write_bytes(b'partial'); raise OSError('interrupted')
        with self.assertRaises(OSError): provision(self.root, self.manifest, interrupted)
        self.assertFalse((self.root/'current').exists())

    def test_bad_hash_and_missing_file_rejected(self):
        with self.assertRaises(ArtifactError):
            provision(self.root, self.manifest, lambda url,p: p.write_bytes(b'wrong'))
        with self.assertRaises(ArtifactError): verify(self.root, self.manifest)

    def test_unsafe_manifest_and_artifact_symlink_rejected(self):
        for path in ['../escape', '/absolute', 'a/../../escape']:
            manifest = json.loads(json.dumps(self.manifest)); manifest['artifacts'][0]['path']=path
            with self.subTest(path=path), self.assertRaises(ArtifactError):
                provision(self.root, manifest, self.download)
        target = self.root/'outside'; target.write_bytes(self.data)
        (self.root/'weights.bin').symlink_to(target)
        with self.assertRaises(ArtifactError): verify(self.root, self.manifest)

    def test_failed_new_revision_preserves_previous(self):
        old = provision(self.root, self.manifest, self.download).resolve()
        other = dict(self.manifest, model_revision='b'*40)
        with self.assertRaises(ArtifactError): provision(self.root, other, lambda url,p: p.write_bytes(b'wrong'))
        self.assertEqual((self.root/'current').resolve(), old)
        verify(old, self.manifest)

    def test_packaged_manifest_loads_for_offline_initialization(self):
        from jev_laya.provision import load_manifest
        manifest = load_manifest()
        self.assertEqual(manifest['model'], 'convaiinnovations/laya-multilingual')
        self.assertGreater(sum(entry['size'] for entry in manifest['artifacts']), 600_000_000)

    def test_concurrent_provisioners_publish_one_verified_revision(self):
        from concurrent.futures import ThreadPoolExecutor
        import threading
        import time
        calls = []
        lock = threading.Lock()
        def download(url, target):
            with lock: calls.append(url)
            time.sleep(0.03)
            target.write_bytes(self.data)
        with ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(provision, self.root, self.manifest, download) for _ in range(2)]
            paths = [future.result(timeout=3) for future in futures]
        self.assertEqual(len(calls), 1)
        self.assertEqual(paths[0].resolve(), paths[1].resolve())
        verify(paths[0], self.manifest)
