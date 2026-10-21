#!/usr/bin/env python3
"""Build release archives into ignored .tools/releases; never publish them."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
TARGETS = [('linux', 'amd64'), ('linux', 'arm64'), ('darwin', 'amd64'), ('darwin', 'arm64')]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('version')
    args = parser.parse_args()
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+', args.version):
        parser.error('use a release version such as v0.1.0')
    destination = ROOT / '.tools' / 'releases' / args.version
    destination.mkdir(parents=True, exist_ok=False)
    sums = []
    for system, arch in TARGETS:
        name = f'receipt_{args.version}_{system}_{arch}.tar.gz'
        with tempfile.TemporaryDirectory() as temp:
            binary = Path(temp) / 'receipt'
            subprocess.run(['go', 'build', '-trimpath', '-o', str(binary), './cmd/receipt'],
                           cwd=ROOT, env={**os.environ, 'CGO_ENABLED': '0', 'GOOS': system, 'GOARCH': arch}, check=True)
            with tarfile.open(destination / name, 'w:gz') as archive:
                archive.add(binary, arcname='receipt')
                for document in ('LICENSE', 'NOTICE', 'THIRD-PARTY-NOTICES', 'README.md'):
                    archive.add(ROOT / document, arcname=document)
        digest = hashlib.sha256((destination / name).read_bytes()).hexdigest()
        sums.append(f'{digest}  {name}\n')
    (destination / 'SHA256SUMS').write_text(''.join(sums))
    print(destination)


if __name__ == '__main__':
    main()
