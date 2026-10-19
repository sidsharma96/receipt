#!/usr/bin/env python3
"""Fixed-budget macOS Receipt-hit hunt; stock Go, no runtime/Trace2/OS trace.
Usage: capture_receipt_go_batch.py SOURCE NEW_OUTPUT [--blocks 5] [--calls 1000]
Raw records stay local. System sampler records names, never process arguments.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import threading
import time

from capture_git_outlier import build, run


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sample_system(stop, destination):
    previous = {}
    with destination.open('w') as stream:
        while not stop.is_set():
            started = time.time_ns()
            try:
                processes = run(['/bin/ps', '-A', '-o', 'pid=,time=,rss=,state=,comm='], timeout=5).stdout
                current = {}
                changes = []
                for line in processes.splitlines():
                    fields = line.split(None, 4)
                    if len(fields) != 5:
                        continue
                    pid, cpu, rss, state, name = fields
                    parts = cpu.split(':')
                    seconds = sum(float(v) * 60**i for i, v in enumerate(reversed(parts)))
                    current[pid] = seconds
                    if pid in previous:
                        changes.append({'pid': int(pid), 'name': Path(name).name,
                                        'cpu_seconds_delta': max(0, seconds - previous[pid]),
                                        'rss_kb': int(rss), 'state': state})
                previous = current
                vm = run(['/usr/bin/vm_stat'], timeout=5).stdout
                memory = {key.strip(): int(value.strip().rstrip('.'))
                          for key, value in re.findall(r'^([^:\n]+):\s*(\d+\.)$', vm, re.M)}
                record = {'start_ns': started, 'end_ns': time.time_ns(),
                          'load': os.getloadavg(), 'memory_pages': memory,
                          'top_cpu_deltas': sorted(changes, key=lambda p: p['cpu_seconds_delta'], reverse=True)[:10]}
            except Exception as error:
                record = {'start_ns': started, 'end_ns': time.time_ns(), 'error': str(error)}
            stream.write(json.dumps(record) + '\n'); stream.flush()
            stop.wait(max(0, 1 - (time.time_ns() - started) / 1e9))


def records(stderr):
    return [json.loads(line) for line in stderr.splitlines() if line.startswith('{"')]


def key(binary, arguments, root, env):
    result = run([str(binary), 'explain'] + [a for a in arguments[1:] if a != '--timings'], cwd=root, env=env, timeout=30)
    return re.search(r'key: ([a-f0-9]{64})', result.stdout).group(1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--blocks', type=int, default=5)
    parser.add_argument('--calls', type=int, default=1000)
    options = parser.parse_args()
    if options.blocks < 1 or options.calls < 1:
        parser.error('blocks and calls must be positive')
    source = options.source.resolve(); out = options.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    plan = {'blocks': options.blocks, 'calls_per_block': options.calls, 'warmups_per_block': 5,
            'go_thresholds_ms': [100, 200, 300], 'stop_on_outlier': False,
            'per_call_timeout_seconds': 30, 'wall_budget_seconds': 1800,
            'pause_between_blocks_seconds': 2, 'system_sample_interval_seconds': 1,
            'go': 'stock PATH-selected Go with normal automatic toolchain selection',
            'git_trace2': False, 'runtime_trace': False, 'os_trace': False,
            'source_commit': run(['git', 'rev-parse', 'HEAD'], cwd=source).stdout.strip()}
    (out / 'plan.json').write_text(json.dumps(plan, indent=2))
    binary = build(source, out, out / 'unused-git-events', trace_git=False)
    stock = out / 'receipt-stock'
    run(['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags=-linkmode=external', '-o', str(stock), './cmd/receipt'], cwd=source)
    root = out / 'fixture'; root.mkdir()
    run(['git', 'init', '-q', str(root)])
    (root / 'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root / 'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
    (root / '.gitignore').write_text('ignored\n')
    (root / 'ignored').write_text('excluded')
    for i in range(256):
        (root / f'input-{i}').write_bytes(bytes([i]) * 4096)
    env = dict(os.environ)
    if any(name in env for name in ('GIT_TRACE2_EVENT', 'RECEIPT_GO_TRACE', 'RECEIPT_GO_EVENTS')):
        raise RuntimeError('unexpected inherited diagnostic environment')
    arguments = ['run', '--timings', '--cache-dir', str(out / 'cache'), '--', 'go', 'test', '-count=1', './...']
    run([str(stock)] + arguments, cwd=root, env=env, timeout=60)
    expected = key(stock, arguments, root, env)
    assert key(binary, arguments, root, env) == expected
    goroot = Path(run(['go', 'env', 'GOROOT'], cwd=root, env=env).stdout.strip())
    report = {'plan': plan, 'stock_key_before': expected, 'diagnostic_key_before': expected,
              'stock_binary_sha256': sha(stock), 'diagnostic_binary_sha256': sha(binary),
              'go_binary_sha256': sha(goroot / 'bin/go'),
              'go_version': run(['go', 'version'], cwd=root, env=env).stdout.strip(),
              'host': run(['/usr/bin/sw_vers']).stdout, 'completed': False,
              'rows_file': 'calls.jsonl', 'system_file': 'system.jsonl'}
    (out / 'results.json').write_text(json.dumps(report, indent=2))
    stop = threading.Event()
    sampler = threading.Thread(target=sample_system, args=(stop, out / 'system.jsonl'))
    sampler.start()
    deadline = time.monotonic() + plan['wall_budget_seconds']
    report['started_ns'] = time.time_ns()
    try:
        with (out / 'calls.jsonl').open('w') as stream:
            for block in range(options.blocks):
                for _ in range(5):
                    p = run([str(binary)] + arguments, cwd=root, env=env, timeout=30)
                    assert 'receipt: hit' in p.stderr
                maximum = 0; slow = 0
                for j in range(options.calls):
                    if time.monotonic() > deadline:
                        raise TimeoutError('fixed wall budget exceeded')
                    started = time.time_ns(); monotonic = time.perf_counter_ns()
                    p = subprocess.run(['/usr/bin/time', '-l', str(binary)] + arguments,
                                       cwd=root, env=env, capture_output=True, text=True, timeout=30)
                    elapsed = (time.perf_counter_ns() - monotonic) / 1e6
                    events = records(p.stderr)
                    row = {'i': block * options.calls + j, 'block': block, 'start_ns': started,
                           'end_ns': time.time_ns(), 'elapsed_ms': elapsed, 'events': events,
                           'returncode': p.returncode}
                    peak = re.search(r'(\d+)\s+maximum resident set size', p.stderr)
                    cpu = re.search(r'([\d.]+) real\s+([\d.]+) user\s+([\d.]+) sys', p.stderr)
                    row['rss_bytes'] = int(peak.group(1)) if peak else None
                    row['time_cpu_ms'] = (float(cpu.group(2)) + float(cpu.group(3))) * 1000 if cpu else None
                    stream.write(json.dumps(row) + '\n'); stream.flush()
                    assert p.returncode == 0 and 'receipt: hit' in p.stderr
                    timing = next(e for e in events if e['type'] == 'receipt_timing')
                    assert not any(s['stage'] == 'execution' for s in timing['stages'])
                    probes = [e for e in events if e.get('program') == 'go' and e['operation'] == 'env']
                    assert len(probes) == 1 and not probes[0]['failed'] and probes[0]['usage'] is not None
                    maximum = max(maximum, probes[0]['wall_ms'])
                    slow += probes[0]['wall_ms'] >= 200
                    if (j + 1) % 100 == 0:
                        print(f'block {block+1} calls {j+1} max Go {maximum:.3f}ms Go>=200ms {slow}', flush=True)
                if block + 1 < options.blocks:
                    time.sleep(2)
        report['stock_key_after'] = key(stock, arguments, root, env)
        report['diagnostic_key_after'] = key(binary, arguments, root, env)
        assert report['stock_key_after'] == report['diagnostic_key_after'] == expected
        assert sha(goroot / 'bin/go') == report['go_binary_sha256']
        report['completed'] = True
    finally:
        stop.set(); sampler.join(timeout=10)
        report['finished_ns'] = time.time_ns()
        for name in ('calls.jsonl', 'system.jsonl'):
            report[name + '_sha256'] = sha(out / name)
        (out / 'results.json').write_text(json.dumps(report, indent=2))
    print('Saved', out / 'results.json', flush=True)


if __name__ == '__main__':
    main()
