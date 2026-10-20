#!/usr/bin/env python3
"""Linux CI profiles of real cache-result decoding; no fixture/token in artifacts."""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import sys
import tempfile
import linux_ci_baseline as base
import http_trace


def main():
    if platform.system() != 'Linux' or os.environ.get('GITHUB_REF') != 'refs/heads/main':
        raise RuntimeError('requires trusted main Linux CI')
    out = Path(sys.argv[1]).resolve()
    out.mkdir(parents=True, exist_ok=False)
    if base.ci.main() != 0:
        raise RuntimeError('self-CI setup failed')
    env = base.ci.test_environment(str(Path(shutil.which('go')).resolve().parent), os.environ['ImageVersion'])
    env['GOPROXY'] = 'off'
    key = http_trace.key(base.ci.CLIENT, env)
    fixture = base.ci.HOME / 'receipt/entries' / (key + '.json')
    raw = fixture.read_bytes()
    payload = json.loads(raw)['payload']
    metadata = {'source_commit': base.run(['git','rev-parse','HEAD']).stdout.strip(),
                'run_id': os.environ['GITHUB_RUN_ID'], 'go_version': base.run(['go','version']).stdout.strip(),
                'runner_image': os.environ['ImageVersion'], 'cpus': os.cpu_count(),
                'key_before': key, 'body_bytes': len(raw), 'inputs': len(payload['manifest']['inputs']),
                'fixture_sha256': hashlib.sha256(raw).hexdigest(),
                'scope': 'Current-source isolated warm operations on self-CI result. Metadata rebuilt with json.Marshal, not original PostgreSQL JSONB bytes. No SQL, file I/O, auth, network or queue costs inside timed operations. Profiles perturb costs; unprofiled benchmarks are separate.'}
    with tempfile.TemporaryDirectory(prefix='receipt-app-profile-') as temporary:
        work = Path(temporary)
        source = base.SOURCE / 'internal/cache/store_test.go'
        text = source.read_text().replace('"context"','"context"\n "encoding/json"',1)
        replacement = work / 'store_test.go'
        replacement.write_text(text + '\n' + (base.SOURCE/'scripts/profile/application_profile_test.go.inc').read_text())
        base.run(['gofmt','-w',str(replacement)])
        overlay = work / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {str(source): str(replacement)}}))
        binary = work / 'cache.test'
        base.run(['go','test','-trimpath','-overlay',str(overlay),'-c','-o',str(binary),'./internal/cache'],cwd=base.SOURCE)
        # Test process receives no CI bearer token and no arbitrary runner settings.
        bench_env = {name: os.environ[name] for name in ('PATH','HOME')}
        bench_env['RECEIPT_PROFILE_FIXTURE'] = str(fixture)
        common = [str(binary),'-test.run=^$']
        result = base.run(common+['-test.bench=^BenchmarkApplicationProfile$','-test.benchtime=1s','-test.count=5'],env=bench_env)
        (out/'bench.txt').write_text(result.stdout)
        for name in ('Decode','MetadataDecode','Encode'):
            stem = out/name
            profiled = base.run(common+['-test.bench=^BenchmarkApplicationProfile$/^'+name+'$', '-test.benchtime=5s',
                            '-test.cpuprofile='+str(stem)+'.cpu.pprof','-test.memprofile='+str(stem)+'.alloc.pprof',
                            '-test.memprofilerate=65536'],env=bench_env)
            selected = set(re.findall(r'^BenchmarkApplicationProfile/([A-Za-z]+)-[0-9]+\s', profiled.stdout, re.M))
            if selected != {name}:
                raise RuntimeError('profile benchmark selection was not isolated')
            (out/(name+'.profile-bench.txt')).write_text(profiled.stdout)
            for kind in ('cpu','alloc'):
                # CPU profile uses its default cpu sample index; allocation uses bytes.
                args = ['go','tool','pprof','-top','-nodecount=35']
                if kind == 'alloc': args.append('-alloc_space')
                report = base.run(args+[str(binary),str(stem)+'.'+kind+'.pprof'])
                (out/(name+'.'+kind+'.txt')).write_text(report.stdout)
        metadata['key_after'] = http_trace.key(base.ci.CLIENT, env)
        if metadata['key_after'] != key:
            raise RuntimeError('fixture key drift')
    metadata['completed'] = True
    (out/'metadata.json').write_text(json.dumps(metadata,indent=2))


if __name__ == '__main__':
    main()
