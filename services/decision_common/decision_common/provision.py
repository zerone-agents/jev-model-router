"""Explicit network provisioning and read-only verification of pinned artifacts."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import tempfile
import urllib.request
import uuid
from .variants import selected_manifest


class ArtifactError(Exception):
    pass


def _entries(manifest):
    seen = set()
    for entry in manifest['artifacts']:
        name = entry['path']
        path = PurePosixPath(name)
        if not name or path.is_absolute() or '..' in path.parts or str(path) != name or name in seen:
            raise ArtifactError('Invalid artifact path')
        if not isinstance(entry['size'], int) or entry['size'] < 0 or not re.fullmatch('[0-9a-f]{64}', entry['sha256']):
            raise ArtifactError('Invalid artifact digest')
        seen.add(name)
        yield entry
    if not seen: raise ArtifactError('Empty artifact manifest')


def verify(directory, manifest):
    root = Path(directory).resolve()
    entries = list(_entries(manifest))
    allowed_code = {entry['path'] for entry in entries if entry['path'].endswith('.py')}
    for candidate in root.rglob('*'):
        relative = candidate.relative_to(root).as_posix()
        if candidate.suffix == '.pyc' or (candidate.suffix == '.py' and relative not in allowed_code):
            raise ArtifactError('Unverified model code is not allowed')
    for entry in entries:
        path = root
        for part in PurePosixPath(entry['path']).parts:
            path = path / part
            if path.is_symlink(): raise ArtifactError('Artifact symlinks are not allowed')
        if not path.is_file() or path.stat().st_size != entry['size']:
            raise ArtifactError('Missing or invalid artifact size')
        with path.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        if digest != entry['sha256']: raise ArtifactError('Invalid artifact digest')


def _download(url, path):
    with urllib.request.urlopen(url, timeout=60) as source, path.open('wb') as target:
        shutil.copyfileobj(source, target, 1024 * 1024)
        target.flush()
        os.fsync(target.fileno())


def provision(destination, manifest, allowed_model, download=_download):
    entries = list(_entries(manifest))
    if manifest['model'] != allowed_model or not re.fullmatch('[0-9a-f]{40}', manifest['model_revision']):
        raise ArtifactError('Unsupported artifact origin')
    root = Path(destination)
    root.mkdir(parents=True, exist_ok=True)
    fingerprint = hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()
    revisions = root / 'revisions'
    revisions.mkdir(exist_ok=True)
    if revisions.is_symlink(): raise ArtifactError('Invalid revision directory')
    with (root/'.provision.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        current = root / 'current'
        # Keep immutable snapshots so a failed repair never damages the active
        # target and a running worker can continue using its resolved directory.
        if current.is_symlink():
            target = current.resolve()
            if target.parent == revisions.resolve() and (target.name == fingerprint or target.name.startswith(fingerprint + '-')):
                try:
                    verify(target, manifest)
                    return current
                except ArtifactError:
                    pass
        revision = revisions / fingerprint
        if revision.is_symlink():
            revision = revisions / (fingerprint + '-' + uuid.uuid4().hex)
        elif revision.exists():
            try:
                verify(revision, manifest)
            except ArtifactError:
                revision = revisions / (fingerprint + '-' + uuid.uuid4().hex)
        if not revision.exists():
            staging = Path(tempfile.mkdtemp(prefix='.staging-', dir=revisions))
            try:
                for entry in entries:
                    path = staging / entry['path']
                    path.parent.mkdir(parents=True, exist_ok=True)
                    url = f"https://huggingface.co/{manifest['model']}/resolve/{manifest['model_revision']}/{entry['path']}"
                    download(url, path)
                verify(staging, manifest)
                staging.rename(revision)
            finally:
                if staging.exists(): shutil.rmtree(staging)
        link = root / ('.current-' + uuid.uuid4().hex)
        try:
            link.symlink_to(Path('revisions') / revision.name, target_is_directory=True)
            os.replace(link, current)
        finally:
            with contextlib.suppress(FileNotFoundError): link.unlink()
        return current


def main():
    parser = argparse.ArgumentParser(description='Provision pinned decision-model artifacts.')
    parser.add_argument('--manifest')
    parser.add_argument('--destination', required=True)
    parser.add_argument('--offline-source')
    args = parser.parse_args()
    kind = os.environ.get('DECISION_BACKEND')
    manifest_path, allowed = selected_manifest(kind, args.manifest)
    manifest = json.loads(manifest_path.read_text())
    if args.manifest and json.loads(Path(args.manifest).read_text()) != manifest:
        raise ArtifactError('Manifest does not match selected model')
    if args.offline_source:
        source = Path(args.offline_source)
        verify(source, manifest)
        def copy(url, target):
            relative = url.split('/' + manifest['model_revision'] + '/', 1)[1]
            shutil.copyfile(source / relative, target)
        provision(args.destination, manifest, allowed, copy)
    else:
        provision(args.destination, manifest, allowed)
    print('Verified local model artifacts are ready.')


if __name__ == '__main__':
    main()
