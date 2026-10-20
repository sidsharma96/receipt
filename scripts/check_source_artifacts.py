#!/usr/bin/env python3
"""Keep raw JSON and oversized files out of source and staged commits."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

JSON_TYPES = {'.json', '.jsonl', '.ndjson'}
MAX_FILE_BYTES = 1024 * 1024


def source_paths(root):
    names = subprocess.check_output(
        ['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=root
    ).split(b'\0')
    return sorted({os.fsdecode(name) for name in names if name and (root / os.fsdecode(name)).exists()})


def forbidden_paths(root):
    return [name for name in source_paths(root) if Path(name).suffix.lower() in JSON_TYPES]


def staged_sizes(root):
    """Read the index (the next commit), even if a staged file was deleted on disk."""
    entries = subprocess.check_output(['git', 'ls-files', '--stage', '-z'], cwd=root).split(b'\0')
    blobs = []
    for entry in entries:
        if not entry:
            continue
        metadata, name = entry.split(b'\t', 1)
        mode, oid, stage = metadata.split()
        if mode != b'160000' and stage == b'0':
            blobs.append((os.fsdecode(name), oid))
    if not blobs:
        return {}
    sizes = subprocess.check_output(
        ['git', 'cat-file', '--batch-check=%(objectsize)'],
        input=b'\n'.join(oid for _, oid in blobs) + b'\n', cwd=root
    ).splitlines()
    return {name: int(size) for (name, _), size in zip(blobs, sizes)}


def violations(root, staged=False):
    sizes = staged_sizes(root) if staged else {name: (root / name).stat().st_size for name in source_paths(root)}
    return sorted(name for name, size in sizes.items()
                  if Path(name).suffix.lower() in JSON_TYPES or size > MAX_FILE_BYTES)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--staged', action='store_true', help='check the index bytes that Git will commit')
    args = parser.parse_args()
    paths = violations(Path(__file__).resolve().parents[1], staged=args.staged)
    if paths:
        print('Source rejects JSON/JSONL/NDJSON and files over 1 MiB. Use .tools/ or a CI artifact:', file=sys.stderr)
        print('\n'.join(paths), file=sys.stderr)
        return 1
    print('Source artifact policy passed: no JSON or files over 1 MiB.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
