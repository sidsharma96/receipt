#!/usr/bin/env python3
"""Fixed-sample, randomized paired macOS discovery experiment.
Usage: discovery_controlled.py BEFORE AFTER OUTPUT [--quartets]
Build BEFORE from 97b833e and AFTER from 4cbf0d7, with identical flags/toolchain.
No source changes, packages, power settings or user processes are modified.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import re
import resource
import statistics
import subprocess
import tempfile
import time

SEED = 20261018
BLOCKS = 3
PAIRS = {'go': 20, 'generic': 20, 'go-aa': 10}
WARMUP_PAIRS = 3
BOOTSTRAPS = 5000
CHUNK = 5


def percentile(values, fraction):
    ordered = sorted(values)
    at = (len(ordered)-1)*fraction
    lo = int(at)
    return ordered[lo] + (ordered[min(lo+1,len(ordered)-1)]-ordered[lo])*(at-lo)


def interval(values, rng):
    draws = [statistics.fmean(rng.choices(values,k=len(values))) for _ in range(BOOTSTRAPS)]
    return [percentile(draws,0.025),percentile(draws,0.975)]


def summary(pairs):
    """Positive difference is a saving. Chunks retain short-range time dependence."""
    result = {}
    for metric in pairs[0]['before']['metrics']:
        before = [p['before']['metrics'][metric] for p in pairs]
        after = [p['after']['metrics'][metric] for p in pairs]
        delta = [a-b for a,b in zip(before,after)]
        chunks = []
        for block in range(BLOCKS):
            subset = [p['before']['metrics'][metric]-p['after']['metrics'][metric] for p in pairs if p['block']==block]
            assert len(subset)%CHUNK==0
            chunks.extend(statistics.fmean(subset[i:i+CHUNK]) for i in range(0,len(subset),CHUNK))
        result[metric] = {'before_mean':statistics.fmean(before),'after_mean':statistics.fmean(after),
            'before_median':statistics.median(before),'after_median':statistics.median(after),
            'before_range':[min(before),max(before)],'after_range':[min(after),max(after)],
            'paired_mean_saving':statistics.fmean(delta),'paired_median_saving':statistics.median(delta),
            'paired_bootstrap_95':interval(delta,random.Random(SEED)),
            'chunk_bootstrap_95':interval(chunks,random.Random(SEED)),
            'block_mean_savings':[statistics.fmean(p['before']['metrics'][metric]-p['after']['metrics'][metric] for p in pairs if p['block']==block) for block in range(BLOCKS)],
            'order_mean_savings':{order:statistics.fmean(p['before']['metrics'][metric]-p['after']['metrics'][metric] for p in pairs if p['order']==order) for order in ['before-first','after-first']}}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('before')
    parser.add_argument('after')
    parser.add_argument('output')
    parser.add_argument('--quartets', action='store_true')
    parser.add_argument('--before-revision', default='97b833e')
    parser.add_argument('--after-revision', default='4cbf0d7')
    options = parser.parse_args()
    before,after = [str(Path(p).resolve()) for p in [options.before, options.after]]
    output = Path(options.output); output.parent.mkdir(parents=True,exist_ok=True)
    quartets = options.quartets
    counts = {'go':20,'generic':10,'go-aa':20} if quartets else PAIRS
    env = dict(os.environ)
    rng = random.Random(SEED)
    def run(command,cwd=None):
        return subprocess.run(command,cwd=cwd,env=env,capture_output=True,text=True,check=True)
    def state():
        return {'loadavg':list(os.getloadavg()),'battery':run(['pmset','-g','batt']).stdout.strip(),
                'thermal':run(['pmset','-g','therm']).stdout.strip()}
    report = {'protocol':{'seed':SEED,'blocks':BLOCKS,'pairs_per_block':counts,'design':'mirrored quartets' if quartets else 'adjacent pairs','warmup_pairs_per_case_per_block':WARMUP_PAIRS,
               'bootstrap_replicates':BOOTSTRAPS,'chunk_size_pairs':CHUNK,'primary':'paired arithmetic mean saving in Receipt duration',
               'exclusions':'none; only prespecified warmups excluded','environment':'one inherited environment snapshot for every child',
               'before_revision':options.before_revision,'after_revision':options.after_revision,'build_flags':'-trimpath -buildvcs=false -ldflags=-linkmode=external'},
              'go':run(['go','version']).stdout.strip(),'git':run(['git','--version']).stdout.strip(),
              'platform':run(['uname','-sm']).stdout.strip(),'cpu':run(['sysctl','-n','machdep.cpu.brand_string']).stdout.strip(),
              'binary_sha256':{v:hashlib.sha256(Path(p).read_bytes()).hexdigest() for v,p in [('before',before),('after',after)]},
              'states':[],'pairs':[],'root_queries':[],'keys':{},'completed':False}
    # Write the plan before observing timings; do not stop or change counts based on outcomes.
    output.write_text(json.dumps(report,indent=2)+'\n')
    def measured(binary,args,cwd):
        start_usage = resource.getrusage(resource.RUSAGE_CHILDREN)
        start = time.perf_counter_ns()
        p = run(['/usr/bin/time','-l',binary]+args,cwd)
        elapsed = (time.perf_counter_ns()-start)/1e6
        usage = resource.getrusage(resource.RUSAGE_CHILDREN)
        timing = next(json.loads(line) for line in (p.stdout+p.stderr).splitlines() if line.startswith('{"type":"receipt_timing"'))
        stages = {s['stage']:s for s in timing['stages']}
        if 'execution' in stages or 'receipt: hit' not in p.stderr:
            raise RuntimeError('expected cached replay')
        rss = int(re.search(r'(\d+)\s+maximum resident set size',p.stderr).group(1))
        metrics = {'receipt_ms':timing['duration_ns']/1e6,'external_ms':elapsed,
            'cpu_ms':1000*((usage.ru_utime-start_usage.ru_utime)+(usage.ru_stime-start_usage.ru_stime)),
            'rss_mb':rss/1e6}
        for stage in ['discovery','toolchain','input_files','executable','git_selection']:
            metrics[stage+'_ms'] = stages.get(stage,{}).get('ns',0)/1e6
        metrics['outside_discovery_ms'] = metrics['receipt_ms']-metrics['discovery_ms']
        return {'metrics':metrics,'timing':timing}
    with tempfile.TemporaryDirectory(prefix='receipt-controlled-') as temp:
        base = Path(temp).resolve(); root = base/'repo'; root.mkdir()
        run(['git','init','-q',str(root)])
        (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
        (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
        (root/'.gitignore').write_text('ignored\n')
        (root/'ignored').write_text('excluded')
        for i in range(256):
            (root/f'input-{i}').write_bytes(bytes([i])*4096)
        cache = base/'cache'
        arguments = {'go':['run','--timings','--cache-dir',str(cache),'--','go','test','-count=1','./...'],
                     'generic':['run','--timings','--cache-dir',str(cache),'--allow-generic','--','/bin/sh','-c','true']}
        arguments['go-aa'] = arguments['go']
        for case in ['go','generic']:
            run([before]+arguments[case],root)
            keys = {}
            for label,binary in [('before',before),('after',after)]:
                args = ['explain']+[a for a in arguments[case][1:] if a!='--timings']
                keys[label] = re.search(r'key: ([a-f0-9]{64})',run([binary]+args,root).stdout).group(1)
            assert keys['before']==keys['after'], 'incompatible keys'
            report['keys'][case] = keys
        for block in range(BLOCKS):
            report['states'].append({'block':block,'position':'before',**state()})
            for case in counts:
                for _ in range(WARMUP_PAIRS):
                    measured(before,arguments[case],root)
                    measured(before if case=='go-aa' else after,arguments[case],root)
            schedule = []
            for case,count in counts.items():
                orders = ['before-first']*(count//2)+['after-first']*(count//2)
                rng.shuffle(orders)
                schedule.extend((case,order) for order in orders)
            schedule.extend(('root-query','') for _ in range(20))
            rng.shuffle(schedule)
            for sequence,(case,order) in enumerate(schedule):
                if case=='root-query':
                    started = time.perf_counter_ns()
                    p = run(['git','rev-parse','--show-toplevel'],root)
                    elapsed = (time.perf_counter_ns()-started)/1e6
                    assert Path(p.stdout.strip())==root
                    report['root_queries'].append({'block':block,'sequence':sequence,'elapsed_ms':elapsed})
                    continue
                pair = {'block':block,'sequence':sequence,'case':case,'order':order}
                labels = ['before','after'] if order=='before-first' else ['after','before']
                if quartets:
                    labels += list(reversed(labels))
                runs = []
                for label in labels:
                    binary = after if label=='after' and case!='go-aa' else before
                    runs.append({'label':label,**measured(binary,arguments[case],root)})
                if quartets:
                    pair['runs'] = runs
                    for label in ['before','after']:
                        selected = [r for r in runs if r['label']==label]
                        pair[label] = {'metrics':{k:statistics.fmean(r['metrics'][k] for r in selected) for k in selected[0]['metrics']}}
                else:
                    for r in runs:
                        pair[r['label']] = {'metrics':r['metrics'],'timing':r['timing']}
                report['pairs'].append(pair)
            report['states'].append({'block':block,'position':'after',**state()})
            output.write_text(json.dumps(report,indent=2)+'\n')
            print('Completed block',block+1,flush=True)
        # Check keys again after all measurements; the fixture must remain unchanged.
        for case in ['go','generic']:
            for label,binary in [('before',before),('after',after)]:
                args = ['explain']+[a for a in arguments[case][1:] if a!='--timings']
                key = re.search(r'key: ([a-f0-9]{64})',run([binary]+args,root).stdout).group(1)
                assert key==report['keys'][case][label], 'fixture identity changed'
    report['summary'] = {case:summary([p for p in report['pairs'] if p['case']==case]) for case in counts}
    roots = [s['elapsed_ms'] for s in report['root_queries']]
    report['root_summary'] = {'mean_ms':statistics.fmean(roots),'median_ms':statistics.median(roots),'range_ms':[min(roots),max(roots)]}
    report['completed'] = True
    output.write_text(json.dumps(report,indent=2)+'\n')
    for case,data in report['summary'].items():
        for metric in ['receipt_ms','discovery_ms','outside_discovery_ms','toolchain_ms','cpu_ms','rss_mb']:
            s = data[metric]
            print(case,metric,'means',s['before_mean'],s['after_mean'],'saving',s['paired_mean_saving'],'chunk95',s['chunk_bootstrap_95'],'blocks',s['block_mean_savings'])
    print('root query',report['root_summary'])

if __name__=='__main__':
    main()
