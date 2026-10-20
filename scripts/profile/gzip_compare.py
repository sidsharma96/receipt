#!/usr/bin/env python3
"""Controlled gzip comparison on a disposable Linux CI runner; no live-server changes."""
import contextlib
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import linux_ci_baseline as base
import http_trace

ci=base.ci
SOURCE=base.SOURCE
NS='receipt-gzip'
ADDRESS='10.203.0.2'


def privileged(args):
    return base.run(['sudo','-n']+args)


@contextlib.contextmanager
def network():
    # Only this private veth pair is shaped. The runner's Internet link is untouched.
    privileged(['ip','netns','add',NS])
    try:
        privileged(['ip','link','add','rg-host','type','veth','peer','name','rg-peer'])
        privileged(['ip','link','set','rg-peer','netns',NS])
        privileged(['ip','addr','add','10.203.0.1/30','dev','rg-host'])
        privileged(['ip','link','set','rg-host','up'])
        privileged(['ip','netns','exec',NS,'ip','addr','add',ADDRESS+'/30','dev','rg-peer'])
        privileged(['ip','netns','exec',NS,'ip','link','set','rg-peer','up'])
        privileged(['ip','netns','exec',NS,'ip','link','set','lo','up'])
        yield
    finally:
        # Deleting the namespace removes its peer and the host veth.
        privileged(['ip','netns','del',NS])
        subprocess.run(['sudo','-n','ip','link','del','rg-host'],capture_output=True,timeout=15)


def delay(milliseconds):
    options=['root','netem','delay',str(milliseconds)+'ms']
    privileged(['tc','qdisc','replace','dev','rg-host']+options)
    privileged(['ip','netns','exec',NS,'tc','qdisc','replace','dev','rg-peer']+options)


class Server:
    def __init__(self,binary,certificate,cache,key,token,compressed,port):
        self.process=subprocess.Popen(['sudo','-n','ip','netns','exec',NS,str(binary)],
              stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,bufsize=1)
        self.send({'address':ADDRESS+':'+str(port),'certificate':str(certificate/'certificate.pem'),
                   'private_key':str(certificate/'private.pem'),'cache':str(cache),'key':key,'token':token,'gzip':compressed})
        self.ready=self.read(self.process.stdout)
        self.url='https://'+ADDRESS+':'+str(port)

    def send(self,value):
        self.process.stdin.write((json.dumps(value) if isinstance(value,dict) else value)+'\n')
        self.process.stdin.flush()

    def read(self,stream):
        # Bound pipe waits even if a fixture fails before writing its expected record.
        import select
        if not select.select([stream],[],[],20)[0]:
            raise RuntimeError('fixture record timeout')
        line=stream.readline()
        try:
            return json.loads(line)
        except ValueError:
            raise RuntimeError('fixture returned no safe JSON record') from None

    def snapshot(self):
        self.send('snapshot')
        return self.read(self.process.stdout)

    def close(self):
        if self.process.poll() is None:
            self.send('stop')
        try:
            code=self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.kill(); self.process.wait(timeout=5)
            raise RuntimeError('fixture shutdown timed out') from None
        if code:
            raise RuntimeError('fixture failed')


def validate(row,server,ready,compressed):
    if row['returncode'] or not row['hit'] or row['timing']['failed'] or any(s['stage']=='execution' for s in row['timing']['stages']):
        raise RuntimeError('expected a validated successful remote hit')
    if len(row.get('http',[]))!=1:
        raise RuntimeError('missing HTTP trace')
    trace=row['http'][0]
    if trace['status']!=200 or trace['uncompressed']!=compressed or trace['reused']:
        raise RuntimeError('unexpected response or connection')
    if server['type']!='gzip_server' or server['status']!=200 or server['failed'] or server['gzip']!=compressed:
        raise RuntimeError('unexpected fixture response')
    if trace['decoded_sha256']!=ready['decoded_sha256'] or server['decoded_sha256']!=ready['decoded_sha256']:
        raise RuntimeError('decoded bytes changed')
    if trace['bytes']!=ready['decoded_bytes'] or server['decoded_bytes']!=ready['decoded_bytes']:
        raise RuntimeError('decoded byte count changed')
    if (server['encoded_bytes'] < server['decoded_bytes']) != compressed:
        raise RuntimeError('compression accounting mismatch')


