#!/usr/bin/env python3
"""Summarize a completed fixed-budget batch; publish compact counters, not raw traces.
Usage: analyze_receipt_go_batch.py CAPTURE NEW_EVIDENCE_JSON
"""
import argparse
import hashlib
import json
from pathlib import Path
import statistics


def percentile(values, fraction):
    ordered = sorted(values)
    index = (len(ordered) - 1) * fraction
    low = int(index)
    return ordered[low] + (ordered[min(low + 1, len(ordered) - 1)] - ordered[low]) * (index - low)


def distribution(values):
    return {'mean': statistics.fmean(values), 'median': statistics.median(values),
            'p95': percentile(values, .95), 'p99': percentile(values, .99), 'max': max(values)}


def summarize(rows):
    return {'calls': len(rows), 'go_wall_ms': distribution([r['go']['wall_ms'] for r in rows]),
            'go_cpu_ms': distribution([r['go']['cpu_ms'] for r in rows]),
            'go_start_ms': distribution([r['go']['start_ms'] for r in rows]),
            'go_switches': distribution([r['go']['usage']['Nivcsw'] for r in rows]),
            'elapsed_ms': distribution([r['elapsed_ms'] for r in rows]),
            'rss_mb': distribution([r['rss_bytes'] / 1e6 for r in rows]),
            'go_rss_mb': distribution([r['go']['usage']['Maxrss'] / 1e6 for r in rows]),
            'time_cpu_ms': distribution([r['time_cpu_ms'] for r in rows]),
            'go_thresholds': {str(t): sum(r['go']['wall_ms'] >= t for r in rows) for t in (100, 150, 200, 300)},
            'whole_hit_thresholds': {str(t): sum(r['elapsed_ms'] >= t for r in rows) for t in (200, 300)},
            'receipt_gc_collections': sum(r['receipt_gc']['collections'] for r in rows),
            'receipt_gc_max_pause_ms': max(r['receipt_gc']['pause_ns'] / 1e6 for r in rows)}


