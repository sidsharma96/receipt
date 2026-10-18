#!/usr/bin/env python3
"""Sequential, alternating executable/probe comparisons on macOS.
Arguments: OLD_CLI BEFORE_CLI AFTER_CLI BEFORE_TEST AFTER_TEST PYTHON OUTPUT.
OLD_CLI populates entries; BEFORE_CLI is the old algorithm with stage timings.
PYTHON must already have pytest installed. No packages are installed or changed.
"""
import json
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

old, before, after, before_test, after_test, python = [str(Path(p).resolve()) if i != 5 else str(Path(p).absolute()) for i,p in enumerate(sys.argv[1:7])]
output = Path(sys.argv[7])

def measured(command, cwd=None):
    p = subprocess.run(['/usr/bin/time','-l'] + command, cwd=cwd, capture_output=True, text=True, check=True)
    raw = p.stdout + p.stderr
    resources = {}
    match = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys', raw)
    if match:
        resources.update(zip(['wall_s','user_s','system_s'], map(float,match.groups())))
    for name in ['maximum resident set size','instructions retired','cycles elapsed']:
        match = re.search(r'(\d+)\s+' + name, raw)
        if match:
            resources[name] = int(match[1])
    return raw, resources

report = {'environment':subprocess.check_output(['go','version'],text=True).strip(), 'benchmarks':[], 'hits':[], 'compatibility':{}}
for pair in range(6):
    order=[('before',before_test),('after',after_test)]
    if pair%2:
        order.reverse()
    for label,binary in order:
        raw,resources=measured([binary,'-test.run','^$','-test.bench','^BenchmarkExecutableInput$','-test.benchtime','100x','-test.benchmem'])
        match=re.search(r'\s(\d+)\s+([\d.]+) ns/op\s+[\d.]+ MB/s\s+(\d+) B/op',raw)
        if not match:
            raise RuntimeError(raw)
        report['benchmarks'].append(dict(version=label,pair=pair,iterations=int(match[1]),ns_per_op=float(match[2]),bytes_per_op=int(match[3]),resources=resources,raw=raw))

with tempfile.TemporaryDirectory(prefix='receipt-executable-') as temp:
    base=Path(temp)
    root=base/'repo'; root.mkdir()
    subprocess.run(['git','init','-q',str(root)],check=True)
    (root/'.gitignore').write_text('__pycache__/\n.pytest_cache/\n')
    (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) { if 2+2!=4 {t.Fatal("bad")} }\n')
    (root/'test_fixture.py').write_text('def test_value():\n    assert 2+2 == 4\n')
    (root/'inputs').mkdir()
    for i in range(256):
        (root/'inputs'/f'{i}.dat').write_bytes(bytes([i])*4096)
    launcher=base/'pytest'
    launcher.write_text('#!'+python+'\nimport pytest\nraise SystemExit(pytest.main())\n')
    launcher.chmod(0o700)
    commands={'go':['go','test','-count=1','./...'], 'python':[python,'-m','pytest','-q','-p','no:cacheprovider'], 'pytest':[str(launcher),'-q','-p','no:cacheprovider']}
    cache=base/'cache'
    for case,command in commands.items():
        args=['run','--timings','--cache-dir',str(cache),'--']+command
        subprocess.run([old]+args,cwd=root,capture_output=True,text=True,check=True)
        for pair in range(6):
            order=[('before',before),('after',after)]
            if pair%2:
                order.reverse()
            for label,binary in order:
                raw,resources=measured([binary]+args,cwd=root)
                timing=next(json.loads(line) for line in raw.splitlines() if line.startswith('{"type":"receipt_timing"'))
                if any(s['stage']=='execution' for s in timing['stages']):
                    raise RuntimeError('expected old-entry replay: '+raw)
                report['hits'].append(dict(case=case,version=label,pair=pair,timing=timing,resources=resources,raw=raw))
        keys={}
        for label,binary in [('old',old),('before',before),('after',after)]:
            p=subprocess.run([binary,'explain','--cache-dir',str(cache),'--']+command,cwd=root,capture_output=True,text=True,check=True)
            keys[label]=re.search(r'key: ([a-f0-9]{64})',p.stdout).group(1)
        if len(set(keys.values()))!=1:
            raise RuntimeError('incompatible keys: '+str(keys))
        report['compatibility'][case]=keys
output.parent.mkdir(parents=True,exist_ok=True)
output.write_text(json.dumps(report,indent=2)+'\n')
for case in commands:
    for version in ['before','after']:
        samples=[s for s in report['hits'] if s['case']==case and s['version']==version]
        summary={'receipt_ms':statistics.median(s['timing']['duration_ns'] for s in samples)/1e6,'rss':statistics.median(s['resources']['maximum resident set size'] for s in samples)}
        for stage in ['fingerprint','discovery','input_files','executable','toolchain']:
            summary[stage+'_ms']=statistics.median(next(t['ns'] for t in s['timing']['stages'] if t['stage']==stage) for s in samples)/1e6
        print(case,version,summary)