def main():
    if platform.system()!='Linux' or os.environ.get('GITHUB_REF')!='refs/heads/main':
        raise RuntimeError('requires trusted disposable Linux main CI')
    for command in ('ip','tc','sudo'):
        if not shutil.which(command): raise RuntimeError('required preinstalled network tool unavailable')
    out=Path(sys.argv[1]).resolve();out.mkdir(parents=True,exist_ok=False)
    plan={'blocks':3,'pairs_per_block':20,'warmups_per_variant_per_block':2,'one_way_delay_ms':[0,12,32],
          'gzip_level':5,'source_commit':base.run(['git','rev-parse','HEAD'],cwd=SOURCE).stdout.strip(),
          'scope':'Controlled private-veth HTTPS network, current Receipt Disk/Handler, fresh level-5 Go gzip writer per response. Not production Caddy/PostgreSQL or Internet latency. Fresh CLI; fresh server pair each block.'}
    (out/'plan.json').write_text(json.dumps(plan,indent=2))
    with tempfile.TemporaryDirectory(prefix='receipt-gzip-build-') as temporary:
        work=Path(temporary)
        client,overlay=http_trace.build(work,compression=True)
        server_binary=work/'gzipserver'
        base.run(['go','build','-trimpath','-buildvcs=false','-o',str(server_binary),'./scripts/profile/gzipserver'],cwd=SOURCE)
        base.run(['go','test','-race','-overlay',str(overlay),'./internal/cache','./scripts/profile/gzipserver'],cwd=SOURCE)
        if ci.main()!=0: raise RuntimeError('self-CI setup failed')
        certificate=ci.HOME/'gzip-certificate';certificate.mkdir(mode=0o700)
        base.run([str(server_binary),'certificate',str(certificate)])
        env=ci.test_environment(str(Path(shutil.which('go')).resolve().parent),os.environ['ImageVersion'])
        env.update(GOPROXY='off',SSL_CERT_FILE=str(certificate/'certificate.pem'),RECEIPT_TOKEN=secrets.token_hex(32))
        configuration=ci.ROOT/'.receipt.json'
        config=json.loads(configuration.read_text());config['extra_inputs'].append(env['SSL_CERT_FILE'])
        configuration.write_text(json.dumps(config))
        # The certificate declaration changes the fixture key. Seed that key locally.
        base.run([ci.CLIENT,'run','--cache-dir',str(ci.HOME/'receipt'),'--']+ci.COMMAND,cwd=ci.ROOT,env=env)
        keys={'stock_before':http_trace.key(ci.CLIENT,env),'traced_before':http_trace.key(client,env)}
        if len(set(keys.values()))!=1: raise RuntimeError('key incompatibility')
        report={'plan':plan,'run_id':os.environ['GITHUB_RUN_ID'],'completed':False,'keys':keys,
                'runner_image':os.environ['ImageVersion'],'go_version':base.run(['go','version']).stdout.strip(),
                'cpus':os.cpu_count(),'cpu_model':re.search(r'^model name\s*:\s*(.*)$',Path('/proc/cpuinfo').read_text(),re.M).group(1),
                'client_sha256':hashlib.sha256(client.read_bytes()).hexdigest(),'stock_sha256':hashlib.sha256(Path(ci.CLIENT).read_bytes()).hexdigest(),
                'server_sha256':hashlib.sha256(server_binary.read_bytes()).hexdigest(),'rows':[],'server_batches':[]}
        try:
            with network():
                for one_way in plan['one_way_delay_ms']:
                    delay(one_way)
                    for block in range(plan['blocks']):
                        servers={}
                        try:
                            for compressed in (False,True):
                                servers[compressed]=Server(server_binary,certificate,ci.HOME/'receipt',keys['stock_before'],env['RECEIPT_TOKEN'],compressed,8443+int(compressed))
                            if servers[False].ready['decoded_sha256']!=servers[True].ready['decoded_sha256']:
                                raise RuntimeError('fixture result differs')
                            starts={}
                            for i in range(-2,plan['pairs_per_block']):
                                if i==0: starts={mode:server.snapshot() for mode,server in servers.items()}
                                order=[False,True] if (i+block)%2==0 else [True,False]
                                for compressed in order:
                                    server=servers[compressed]
                                    with tempfile.TemporaryDirectory(prefix='receipt-gzip-empty-',dir=ci.HOME) as empty:
                                        args=[str(client),'run','--timings','--cache-dir',empty,'--remote',server.url,'--']+ci.COMMAND
                                        row=base.measure(args,env,'gzip' if compressed else 'identity',block)
                                    event=server.read(server.process.stderr)
                                    validate(row,event,server.ready,compressed)
                                    if i>=0:
                                        row.update(one_way_delay_ms=one_way,i=i,server=event)
                                        report['rows'].append(row)
                            for compressed,server in servers.items():
                                report['server_batches'].append({'case':'gzip' if compressed else 'identity','one_way_delay_ms':one_way,'block':block,
                                   'calls':plan['pairs_per_block'],'before':starts[compressed],'after':server.snapshot(),
                                   'rss_scope':'Fresh server process per variant/block; peak includes setup and two warmups. CPU delta excludes warmups, includes TLS and diagnostics.'})
                        finally:
                            for server in servers.values(): server.close()
                        print('completed delay',one_way,'block',block,flush=True)
            keys.update(stock_after=http_trace.key(ci.CLIENT,env),traced_after=http_trace.key(client,env))
            if len(set(keys.values()))!=1 or len({r['stdout_sha256'] for r in report['rows']})!=1:
                raise RuntimeError('key or replay drift')
            report['completed']=True
        finally:
            (out/'results.json').write_text(json.dumps(report,indent=2))

if __name__=='__main__': main()
