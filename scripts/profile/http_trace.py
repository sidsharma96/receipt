#!/usr/bin/env python3
"""Build and measure a Go HTTP diagnostic overlay. Never changes production files."""
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

SOURCE = base.SOURCE
ci = base.ci


def build(out, compression=False, identity=False):
    source = SOURCE / 'internal/cache/http.go'
    text = source.read_text()
    changes = {
        '"context"': '"context"\n "crypto/tls"\n "net/http/httptrace"\n "os"\n "regexp"\n "sync"',
        'return c.Do(req)': 'return diagnosticRequest(c, req, kind)',
        '\tb, e := io.ReadAll(io.LimitReader(resp.Body, MaxEntryBytes+1))': '\tdiagnosticMark(resp, "body_read_start")\n\tb, e := io.ReadAll(io.LimitReader(resp.Body, MaxEntryBytes+1))\n\tdiagnosticMark(resp, "body_read_done")',
        '\tv, e = Decode(b)': '\tdiagnosticMark(resp, "decode_start")\n\tv, e = Decode(b)\n\tdiagnosticMark(resp, "decode_done")',
    }
    for old, new in changes.items():
        if text.count(old) != 1:
            raise RuntimeError('HTTP source changed: review overlay anchor')
        text = text.replace(old, new)
    if identity:
        anchor = 'req.Header.Set("Content-Type", "application/json")'
        if text.count(anchor) != 1:
            raise RuntimeError('identity request anchor changed')
        text = text.replace(anchor, anchor + '\n req.Header.Set("Accept-Encoding", "identity")')
    fragment = (SOURCE / 'scripts/profile/http_trace.go.inc').read_text()
    if compression:
        text = text.replace('"context"', '"context"\n "crypto/sha256"\n "encoding/hex"\n "hash"')
        changes = {
            'body io.ReadCloser': 'body io.ReadCloser\n digest hash.Hash\n uncompressed bool',
            'd.bytes += int64(n)': '_, _ = d.digest.Write(p[:n])\n d.bytes += int64(n)',
            'Events []diagnosticHTTPEvent': 'DecodedSHA256 string `json:"decoded_sha256"`\n Uncompressed bool `json:"uncompressed"`\n Events []diagnosticHTTPEvent',
            'd.bytes,d.events}': 'd.bytes,hex.EncodeToString(d.digest.Sum(nil)),d.uncompressed,d.events}',
            'kind:kind}': 'kind:kind,digest:sha256.New()}',
            'd.status = resp.StatusCode': 'd.uncompressed = resp.Uncompressed\n d.status = resp.StatusCode',
        }
        for old, new in changes.items():
            if fragment.count(old) != 1:
                raise RuntimeError('compression diagnostic anchor changed')
            fragment = fragment.replace(old, new)
    replacement = out / 'http.go'
    replacement.write_text(text + '\n' + fragment)
    base.run(['gofmt', '-w', str(replacement)])
    overlay = out / 'overlay.json'
    test_source = SOURCE / 'internal/cache/diagnostics_test.go'
    test_replacement = out / 'diagnostics_test.go'
    test_replacement.write_text(test_source.read_text() + (SOURCE / 'scripts/profile/http_trace_test.go.inc').read_text())
    base.run(['gofmt', '-w', str(test_replacement)])
    overlay.write_text(json.dumps({'Replace': {str(source): str(replacement), str(test_source): str(test_replacement)}}))
    binary = out / 'receipt-traced'
    base.run(['go', 'build', '-trimpath', '-buildvcs=false', '-overlay', str(overlay), '-o', str(binary), './cmd/receipt'], cwd=SOURCE)
    return binary, overlay


def key(binary, env):
    p = base.run([str(binary), 'explain', '--cache-dir', str(ci.HOME / 'receipt'), '--'] + ci.COMMAND, cwd=ci.ROOT, env=env)
    return re.search(r'key: ([a-f0-9]{64})', p.stdout).group(1)


def main():
    if platform.system() != 'Linux' or os.environ.get('GITHUB_REF') != 'refs/heads/main':
        raise RuntimeError('requires trusted main Linux CI')
    out = Path(sys.argv[1]).resolve(); out.mkdir(parents=True, exist_ok=False)
    # Build outside the cloned workload and artifact directory.
    with tempfile.TemporaryDirectory(prefix='receipt-http-build-') as temporary:
        binary, overlay = build(Path(temporary))
        base.run(['go', 'test', '-race', '-overlay', str(overlay), './internal/cache'], cwd=SOURCE)
        if ci.main() != 0:
            raise RuntimeError('seed failed')
        env = ci.test_environment(str(Path(shutil.which('go')).resolve().parent), os.environ['ImageVersion'])
        env['GOPROXY'] = 'off'
        with ci.remote_cache() as ready:
            if not ready:
                raise RuntimeError('remote unavailable')
            env['RECEIPT_TOKEN'] = os.environ['RECEIPT_SELF_CI_TOKEN']
            keys = {'stock_before': key(ci.CLIENT, env), 'traced_before': key(binary, env)}
            if len(set(keys.values())) != 1:
                raise RuntimeError('overlay changed key')
            report = {'completed': False, 'source_commit': base.run(['git','rev-parse','HEAD'],cwd=SOURCE).stdout.strip(),
                      'run_id': os.environ['GITHUB_RUN_ID'], 'runner_image': os.environ['ImageVersion'],
                      'go_version': base.run(['go','version']).stdout.strip(), 'keys': keys,
                      'stock_sha256': hashlib.sha256(Path(ci.CLIENT).read_bytes()).hexdigest(),
                      'traced_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                      'scope': 'Fresh CLI processes, alternating stock/overlay. Declared CLIENT extra input remains stock for both. Three blocks, 10 measured calls per variant/case/block; one excluded warmup each.',
                      'rows': []}
            try:
                for block in range(3):
                    cases = ['checked-local', 'remote-fetch'] if block % 2 == 0 else ['remote-fetch', 'checked-local']
                    for case in cases:
                        for i in range(-1,10):
                            variants = [('stock',ci.CLIENT),('traced',str(binary))]
                            if (i+block)%2: variants.reverse()
                            for variant, client in variants:
                                with tempfile.TemporaryDirectory(prefix='receipt-http-cache-', dir=ci.HOME) as empty:
                                    cache = empty if case == 'remote-fetch' else str(ci.HOME/'receipt')
                                    args = [client,'run','--timings','--cache-dir',cache,'--remote',ci.REMOTE,'--']+ci.COMMAND
                                    row = base.measure(args,env,variant+'-'+case,block)
                                    stages = [s['stage'] for s in row['timing']['stages']]
                                    if row['returncode'] or not row['hit'] or row['timing']['failed'] or 'execution' in stages:
                                        raise RuntimeError('expected successful hit')
                                    traces = row.get('http',[])
                                    if variant == 'traced' and (len(traces)!=1 or traces[0]['status'] != (200 if case=='remote-fetch' else 204)):
                                        raise RuntimeError('missing or unexpected HTTP trace')
                                    if i >= 0:
                                        row['i'] = i; report['rows'].append(row)
                        print('completed block',block,case,flush=True)
                keys.update(stock_after=key(ci.CLIENT,env),traced_after=key(binary,env))
                if len(set(keys.values())) != 1:
                    raise RuntimeError('fixture key drift')
                if len({r['stdout_sha256'] for r in report['rows']}) != 1:
                    raise RuntimeError('replayed stdout differs')
                report['summary'] = base.distributions(report['rows'])
                report['completed'] = True
            finally:
                (out/'results.json').write_text(json.dumps(report,indent=2))

if __name__ == '__main__':
    main()
