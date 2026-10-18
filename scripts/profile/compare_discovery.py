#!/usr/bin/env python3
"""macOS warm-cache comparison: BEFORE_CLI AFTER_CLI OUTPUT.
Run sequentially without other tests/benchmarks. Creates only temporary fixtures.
"""
import json
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

before, after = [str(Path(p).resolve()) for p in sys.argv[1:3]]
output = Path(sys.argv[3])

def measured(command, cwd):
    p = subprocess.run(['/usr/bin/time', '-l'] + command, cwd=cwd, capture_output=True, text=True, check=True)
    raw = p.stdout + p.stderr
    match = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys', raw)
    resources = dict(zip(['wall_s', 'user_s', 'system_s'], map(float, match.groups())))
    for name in ['maximum resident set size', 'instructions retired', 'cycles elapsed', 'block input operations', 'block output operations']:
        match = re.search(r'(\d+)\s+' + name, raw)
        if match:
            resources[name] = int(match[1])
    return raw, resources

report = {'go': subprocess.check_output(['go','version'],text=True).strip(),
          'git': subprocess.check_output(['git','--version'],text=True).strip(),
          'platform': subprocess.check_output(['uname','-sm'],text=True).strip(),
          'hits': [], 'compatibility': {}, 'git_commands': []}
with tempfile.TemporaryDirectory(prefix='receipt-discovery-') as temp:
    base = Path(temp)
    root = base/'repo'; root.mkdir()
    subprocess.run(['git','init','-q',str(root)],check=True)
    (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
    (root/'nested').mkdir()
    (root/'.gitignore').write_text('ignored\n')
    (root/'ignored').write_text('excluded')
    for i in range(256):
        (root/f'input-{i}').write_bytes(bytes([i])*4096)
    # Measure actual Git subprocesses separately; these are not additive CPU profiles.
    for pair in range(10):
        for name, command in [('root',['git','rev-parse','--show-toplevel']), ('listing',['git','ls-files','-z','--cached','--others','--exclude-standard'])]:
            _, resources = measured(command, root)
            report['git_commands'].append(dict(pair=pair, command=name, resources=resources))
    cache = base/'cache'
    for case, cwd, command in [('generic-root',root,['/bin/sh','-c','true']), ('generic-nested',root/'nested',['/bin/sh','-c','true']), ('go',root,['go','test','-count=1','./...'])]:
        flags = ['--allow-generic'] if case.startswith('generic') else []
        args = ['run','--timings','--cache-dir',str(cache)] + flags + ['--'] + command
        subprocess.run([before]+args,cwd=cwd,capture_output=True,text=True,check=True)
        for pair in range(10):
            order = [('before',before),('after',after)]
            if pair%2:
                order.reverse()
            for label,binary in order:
                raw,resources = measured([binary]+args,cwd)
                timing = next(json.loads(line) for line in raw.splitlines() if line.startswith('{"type":"receipt_timing"'))
                if any(s['stage']=='execution' for s in timing['stages']) or 'receipt: hit' not in raw:
                    raise RuntimeError('expected old-entry replay: '+raw)
                report['hits'].append(dict(case=case,version=label,pair=pair,timing=timing,resources=resources))
        keys = {}
        for label,binary in [('before',before),('after',after)]:
            p = subprocess.run([binary,'explain','--cache-dir',str(cache)]+flags+['--']+command,cwd=cwd,capture_output=True,text=True,check=True)
            keys[label] = re.search(r'key: ([a-f0-9]{64})',p.stdout).group(1)
        if len(set(keys.values())) != 1:
            raise RuntimeError('incompatible keys: '+str(keys))
        report['compatibility'][case] = keys
output.parent.mkdir(parents=True,exist_ok=True)
output.write_text(json.dumps(report,indent=2)+'\n')
for case in report['compatibility']:
    for version in ['before','after']:
        samples = [s for s in report['hits'] if s['case']==case and s['version']==version]
        summary = {'receipt_ms':statistics.median(s['timing']['duration_ns'] for s in samples)/1e6,
                   'cpu_s':statistics.median(s['resources']['user_s']+s['resources']['system_s'] for s in samples),
                   'rss_bytes':statistics.median(s['resources']['maximum resident set size'] for s in samples),
                   'cycles':statistics.median(s['resources']['cycles elapsed'] for s in samples)}
        summary['range_ms'] = [min(s['timing']['duration_ns'] for s in samples)/1e6,max(s['timing']['duration_ns'] for s in samples)/1e6]
        for stage in ['fingerprint','discovery']:
            summary[stage+'_ms'] = statistics.median(next(t['ns'] for t in s['timing']['stages'] if t['stage']==stage) for s in samples)/1e6
        print(case,version,summary)
