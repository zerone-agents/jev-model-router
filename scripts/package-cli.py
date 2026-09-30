#!/usr/bin/env python3
"""Build release archives from an explicit source checkout; never publish."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path, default=Path('.'))
parser.add_argument('--output', type=Path, required=True)
parser.add_argument('--version', required=True, help='Release tag, e.g. v0.1.1')
args = parser.parse_args()
source = args.source.resolve()
output = args.output.resolve()
if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?', args.version):
    parser.error('version must be a v-prefixed semantic version')
version = re.search(r'const Version = "([^"]+)"', (source / 'internal/transport/cli/run.go').read_text())
if not version or version.group(1) != args.version[1:]:
    parser.error('release tag does not match the CLI source version')
if output.exists() and any(output.iterdir()):
    parser.error('output directory must be empty')
output.mkdir(parents=True, exist_ok=True)
platforms = [('darwin', 'amd64'), ('darwin', 'arm64'), ('linux', 'amd64'), ('linux', 'arm64'), ('windows', 'amd64')]
with tempfile.TemporaryDirectory(prefix='jev-cli-') as tmp:
    for goos, goarch in platforms:
        name = f'jev-router_{args.version[1:]}_{goos}_{goarch}'
        folder = Path(tmp) / name
        folder.mkdir()
        binary = folder / ('jev-router.exe' if goos == 'windows' else 'jev-router')
        subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w', '-o', str(binary), './cmd/jev-router'],
                       cwd=source, env={**os.environ, 'CGO_ENABLED': '0', 'GOOS': goos, 'GOARCH': goarch}, check=True)
        shutil.copyfile(source / 'LICENSE', folder / 'LICENSE')
        if goos == 'windows':
            with zipfile.ZipFile(output / f'{name}.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
                for item in sorted(folder.iterdir()):
                    archive.write(item, f'{name}/{item.name}')
        else:
            with tarfile.open(output / f'{name}.tar.gz', 'w:gz') as archive:
                archive.add(folder, arcname=name)
        print(f'Built {name}', flush=True)
checksums = [f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in sorted(output.iterdir())]
(output / 'SHA256SUMS').write_text(''.join(checksums))