def analyze(capture):
    metadata = json.loads((capture / 'results.json').read_text())
    assert metadata['completed']
    for name in ('calls.jsonl', 'system.jsonl'):
        assert hashlib.sha256((capture / name).read_bytes()).hexdigest() == metadata[name + '_sha256']
    raw = [json.loads(line) for line in (capture / 'calls.jsonl').read_text().splitlines()]
    rows = []
    for record in raw:
        assert record['returncode'] == 0
        events = record['events']
        children = [e for e in events if e['type'] == 'diagnostic_child']
        assert all(not e['failed'] and e['usage'] is not None for e in children)
        go = [e for e in children if e['program'] == 'go' and e['operation'] == 'env']
        assert len(go) == 1
        go = dict(go[0])
        go.pop('type')
        # No private paths, environment values or output bytes are retained.
        go['usage'] = {k: go['usage'][k] for k in ('Maxrss', 'Minflt', 'Majflt', 'Nswap', 'Inblock', 'Oublock', 'Nvcsw', 'Nivcsw')}
        timing = next(e for e in events if e['type'] == 'receipt_timing')
        stages = {s['stage']: s['ns'] / 1e6 for s in timing['stages']}
        assert 'execution' not in stages
        rows.append({**{k: record[k] for k in ('i', 'block', 'start_ns', 'end_ns', 'elapsed_ms', 'rss_bytes', 'time_cpu_ms')},
                     'receipt_duration_ms': timing['duration_ns'] / 1e6,
                     'go': go, 'git_wall_ms': [e['wall_ms'] for e in children if e['program'] == 'git'],
                     'stages_ms': stages,
                     'receipt_gc': {k: next(e for e in events if e['type'] == 'diagnostic_gc')[k]
                                    for k in ('collections', 'pause_ns')}})
    assert len(rows) == metadata['plan']['blocks'] * metadata['plan']['calls_per_block']
    system = [json.loads(line) for line in (capture / 'system.jsonl').read_text().splitlines()]
    # Retain counts and system-wide counters; omit the process list from publication.
    compact_system = [{k: sample[k] for k in ('start_ns', 'end_ns', 'load', 'memory_pages')}
                      if 'error' not in sample else {k: sample[k] for k in ('start_ns', 'end_ns', 'error')}
                      for sample in system]
    good = [s for s in compact_system if 'error' not in s]
    if not good:
        raise ValueError('no successful system samples; inspect the local capture')
    summary = summarize(rows)
    summary['blocks'] = [summarize([r for r in rows if r['block'] == b]) for b in range(metadata['plan']['blocks'])]
    summary['system_sample_duration_ms'] = distribution([(s['end_ns'] - s['start_ns']) / 1e6 for s in system])
    summary['system_errors'] = sum('error' in s for s in system)
    summary['system_vm_delta_pages'] = {k: good[-1]['memory_pages'][k] - good[0]['memory_pages'][k]
                                        for k in ('Compressions', 'Decompressions', 'Pageins', 'Pageouts', 'Swapins', 'Swapouts')}
    summary['block_system'] = []
    for block in range(metadata['plan']['blocks']):
        block_rows = [r for r in rows if r['block'] == block]
        samples = [s for s in good if block_rows[0]['start_ns'] <= s['start_ns'] <= block_rows[-1]['end_ns']]
        if not samples:
            summary['block_system'].append({'sample_count': 0})
            continue
        first, last = samples[0], samples[-1]
        summary['block_system'].append({'first_sample_start_ns': first['start_ns'],
                                        'last_sample_end_ns': last['end_ns'],
                                        'sample_count': len(samples),
                                        'max_sample_duration_ms': max((s['end_ns']-s['start_ns']) / 1e6 for s in samples),
                                        'vm_delta_pages': {k: last['memory_pages'][k]-first['memory_pages'][k]
                                                           for k in summary['system_vm_delta_pages']}})
    summary['major_fault_values'] = sorted({r['go']['usage']['Majflt'] for r in rows})
    summary['go_block_io_counts'] = {'input': sum(r['go']['usage']['Inblock'] for r in rows),
                                    'output': sum(r['go']['usage']['Oublock'] for r in rows)}
    slow = [r for r in rows if r['go']['wall_ms'] >= 100]
    worst = max(rows, key=lambda r: r['go']['wall_ms'])
    # Neighbors and overlapping system intervals are directly recoverable by index.
    neighborhoods = []
    for row in [r for r in slow if r['go']['wall_ms'] >= 200] or [worst]:
        overlapping = [j for j in range(1, len(system))
                       if system[j-1]['start_ns'] <= row['go']['wait_return_ns']
                       and system[j]['end_ns'] >= row['go']['start_ns']]
        neighborhoods.append({'i': row['i'], 'neighbor_indices': list(range(max(0, row['i']-2), min(len(rows), row['i']+3))),
                              'system_interval_end_indices': overlapping})
    allowed_names = {'Codex (Renderer)', 'Google Chrome Helper (Renderer)', 'ffmpeg', 'ffmpeg-mac', 'WindowServer'}
    context_indices = sorted({j for n in neighborhoods for j in n['system_interval_end_indices']})
    contexts = [{'system_index': j, 'processes': [p for p in system[j].get('top_cpu_deltas', [])
                                                 if p['name'] in allowed_names]}
                for j in context_indices]
    return {'scope': 'One local macOS session, current Receipt diagnostic overlay, stock selected Go; not a before/after optimization or OS thread trace.',
            'metadata': metadata, 'summary': summary, 'rows': rows, 'system_samples': compact_system,
            'go_ge100_indices': [r['i'] for r in slow], 'neighborhoods': neighborhoods,
            'selected_process_context': contexts,
            'process_context_scope': 'Cumulative CPU deltas for surviving PIDs only, selected common application names; not a complete CPU accounting or evidence of blame.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('capture', type=Path)
    parser.add_argument('output', type=Path)
    options = parser.parse_args()
    report = analyze(options.capture)
    options.output.parent.mkdir(parents=True, exist_ok=True)
    with options.output.open('x') as stream:
        json.dump(report, stream, separators=(',', ':'))
        stream.write('\n')
    print(json.dumps(report['summary'], indent=2))


if __name__ == '__main__':
    main()
