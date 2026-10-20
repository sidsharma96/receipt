#!/usr/bin/env python3
"""Profile the existing self-CI setup on a disposable Ubuntu GitHub runner.
No credentials, environments, manifests or test output enter the artifact.
Usage: linux_ci_baseline.py NEW_OUTPUT_DIRECTORY
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import resource
import shutil
import statistics
import subprocess
import sys
import tempfile
import time

SOURCE = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(SOURCE / 'deploy/ci'))
import self_test as ci


def run(args, **kwargs):
    return subprocess.run(args, capture_output=True, text=True, check=True, timeout=180, **kwargs)


def percentile(values, fraction):
    ordered = sorted(values)
    index = (len(ordered)-1) * fraction
    low = int(index)
    return ordered[low] + (ordered[min(low+1, len(ordered)-1)]-ordered[low]) * (index-low)


def distributions(rows):
    result = {}
    for case in dict.fromkeys(r['case'] for r in rows):
        selected = [r for r in rows if r['case'] == case]
        metrics = {name: [r[name] for r in selected] for name in ('elapsed_ms', 'cpu_ms', 'rss_mb', 'inblock', 'outblock')}
        stages = sorted({s['stage'] for r in selected for s in r['timing']['stages']})
        for stage in stages:
            metrics[stage + '_ms'] = [sum(s['ns'] for s in r['timing']['stages'] if s['stage'] == stage) / 1e6 for r in selected]
        result[case] = {'calls': len(selected), 'metrics': {name: {'mean': statistics.fmean(v), 'median': statistics.median(v),
                            'p95': percentile(v, .95), 'p99': percentile(v, .99), 'max': max(v)} for name, v in metrics.items()}}
    return result


def measure(args, env, case, block):
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    started = time.time_ns(); clock = time.perf_counter_ns()
    p = subprocess.run(['/usr/bin/time', '-v'] + args, cwd=ci.ROOT, env=env,
                       capture_output=True, text=True, timeout=60)
    elapsed = (time.perf_counter_ns()-clock) / 1e6
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    events = [json.loads(line) for line in p.stderr.splitlines() if line.startswith('{"type":"receipt_timing"')]
    if len(events) != 1:
        raise RuntimeError('missing timing record; refusing to retain arbitrary stderr')
    timing = events[0]
    peak = re.search(r'Maximum resident set size \(kbytes\):\s*(\d+)', p.stderr)
    if peak is None:
        raise RuntimeError('missing GNU time resource report')
    return {'case': case, 'block': block, 'start_ns': started, 'end_ns': time.time_ns(),
            'elapsed_ms': elapsed, 'cpu_ms': 1000 * (after.ru_utime-before.ru_utime+after.ru_stime-before.ru_stime),
            'rss_mb': int(peak.group(1))*1024/1e6,
            'inblock': after.ru_inblock-before.ru_inblock, 'outblock': after.ru_oublock-before.ru_oublock,
            'minor_faults': after.ru_minflt-before.ru_minflt, 'major_faults': after.ru_majflt-before.ru_majflt,
            'voluntary_switches': after.ru_nvcsw-before.ru_nvcsw,
            'involuntary_switches': after.ru_nivcsw-before.ru_nivcsw,
            'returncode': p.returncode, 'hit': 'receipt: hit' in p.stderr, 'timing': timing,
            'stdout_sha256': hashlib.sha256(p.stdout.encode()).hexdigest(),
            **({'http': [json.loads(line) for line in p.stderr.splitlines() if line.startswith('{"type":"diagnostic_http"')]}
               if '{"type":"diagnostic_http"' in p.stderr else {})}


def explain(env):
    p = run([ci.CLIENT, 'explain', '--cache-dir', str(ci.HOME / 'receipt'), '--'] + ci.COMMAND, cwd=ci.ROOT, env=env)
    return re.search(r'key: ([a-f0-9]{64})', p.stdout).group(1)


def transport_samples(env, key):
    # Separate diagnostic: curl's DNS/TLS implementation differs from Go's.
    result = []
    fields = {'status': 'http_code', 'dns_s': 'time_namelookup', 'connected_s': 'time_connect',
              'tls_ready_s': 'time_appconnect', 'first_byte_s': 'time_starttransfer',
              'total_s': 'time_total', 'download_bytes': 'size_download'}
    template = '{' + ','.join(json.dumps(name) + ':%{' + variable + '}' for name, variable in fields.items()) + '}'
    header = 'header = ' + json.dumps('Authorization: Bearer ' + env['RECEIPT_TOKEN']) + '\n'
    for case, route, expected in [('status', '/v1/status/', 204), ('fetch', '/v1/entries/', 200)]:
        for i in range(10):
            p = run(['curl', '--silent', '--show-error', '--connect-timeout', '5', '--max-time', '15',
                     '--config', '-', '--output', '/dev/null', '--write-out', template, ci.REMOTE + route + key],
                    input=header, env=env)
            row = json.loads(p.stdout)
            if row['status'] != expected:
                raise RuntimeError('transport diagnostic returned unexpected status')
            result.append({'case': case, 'i': i, **row})
    return result


def main():
    if platform.system() != 'Linux' or os.environ.get('GITHUB_REF') != 'refs/heads/main':
        raise RuntimeError('run only on the trusted disposable Linux main-branch workflow')
    out = Path(sys.argv[1]).resolve(); out.mkdir(parents=True, exist_ok=False)
    plan = {'blocks': 3, 'calls_per_block': {'local': 100, 'checked-local': 20, 'remote-fetch': 10},
            'case_order': [['local', 'checked-local', 'remote-fetch'], ['checked-local', 'remote-fetch', 'local'], ['remote-fetch', 'local', 'checked-local']],
            'warmups_per_case_per_block': 3, 'stop_on_outlier': False, 'per_call_timeout_seconds': 60,
            'source_commit': run(['git', 'rev-parse', 'HEAD'], cwd=SOURCE).stdout.strip(),
            'environment': 'deploy/ci/self_test.py test_environment plus its GOPROXY=off and existing CI token',
            'scope': 'Warm same-runner client baseline, actual self-CI repository/dependency declarations; no capacity or optimization comparison.'}
    (out / 'plan.json').write_text(json.dumps(plan, indent=2))
    seed_start = time.time_ns()
    if ci.main() != 0:
        raise RuntimeError('existing CI helper did not succeed')
    seed_end = time.time_ns()
    env = ci.test_environment(str(Path(shutil.which('go')).resolve().parent), os.environ['ImageVersion'])
    env['GOPROXY'] = 'off'
    with ci.remote_cache() as ready:
        if not ready:
            raise RuntimeError('shared cache unavailable; refusing to call executed tests cache-hit measurements')
        env['RECEIPT_TOKEN'] = os.environ['RECEIPT_SELF_CI_TOKEN']
        key_before = explain(env)
        memory = dict(re.findall(r'^(MemTotal|MemAvailable):\s*(\d+) kB', Path('/proc/meminfo').read_text(), re.M))
        model = re.search(r'^model name\s*:\s*(.*)$', Path('/proc/cpuinfo').read_text(), re.M)
        report = {'plan': plan, 'completed': False, 'key_before': key_before,
                  'run_id': os.environ.get('GITHUB_RUN_ID'), 'run_attempt': os.environ.get('GITHUB_RUN_ATTEMPT'),
                  'runner_image': os.environ['ImageVersion'], 'os': platform.platform(), 'cpus': os.cpu_count(),
                  'cpu_model': model.group(1) if model else None, 'memory_kb': memory,
                  'go_version': run(['go', 'version'], cwd=ci.ROOT, env=env).stdout.strip(),
                  'git_version': run(['git', '--version'], cwd=ci.ROOT, env=env).stdout.strip(),
                  'client_sha256': hashlib.sha256(Path(ci.CLIENT).read_bytes()).hexdigest(),
                  'seed_start_ns': seed_start, 'seed_end_ns': seed_end, 'rows': []}
        (out / 'results.json').write_text(json.dumps(report, indent=2))
        try:
            for block, cases in enumerate(plan['case_order']):
                for case in cases:
                    count = plan['calls_per_block'][case]
                    for i in range(-3, count):
                        with tempfile.TemporaryDirectory(prefix='receipt-profile-', dir=ci.HOME) as empty:
                            cache = empty if case == 'remote-fetch' else str(ci.HOME / 'receipt')
                            args = [ci.CLIENT, 'run', '--timings', '--cache-dir', cache]
                            if case != 'local':
                                args += ['--remote', ci.REMOTE]
                            row = measure(args + ['--'] + ci.COMMAND, env, case, block)
                            if i >= 0:
                                row['i'] = len(report['rows']); report['rows'].append(row)
                            stages = [s['stage'] for s in row['timing']['stages']]
                            if row['returncode'] != 0 or not row['hit'] or 'execution' in stages or row['timing']['failed']:
                                raise RuntimeError('expected a successful hit; capture stopped without hiding fallback execution')
                    print('block', block+1, case, 'hits', count, flush=True)
            report['key_after'] = explain(env)
            if report['key_after'] != key_before:
                raise RuntimeError('fixture identity changed during measurement')
            report['summary'] = distributions(report['rows'])
            report['transport'] = {'scope': '20 separate fresh curl processes, verified HTTPS, same endpoint/key/token; not Go httptrace or measured Receipt calls.',
                                   'rows': transport_samples(env, key_before)}
            tracer = shutil.which('strace')
            if tracer:
                p = subprocess.run([tracer, '-f', '-c', '-o', str(out / 'strace-local.txt'), ci.CLIENT,
                                    'run', '--timings', '--cache-dir', str(ci.HOME / 'receipt'), '--'] + ci.COMMAND,
                                   cwd=ci.ROOT, env=env, capture_output=True, text=True, timeout=60)
                report['strace'] = {'returncode': p.returncode, 'hit': 'receipt: hit' in p.stderr,
                                    'scope': 'Separate perturbed local hit; summary syscall counts only.'}
            else:
                report['strace'] = {'available': False}
            report['completed'] = True
        finally:
            report['finished_ns'] = time.time_ns()
            (out / 'results.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report['summary'], indent=2))


if __name__ == '__main__':
    main()
