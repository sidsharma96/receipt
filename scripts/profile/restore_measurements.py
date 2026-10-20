#!/usr/bin/env python3
"""Restore optional historical measurement JSON to ignored local storage, offline.

No server is contacted and no benchmark runs. The historical Git object must be
available locally (a shallow checkout may need an explicit fetch first).
"""
from pathlib import Path
import subprocess

REVISION = 'f87c9b5eb6ee5ba74ac1837181189732ceaebf7b'


def restore(root):
    paths = subprocess.check_output(
        ['git', 'ls-tree', '-r', '--name-only', REVISION, '--', 'docs/measurements'],
        cwd=root, text=True
    ).splitlines()
    selected = [Path(p) for p in paths if p.endswith('.json')]
    if not selected:
        raise RuntimeError('historical measurement archive is empty')
    for source in selected:
        relative = source.relative_to('docs/measurements')
        if relative.is_absolute() or '..' in relative.parts:
            raise RuntimeError('unsafe historical path: ' + str(source))
        data = subprocess.check_output(['git', 'show', REVISION + ':' + source.as_posix()], cwd=root)
        destination = root / '.tools/measurements' / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        if destination.exists():
            if destination.read_bytes() != data:
                raise RuntimeError('refusing to overwrite different measurements: ' + str(destination))
            continue
        with destination.open('xb') as output:
            output.write(data)
    return len(selected)


if __name__ == '__main__':
    count = restore(Path(__file__).resolve().parents[2])
    print(f'{count} historical JSON records available in .tools/measurements/ (ignored).')
