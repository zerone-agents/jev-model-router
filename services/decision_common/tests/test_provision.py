import hashlib
import tempfile
import unittest
from pathlib import Path

from decision_common.provision import ArtifactError, provision, verify


class ProvisionTests(unittest.TestCase):
    def test_failed_download_preserves_current_and_cached_use_is_offline(self):
        payload = b'first'
        def manifest(data, revision):
            return {'model': 'example/model', 'model_revision': revision * 40, 'artifacts': [{'path': 'model.bin', 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}]}
        with tempfile.TemporaryDirectory() as temp:
            first = manifest(payload, 'a')
            current = provision(temp, first, 'example/model', lambda url, path: path.write_bytes(payload))
            verify(current, first)
            cached = provision(temp, first, 'example/model', lambda url, path: self.fail('unexpected download'))
            self.assertEqual(current, cached)
            second = manifest(b'second', 'b')
            with self.assertRaises(OSError):
                provision(temp, second, 'example/model', lambda url, path: (_ for _ in ()).throw(OSError('failed')))
            verify(current, first)

    def test_rejects_unexpected_origin(self):
        data = b'a'
        manifest = {'model': 'attacker/model', 'model_revision': 'a' * 40, 'artifacts': [{'path': 'model', 'size': 1, 'sha256': hashlib.sha256(data).hexdigest()}]}
        with tempfile.TemporaryDirectory() as temp, self.assertRaises(ArtifactError):
            provision(temp, manifest, 'example/model')

    def test_concurrent_provisioners_publish_one_verified_revision(self):
        from concurrent.futures import ThreadPoolExecutor
        import threading
        import time

        data = b'one verified model'
        manifest = {'model': 'example/model', 'model_revision': 'a' * 40,
                    'artifacts': [{'path': 'model', 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}]}
        calls = []
        lock = threading.Lock()
        def download(url, target):
            with lock:
                calls.append(url)
            time.sleep(0.03)
            target.write_bytes(data)
        with tempfile.TemporaryDirectory() as temp:
            with ThreadPoolExecutor(max_workers=2) as pool:
                futures = [pool.submit(provision, temp, manifest, 'example/model', download) for _ in range(2)]
                paths = [future.result(timeout=5) for future in futures]
            self.assertEqual(len(calls), 1)
            self.assertEqual(paths[0].resolve(), paths[1].resolve())
            verify(paths[0], manifest)
