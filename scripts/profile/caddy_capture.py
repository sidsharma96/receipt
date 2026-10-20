#!/usr/bin/env python3
"""Root operator helper: bounded Caddy experiment, rollback, numeric resource capture.
No tokens or application data are read. Root-only /run state is temporary.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

STATE=Path('/run/receipt-caddy-gzip-20261020')
CONFIG=Path('/etc/caddy/Caddyfile')
UNIT='receipt-caddy-gzip-rollback'
EXPECTED=b'receipt.sidsharma.dev {\n\treverse_proxy 127.0.0.1:7777\n}\n'


def run(args):
    return subprocess.check_output(args,text=True,timeout=30).strip()


def digest(value): return hashlib.sha256(value).hexdigest()


def replace(data):
    info=CONFIG.stat()
    descriptor,name=tempfile.mkstemp(prefix='.receipt-gzip-',dir=CONFIG.parent)
    try:
        with os.fdopen(descriptor,'wb') as stream:
            stream.write(data);stream.flush();os.fsync(stream.fileno())
        os.chmod(name,info.st_mode&0o777);os.chown(name,info.st_uid,info.st_gid)
        run(['caddy','validate','--config',name,'--adapter','caddyfile'])
        os.replace(name,CONFIG)
    finally:
        Path(name).unlink(missing_ok=True)
    run(['systemctl','reload','caddy'])


def prepare():
    if (STATE/'metadata.json').exists() or CONFIG.read_bytes()!=EXPECTED:
        raise RuntimeError('refusing existing session or unexpected Caddy configuration')
    profile=(STATE/'profile.Caddyfile').read_bytes()
    before=CONFIG.read_bytes()
    (STATE/'original.Caddyfile').write_bytes(before)
    meta={'started_ns':time.time_ns(),'original_sha256':digest(before),'profile_sha256':digest(profile),
          'caddy_version':run(['caddy','version']),
          'caddy_binary_sha256':digest(Path('/usr/bin/caddy').read_bytes()),
          'receipt_binary_sha256':digest(Path('/usr/local/bin/receipt').read_bytes()),
          'scope':'Temporary gzip for successful entry GETs; request/response headers removed before journal output.'}
    (STATE/'metadata.json').write_text(json.dumps(meta))
    # The root-owned timer restores only this exact profile, never unrelated edits.
    run(['systemd-run','--unit',UNIT,'--on-active=25m','/usr/bin/python3',str(STATE/'control.py'),'restore'])
    try:
        replace(profile)
    except BaseException:
        if CONFIG.read_bytes() in (profile,before): replace(before)
        raise
    print(json.dumps(meta))


def restore():
    meta=json.loads((STATE/'metadata.json').read_text())
    current=CONFIG.read_bytes()
    if digest(current)==meta['profile_sha256']:
        replace((STATE/'original.Caddyfile').read_bytes())
    elif digest(current)!=meta['original_sha256']:
        raise RuntimeError('refusing to overwrite concurrent Caddy changes')
    (STATE/'stop').touch()
    result={'restored_ns':time.time_ns(),'restored_sha256':digest(CONFIG.read_bytes()),
            'caddy_active':run(['systemctl','is-active','caddy'])}
    (STATE/'restored.json').write_text(json.dumps(result))
    subprocess.run(['systemctl','stop',UNIT+'.timer'],capture_output=True,timeout=10)
    print(json.dumps(result))


def sample():
    services={}
    for unit in ('caddy','receipt','postgresql@16-main'):
        pid=int(run(['systemctl','show',unit,'-p','MainPID','--value']))
        stat=Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()
        control=run(['systemctl','show',unit,'-p','ControlGroup','--value'])
        services[unit]=(pid,int(stat[19]),Path('/sys/fs/cgroup'+control))
    with (STATE/'samples.jsonl').open('x') as output:
        while not (STATE/'stop').exists():
            started=time.monotonic()
            row={'unix_ns':time.time_ns(),'services':{}}
            for unit,(pid,start,group) in services.items():
                cpu=dict(line.split() for line in (group/'cpu.stat').read_text().splitlines())
                status=Path(f'/proc/{pid}/status').read_text()
                rss=int(re.search(r'^VmRSS:\s*(\d+) kB',status,re.M).group(1))
                current=Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()
                if int(current[19])!=start: raise RuntimeError('service process changed')
                row['services'][unit]={'pid':pid,'start_ticks':start,'cpu_us':int(cpu['usage_usec']),
                                       'rss_kib':rss,'cgroup_bytes':int((group/'memory.current').read_text())}
            output.write(json.dumps(row)+'\n');output.flush()
            time.sleep(max(0,.1-(time.monotonic()-started)))


def export():
    meta=json.loads((STATE/'metadata.json').read_text())
    since=meta['started_ns']//1000000000
    caddy=[];receipt=[]
    for unit in ('caddy','receipt'):
        raw=run(['journalctl','-u',unit,'--since','@'+str(since),'-o','json','--no-pager'])
        for line in raw.splitlines():
            entry=json.loads(line)
            try: event=json.loads(entry.get('MESSAGE',''))
            except (ValueError,TypeError): continue
            if not isinstance(event,dict): continue
            if unit=='caddy' and event.get('logger')=='http.log.access.receipt_gzip':
                # No arbitrary fields are copied, even if Caddy's runtime schema changes.
                request_id=event.get('request_id','')
                if not isinstance(request_id,str) or not re.fullmatch('[a-f0-9]{32}-[a-f0-9]{1,16}',request_id): continue
                caddy.append({k:event.get(k) for k in ('ts','duration','status','size','request_id','encoding')})
            if unit=='receipt' and set(event)=={'version','request_id','operation','status','duration_ns','stages'}:
                receipt.append(event)
    samples=[json.loads(line) for line in (STATE/'samples.jsonl').read_text().splitlines()]
    print(json.dumps({'metadata':meta,'restoration':json.loads((STATE/'restored.json').read_text()),
                      'samples':samples,'caddy':caddy,'receipt':receipt}))

if __name__=='__main__':
    {'prepare':prepare,'restore':restore,'sample':sample,'export':export}[sys.argv[1]]()
