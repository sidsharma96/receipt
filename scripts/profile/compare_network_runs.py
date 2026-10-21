#!/usr/bin/env python3
"""Compare completed network jobs without treating runner assignment as a treatment.

Inputs are ignored local downloads of profile-network.yml artifacts. Output is
numeric JSON on stdout; redirect it under .tools/, never into versioned docs.
"""
import argparse
import json
from pathlib import Path
from summarize_network import stages, stats


def load_run(directory):
    directory = Path(directory)
    meta = json.loads((directory / 'metadata.json').read_text())
    rows = json.loads((directory / 'requests.json').read_text())
    if not meta.get('completed') or len(rows) != 120:
        raise ValueError('incomplete network run')
    if any(r['failed'] or r['trace']['status'] != (200 if r['endpoint'] == 'fetch' else 204) for r in rows):
        raise ValueError('failed requests must be investigated, not dropped')
    ids = [r['trace']['request_id'] for r in rows]
    if len(set(ids)) != 120 or not all(ids):
        raise ValueError('missing or duplicate request IDs')
    fetches = [r for r in rows if r['endpoint'] == 'fetch']
    if len(fetches) != 108:
        raise ValueError('unexpected fetch count')
    identities = {(r['trace']['body_sha256'], r['decoded_sha256'], r['trace']['body_bytes']) for r in fetches}
    if len(identities) != 1:
        raise ValueError('body or decoded result changed')
    fresh = [r for r in rows if r['mode'] == 'fresh' and r['endpoint'] == 'fetch'
             and not r.get('persistent_control') and not r.get('http1_control')]
    reused = [r for r in rows if r['mode'] == 'reuse' and r.get('persistent_control') and r['index'] > 0]
    if len(fresh) != 12 or {r['block'] for r in fresh} != set(range(12)):
        raise ValueError('fresh-process blocks missing')
    if len(reused) != 19 or {r['index'] for r in reused} != set(range(1, 20)):
        raise ValueError('warm reuse control incomplete')
    if any(r['trace']['reused'] for r in fresh) or not all(r['trace']['reused'] for r in reused):
        raise ValueError('connection lifetime differs from case label')
    def metrics(group):
        values = [stages(r) for r in group]
        names = ('operation_ms', 'tcp_ms', 'dns_ms', 'tls_ms', 'first_byte_ms', 'body_ms', 'decode_gap_ms', 'operation_cpu_ms')
        return {name: stats([v[name] for v in values if name in v]) for name in names}
    return {'metadata': meta, 'identity': next(iter(identities)), 'fresh': metrics(fresh),
            'reused': metrics(reused), 'requests': len(rows), 'fetches': len(fetches)}


def compare(directories):
    runs = [load_run(d) for d in directories]
    if len(runs) < 2 or len({r['metadata']['run_id'] for r in runs}) != len(runs):
        raise ValueError('at least two distinct runs required')
    for field in ('source_commit', 'go_version', 'fixture_key', 'cgo_enabled', 'runner_image'):
        if len({r['metadata'][field] for r in runs}) != 1:
            raise ValueError('comparison drift: ' + field)
    if len({r['metadata']['probe_sha256']['network'] for r in runs}) != 1:
        raise ValueError('network probe binary drift')
    if len({r['identity'] for r in runs}) != 1:
        raise ValueError('cross-run result drift')
    return {'scope': 'Observational runner assignments, not controlled geography. Fresh fetch excludes repository fingerprinting and CLI startup. Reuse is a later warm batch, not a shipped speedup.',
            'runs': runs, 'requests': sum(r['requests'] for r in runs),
            'fetches': sum(r['fetches'] for r in runs)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directories', nargs='+')
    args = parser.parse_args()
    print(json.dumps(compare(args.directories), indent=2))
