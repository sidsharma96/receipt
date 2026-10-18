#!/usr/bin/env python3
"""Compare prebuilt before/after binaries with fixed work and alternating order.
Usage: python3 scripts/profile/compare_hashing.py BEFORE_TEST AFTER_TEST BEFORE_CLI AFTER_CLI OUTPUT
macOS only (/usr/bin/time -l). Creates only temporary workload/cache directories.
"""
import json
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

before_test, after_test, before_cli, after_cli = [str(Path(p).resolve()) for p in sys.argv[1:5]]
destination = Path(sys.argv[5])
destination.parent.mkdir(parents=True, exist_ok=True)
report = {"scope": "fixed iterations; alternating order; warm OS caches; macOS time -l includes process startup/calibration/fixture setup", "benchmarks": [], "cli": []}

def measured(command, cwd=None):
    p = subprocess.run(['/usr/bin/time', '-l'] + command, cwd=cwd, capture_output=True, text=True, check=True)
    raw = p.stdout + p.stderr
    stats = {}
    match = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys', raw)
    if match:
        stats.update(zip(['wall_s', 'user_s', 'system_s'], map(float, match.groups())))
    for name in ['maximum resident set size', 'instructions retired', 'cycles elapsed', 'block input operations', 'block output operations']:
        match = re.search(r'(\d+)\s+' + name, raw)
        if match:
            stats[name] = int(match[1])
    return raw, stats

for size, iterations in [(4096, 2000), (8388608, 200)]:
    for pair in range(6):
        order = [('before', before_test), ('after', after_test)]
        if pair % 2:
            order.reverse()
        for label, binary in order:
            raw, stats = measured([binary, '-test.run', '^$', '-test.bench', f'^BenchmarkProfileFileInput/{size}$', '-test.benchtime', f'{iterations}x', '-test.benchmem'])
            match = re.search(r'\s(\d+)\s+([\d.]+) ns/op\s+[\d.]+ MB/s\s+(\d+) B/op\s+(\d+) allocs/op', raw)
            if not match:
                raise RuntimeError('missing benchmark: ' + raw)
            report['benchmarks'].append(dict(version=label, size=size, pair=pair, iterations=int(match[1]), ns_per_op=float(match[2]), bytes_per_op=int(match[3]), allocs_per_op=int(match[4]), resources=stats, raw=raw))

with tempfile.TemporaryDirectory(prefix='receipt-hash-compare-') as base:
    root = Path(base) / 'repo'
    root.mkdir()
    cache = Path(base) / 'cache'
    subprocess.run(['git', 'init', '-q', str(root)], check=True)
    (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) { if 2+2 != 4 { t.Fatal("bad") } }\n')
    (root/'inputs').mkdir()
    for n in range(256):
        (root/'inputs'/f'{n}.dat').write_bytes(bytes([n])*4096)
    (root/'inputs'/'large.dat').write_bytes(bytes(range(256)) * 32768)
    command = ['run', '--timings', '--cache-dir', str(cache), '--', 'go', 'test', '-count=1', './...']
    def invoke(label, binary, kind, pair):
        raw, resources = measured([binary] + command, cwd=root)
        timing = next(json.loads(line) for line in raw.splitlines() if line.startswith('{"type":"receipt_timing"'))
        executed = any(s['stage'] == 'execution' for s in timing['stages'])
        if (kind == 'miss') != executed:
            raise RuntimeError('unexpected execution state: ' + raw)
        report['cli'].append(dict(version=label, kind=kind, pair=pair, timing=timing, resources=resources, raw=raw))
        return raw
    invoke('before', before_cli, 'miss', -1)
    for pair in range(8):
        order = [('before', before_cli), ('after', after_cli)]
        if pair % 2:
            order.reverse()
        for label, binary in order:
            invoke(label, binary, 'hit', pair)
    # Compare the full key, not merely the short key in the hit diagnostic.
    keys = {}
    for label, binary in [('before', before_cli), ('after', after_cli)]:
        p = subprocess.run([binary, 'explain', '--cache-dir', str(cache), '--', 'go', 'test', '-count=1', './...'], cwd=root, capture_output=True, text=True, check=True)
        keys[label] = re.search(r'key: ([a-f0-9]{64})', p.stdout).group(1)
    if keys['before'] != keys['after']:
        raise RuntimeError('full keys differ')
    report['compatibility'] = dict(keys=keys, after_replayed_before_entry=True, fixture_files=259, fixture_payload_bytes=9*1024*1024, note='payload byte count excludes go.mod and test source; toolchain/executable/environment also fingerprinted')
report['environment'] = dict(go=subprocess.check_output(['go','version'],text=True).strip(), platform=subprocess.check_output(['uname','-sm'],text=True).strip())
destination.write_text(json.dumps(report, indent=2)+'\n')
for size in [4096,8388608]:
    for label in ['before','after']:
        samples=[x for x in report['benchmarks'] if x['size']==size and x['version']==label]
        print(size,label,'ns/op',statistics.median(x['ns_per_op'] for x in samples),'B/op',statistics.median(x['bytes_per_op'] for x in samples),'RSS',statistics.median(x['resources']['maximum resident set size'] for x in samples),'CPU s',statistics.median(x['resources']['user_s']+x['resources']['system_s'] for x in samples))
for label in ['before','after']:
    samples=[x for x in report['cli'] if x['kind']=='hit' and x['version']==label]
    print('CLI',label,'receipt ms',statistics.median(x['timing']['duration_ns'] for x in samples)/1e6,'RSS',statistics.median(x['resources']['maximum resident set size'] for x in samples))
