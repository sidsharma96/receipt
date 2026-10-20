#!/usr/bin/env python3
"""Curate the two retained Linux sessions and read-only droplet observations.
Usage: summarize_linux_baseline.py CAPTURE NEW_OUTPUT
No bearer tokens, manifests, output bodies or raw journal messages are copied.
"""
import collections
import hashlib
import json
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import sys

from linux_ci_baseline import distributions


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def request_summary(events):
    result = {}
    for operation in sorted({e['operation'] for e in events}):
        selected = [e for e in events if e['operation'] == operation]
        successful = [e for e in selected if 200 <= e['status'] < 300]
        result[operation] = {'count': len(selected), 'status_counts': dict(collections.Counter(str(e['status']) for e in selected))}
        if successful:
            result[operation]['successful_median_ms'] = statistics.median(e['duration_ns']/1e6 for e in successful)
            result[operation]['successful_max_ms'] = max(e['duration_ns']/1e6 for e in successful)
            stages = {s['stage'] for e in successful for s in e['stages']}
            result[operation]['stage_medians_ms'] = {s: statistics.median(sum(t['ns'] for t in e['stages'] if t['stage'] == s)/1e6 for e in successful) for s in sorted(stages)}
    return result


def inventory(revision):
    by_directory = collections.defaultdict(lambda: {'files': 0, 'bytes': 0})
    for line in subprocess.check_output(['git', 'ls-tree', '-rl', revision], text=True).splitlines():
        info, name = line.split('\t', 1)
        size = info.split()[3]
        if size == '-':
            continue
        group = by_directory[name.split('/')[0]]
        group['files'] += 1; group['bytes'] += int(size)
    return {'by_top_level': dict(by_directory), 'scope': 'Committed Git blob sizes only; excludes generated configuration, declared external module/binaries and the separately hashed Go executable.'}


def main():
    capture = Path(sys.argv[1]); out = Path(sys.argv[2]); out.mkdir(parents=True, exist_ok=False)
    result = {'scope': 'Two descriptive warm GitHub Ubuntu CI-client baselines, not an optimization comparison; deployed droplet observed without replacement.', 'clients': []}
    for i in (1, 2):
        source = capture / f'client-{i}' / 'results.json'
        data = json.loads(source.read_text())
        assert data['completed'] and data['key_before'] == data['key_after']
        assert data['summary'] == distributions(data['rows'])
        assert len(data['rows']) == 390 and all(row['hit'] and row['returncode'] == 0 for row in data['rows'])
        shutil.copyfile(source, out / f'client-{i}.json')
        trace = {'scope': data['strace']['scope'],
                 'summary': (capture / f'client-{i}' / 'strace-local.txt').read_text()}
        (out / f'strace-{i}.json').write_text(json.dumps(trace, indent=2) + '\n')
        result['clients'].append({'file': f'client-{i}.json', 'sha256': digest(source),
                                  'run_id': data['run_id'], 'source_revision': data['plan']['source_commit'],
                                  'inventory': inventory(data['plan']['source_commit']), 'summary': data['summary']})
        result['clients'][-1]['strace_file'] = f'strace-{i}.json'
        if 'transport' in data:
            transport = {}
            for case in ('status', 'fetch'):
                rows = [r for r in data['transport']['rows'] if r['case'] == case]
                metrics = {'dns_ms': [r['dns_s']*1000 for r in rows],
                           'tcp_ms': [(r['connected_s']-r['dns_s'])*1000 for r in rows],
                           'tls_ms': [(r['tls_ready_s']-r['connected_s'])*1000 for r in rows],
                           'request_to_first_byte_ms': [(r['first_byte_s']-r['tls_ready_s'])*1000 for r in rows],
                           'after_first_byte_ms': [(r['total_s']-r['first_byte_s'])*1000 for r in rows],
                           'total_ms': [r['total_s']*1000 for r in rows]}
                transport[case] = {'calls': len(rows), 'median_ms': {k: statistics.median(v) for k, v in metrics.items()},
                                   'download_bytes': sorted({r['download_bytes'] for r in rows})}
            result['clients'][-1]['transport'] = transport
    result['server'] = {'scope': 'All request events in observation windows, including unrelated normal CI; CPU and memory samples are coarse service-window counters, not per-request attribution.',
                        'windows': []}
    for i, suffix in ((1, ''), (2, '-2')):
        before_name = 'server-before.json' if i == 1 else 'server-before-2-services.json'
        before = json.loads((capture / before_name).read_text())
        after = json.loads((capture / f'server-after{suffix}.json').read_text())
        logs = json.loads((capture / f'server-logs{suffix}.json').read_text())
        assert before['pid'] == after['pid'] and before['start_ticks'] == after['start_ticks']
        assert before['binary_sha256'] == after['binary_sha256']
        window = {'before': before, 'after': after, 'logs': logs, 'request_summary': request_summary(logs['events']),
                  'receipt_cpu_seconds_delta': after['cpu_seconds']-before['cpu_seconds'],
                  'receipt_io_delta': {k: after['io'][k]-before['io'][k] for k in before['io']}}
        if 'services' in before:
            assert all(before['services'][s]['MainPID'] == after['services'][s]['MainPID'] for s in before['services'])
            window['service_cpu_seconds_delta'] = {s: (after['services'][s]['CPUUsageNSec']-before['services'][s]['CPUUsageNSec'])/1e9 for s in before['services']}
            window['resource_scope_note'] = 'Second service snapshot begins after helper startup; log window begins earlier. Observe explicit timestamps; not a full-job CPU bill.'
        result['server']['windows'].append(window)
    result['server']['plans'] = json.loads((capture / 'server-plans.json').read_text())
    build = (capture / 'server-build.txt').read_text()
    result['server']['build'] = {'go_version': re.search(r'receipt: (go\S+)', build).group(1),
                                 'embedded_revision': re.search(r'vcs.revision=(\w+)', build).group(1),
                                 'modified': 'vcs.modified=true' in build,
                                 'scope': 'Embedded base revision plus modified=true cannot identify the exact deployed source tree.'}
    with (out / 'summary.json').open('x') as stream:
        json.dump(result, stream, indent=2); stream.write('\n')
    print('Curated', out)


if __name__ == '__main__':
    main()
