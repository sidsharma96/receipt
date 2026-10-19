#!/usr/bin/env python3
"""macOS sequential comparison: BEFORE AFTER BEFORE_TEST AFTER_TEST PYTHON OUTPUT.
Existing Python must have pytest. No packages are installed or modified.
"""
import json
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile
import time

before, after, before_test, after_test = [str(Path(p).resolve()) for p in sys.argv[1:5]]
python = str(Path(sys.argv[5]).absolute())  # Preserve venv symlink spelling.
output = Path(sys.argv[6])

def measured(command, cwd=None):
    start = time.perf_counter_ns()
    p = subprocess.run(['/usr/bin/time','-l']+command,cwd=cwd,capture_output=True,text=True,check=True)
    elapsed = time.perf_counter_ns()-start
    raw = p.stdout+p.stderr
    match = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys',raw)
    resources = dict(zip(['wall_s','user_s','system_s'],map(float,match.groups())))
    for name in ['maximum resident set size','instructions retired','cycles elapsed']:
        match = re.search(r'(\d+)\s+'+name,raw)
        if match:
            resources[name] = int(match[1])
    return raw,resources,elapsed

report = {'go':subprocess.check_output(['go','version'],text=True).strip(),
          'python':subprocess.check_output([python,'--version'],text=True).strip(),
          'platform':subprocess.check_output(['uname','-sm'],text=True).strip(),
          'normalization':[], 'breakdown':[], 'hits':[], 'compatibility':{}}
for pair in range(6):
    order = [('before',before_test),('after',after_test)]
    if pair%2:
        order.reverse()
    for label,binary in order:
        raw,resources,_ = measured([binary,'-test.run','^$','-test.bench','^BenchmarkProbeBreakdown/GoNormalize$','-test.benchtime','10000x','-test.benchmem'])
        match = re.search(r'\s(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op',raw)
        if not match:
            raise RuntimeError(raw)
        report['normalization'].append(dict(pair=pair,version=label,iterations=int(match[1]),ns_per_op=float(match[2]),bytes_per_op=int(match[3]),allocs_per_op=int(match[4]),resources=resources))

with tempfile.TemporaryDirectory(prefix='receipt-probes-') as temp:
    base = Path(temp); root = base/'repo'; root.mkdir()
    subprocess.run(['git','init','-q',str(root)],check=True)
    (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
    (root/'test_fixture.py').write_text('def test_value():\n    assert 2+2 == 4\n')
    (root/'.gitignore').write_text('__pycache__/\n.pytest_cache/\n')
    for i in range(256):
        (root/f'input-{i}').write_bytes(bytes([i])*4096)
    source = (Path(__file__).resolve().parents[2]/'internal/fingerprint/manifest.go').read_text()
    python_probe = re.search(r'const pythonProbe = `([^`]+)`',source).group(1)
    probes = [('go-version',['go','version']),('go-env',['go','env','-json']),
              ('python-start',[python,'-c','pass']),('python-imports',[python,'-c','import sys,json,importlib.metadata']),
              ('python-full',[python,'-c',python_probe])]
    for pair in range(10):
        for name,command in (probes if pair%2==0 else list(reversed(probes))):
            _,resources,elapsed = measured(command,root)
            report['breakdown'].append(dict(pair=pair,probe=name,elapsed_ns=elapsed,resources=resources))
    launcher = base/'pytest'
    launcher.write_text('#!'+python+'\nimport pytest\nraise SystemExit(pytest.main())\n'); launcher.chmod(0o700)
    commands = {'go':['go','test','-count=1','./...'], 'python':[python,'-m','pytest','-q','-p','no:cacheprovider'], 'pytest':[str(launcher),'-q','-p','no:cacheprovider']}
    cache = base/'cache'
    for case,command in commands.items():
        args = ['run','--timings','--cache-dir',str(cache),'--']+command
        subprocess.run([before]+args,cwd=root,capture_output=True,text=True,check=True)
        for pair in range(10):
            order = [('before',before),('after',after)]
            if pair%2:
                order.reverse()
            for label,binary in order:
                raw,resources,_ = measured([binary]+args,root)
                timing = next(json.loads(line) for line in raw.splitlines() if line.startswith('{"type":"receipt_timing"'))
                if any(s['stage']=='execution' for s in timing['stages']) or 'receipt: hit' not in raw:
                    raise RuntimeError('expected old-entry replay: '+raw)
                report['hits'].append(dict(case=case,pair=pair,version=label,timing=timing,resources=resources))
        keys = {}
        for label,binary in [('before',before),('after',after)]:
            p = subprocess.run([binary,'explain','--cache-dir',str(cache),'--']+command,cwd=root,capture_output=True,text=True,check=True)
            keys[label] = re.search(r'key: ([a-f0-9]{64})',p.stdout).group(1)
        if len(set(keys.values()))!=1:
            raise RuntimeError('key mismatch: '+str(keys))
        report['compatibility'][case] = keys
output.parent.mkdir(parents=True,exist_ok=True)
output.write_text(json.dumps(report,indent=2)+'\n')
for version in ['before','after']:
    samples = [s for s in report['normalization'] if s['version']==version]
    print('normalize',version,{k:statistics.median(s[k] for s in samples) for k in ['ns_per_op','bytes_per_op','allocs_per_op']},
          {k:statistics.median(s['resources'][k] for s in samples) for k in ['user_s','system_s','maximum resident set size']})
for name,_ in probes:
    samples = [s for s in report['breakdown'] if s['probe']==name]
    print('probe',name,statistics.median(s['elapsed_ns'] for s in samples)/1e6)
for case in commands:
    for version in ['before','after']:
        samples = [s for s in report['hits'] if s['case']==case and s['version']==version]
        print(case,version,{'receipt_ms':statistics.median(s['timing']['duration_ns'] for s in samples)/1e6,
             'range_ms':[min(s['timing']['duration_ns'] for s in samples)/1e6,max(s['timing']['duration_ns'] for s in samples)/1e6],
             'toolchain_ms':statistics.median(next(t['ns'] for t in s['timing']['stages'] if t['stage']=='toolchain') for s in samples)/1e6,
             'cpu_s':statistics.median(s['resources']['user_s']+s['resources']['system_s'] for s in samples),
             'rss_bytes':statistics.median(s['resources']['maximum resident set size'] for s in samples)})
