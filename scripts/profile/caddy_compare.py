#!/usr/bin/env python3
"""Actual Caddy HTTPS comparison; operator configures/observes the existing server.
Only fixed numeric records and hashes enter the artifact. Never logs credentials.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import sys
import tempfile
import time
import http_trace
import linux_ci_baseline as base

ci=base.ci
SOURCE=base.SOURCE


def validate(row,case):
    if row['returncode'] or not row['hit'] or row['timing']['failed'] or any(s['stage']=='execution' for s in row['timing']['stages']):
        raise RuntimeError('expected successful remote hit without execution')
    if len(row.get('http',[]))!=1:
        raise RuntimeError('expected one HTTP request')
    trace=row['http'][0]
    if trace['status']!=200 or trace['uncompressed']!=(case=='gzip') or trace['reused']:
        raise RuntimeError('compression not active or unexpected response')
    if not re.fullmatch('[a-f0-9]{32}-[a-f0-9]{1,16}',trace['request_id']):
        raise RuntimeError('missing safe server request ID')


def main():
    if platform.system()!='Linux' or os.environ.get('GITHUB_REF')!='refs/heads/main':
        raise RuntimeError('requires trusted main Linux CI')
    out=Path(sys.argv[1]).resolve();out.mkdir(parents=True,exist_ok=False)
    plan={'blocks':3,'calls_per_variant_per_block':30,'warmups_per_variant_per_block':2,
          'pause_after_warmups_seconds':5,'pause_between_variants_seconds':5,
          'order':[['identity','gzip'],['gzip','identity'],['identity','gzip']],
          'source_commit':base.run(['git','rev-parse','HEAD'],cwd=SOURCE).stdout.strip(),
          'scope':'Actual HTTPS Caddy/Receipt/PostgreSQL deployment. Fresh CLI and empty cache per call. Same declared stock input, diagnostic gzip/default and identity-request builds. Resource windows include background work.'}
    (out/'plan.json').write_text(json.dumps(plan,indent=2))
    with tempfile.TemporaryDirectory(prefix='receipt-caddy-build-') as temporary:
        work=Path(temporary);(work/'identity').mkdir();(work/'gzip').mkdir()
        clients={}
        for case in ('identity','gzip'):
            clients[case],overlay=http_trace.build(work/case,compression=True,identity=case=='identity')
            if case=='identity':
                base.run(['go','test','-race','-overlay',str(overlay),'./internal/cache'],cwd=SOURCE)
        if ci.main()!=0: raise RuntimeError('self-CI setup failed')
        env=ci.test_environment(str(Path(shutil.which('go')).resolve().parent),os.environ['ImageVersion'])
        env['GOPROXY']='off'
        with ci.remote_cache() as ready:
            if not ready: raise RuntimeError('shared cache unavailable')
            env['RECEIPT_TOKEN']=os.environ['RECEIPT_SELF_CI_TOKEN']
            keys={case+'_before':http_trace.key(binary,env) for case,binary in {'stock':ci.CLIENT,**clients}.items()}
            if len(set(keys.values()))!=1: raise RuntimeError('key incompatibility')
            report={'completed':False,'run_id':os.environ['GITHUB_RUN_ID'],'plan':plan,'keys':keys,
                    'runner_image':os.environ['ImageVersion'],'go_version':base.run(['go','version']).stdout.strip(),
                    'cpus':os.cpu_count(),'cpu_model':re.search(r'^model name\s*:\s*(.*)$',Path('/proc/cpuinfo').read_text(),re.M).group(1),
                    'binary_sha256':{case:hashlib.sha256(Path(binary).read_bytes()).hexdigest() for case,binary in {'stock':ci.CLIENT,**clients}.items()},'rows':[]}
            try:
                for block,cases in enumerate(plan['order']):
                    for case in cases:
                        for i in range(-2,30):
                            if i==0: time.sleep(5)
                            with tempfile.TemporaryDirectory(prefix='receipt-caddy-empty-',dir=ci.HOME) as empty:
                                args=[str(clients[case]),'run','--timings','--cache-dir',empty,'--remote',ci.REMOTE,'--']+ci.COMMAND
                                row=base.measure(args,env,case,block)
                            validate(row,case)
                            if i>=0:
                                row['i']=i;report['rows'].append(row)
                        print('completed block',block,case,flush=True)
                        time.sleep(5)
                keys.update({case+'_after':http_trace.key(binary,env) for case,binary in {'stock':ci.CLIENT,**clients}.items()})
                if len(set(keys.values()))!=1 or len({r['stdout_sha256'] for r in report['rows']})!=1 or len({r['http'][0]['decoded_sha256'] for r in report['rows']})!=1:
                    raise RuntimeError('key, decoded result or replay drift')
                report['completed']=True
            finally:
                (out/'results.json').write_text(json.dumps(report,indent=2))

if __name__=='__main__': main()
