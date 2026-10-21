#!/usr/bin/env python3
"""Summarize symbol-only perf script output, counting each stack once per group."""
from collections import Counter
import json
from pathlib import Path
import sys


def summarize(text):
    stacks = []
    for block in text.strip().split('\n\n'):
        if not block.strip():
            continue
        lines = block.splitlines()
        if 'cpu-clock:u:' not in lines[0]:
            raise ValueError('expected cpu-clock user-space sample')
        symbols = [line.strip().split(' ', 1)[1] for line in lines[1:] if ' ' in line.strip()]
        stacks.append(symbols or ['[no call chain]'])
    if not stacks:
        raise ValueError('empty capture')
    leaves = Counter(stack[0] for stack in stacks)
    inclusive = Counter()
    for stack in stacks:
        inclusive.update(set(stack))
    groups = {
        'strict_object': lambda s: s in ('receipt/internal/strictjson.Object', 'github.com/sidsharma96/receipt/internal/strictjson.Object'),
        'postgres_get': lambda s: s in ('receipt/internal/cache.Postgres.Get', 'github.com/sidsharma96/receipt/internal/cache.Postgres.Get'),
        'encode': lambda s: s in ('receipt/internal/cache.Encode', 'github.com/sidsharma96/receipt/internal/cache.Encode'),
        'regexp': lambda s: s.startswith('regexp.'),
        'gc_worker_assist_or_start': lambda s: s in ('runtime.gcBgMarkWorker','runtime.gcAssistAlloc','runtime.gcStart'),
    }
    return {'samples': len(stacks), 'samples_without_frames': leaves['[no call chain]'],
            'scope': 'Unweighted sample counts, user CPU only. Inclusive groups overlap. GC group is recognized worker/assist/start stacks, not a pause measurement. Frame-pointer unwinding can truncate stacks.',
            'inclusive_groups': {name: sum(any(predicate(s) for s in stack) for stack in stacks) for name,predicate in groups.items()},
            'leaf_groups': {prefix: sum(n for name,n in leaves.items() if name.startswith(prefix)) for prefix in ('encoding/json.','regexp.','regexp/','crypto/sha256.','runtime.mallocgc')},
            'leaf_symbols': dict(leaves.most_common()),
            'inclusive_symbols': dict(inclusive.most_common())}


if __name__ == '__main__':
    print(json.dumps(summarize(Path(sys.argv[1]).read_text()),indent=2))
