#!/usr/bin/env python3
"""Read-only Linux CI network diagnostics. No seeding, writes or token artifacts."""
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import tempfile
import time
import shutil
import hashlib
import threading

PACKET = re.compile(r'^([0-9]+\.[0-9]+) IP (\S+)\.(\d+) > (\S+)\.(\d+): Flags \[([^]]*)\], (.*)$')

def packet_record(line):
    match=PACKET.match(line.strip())
    if not match:return None
    stamp,source,sport,destination,dport,flags,rest=match.groups()
    if source!="104.248.183.112" and destination!="104.248.183.112":return None
    incoming=source=="104.248.183.112"
    result={"unix_ns":int(float(stamp)*1e9),"incoming":incoming,"client_port":int(dport if incoming else sport),"flags":flags}
    for field,pattern in (("seq",r"seq (\d+)"),("seq_end",r"seq \d+:(\d+)"),("ack",r"ack (\d+)"),("length",r"length (\d+)"),("window",r"win (\d+)")):
        value=re.search(pattern,rest)
        if value:result[field]=int(value.group(1))
    return result


SOURCE=Path(__file__).resolve().parents[2]


def run(args,**kwargs):
    return subprocess.run(args,capture_output=True,text=True,timeout=180,check=True,**kwargs)


def main():
    if platform.system()!='Linux' or os.environ.get('GITHUB_REF')!='refs/heads/main':
        raise RuntimeError('trusted Linux main workflow required')
    out=Path(sys.argv[1]).resolve();out.mkdir(parents=True,exist_ok=False)
    fixture=json.loads((SOURCE/'docs/measurements/application-profile-2026-10-20/server-metadata.json').read_text())['fixture_key']
    metadata={'source_commit':run(['git','rev-parse','HEAD']).stdout.strip(),'run_id':os.environ['GITHUB_RUN_ID'],
              'go_version':run(['go','version']).stdout.strip(),'runner_image':os.environ['ImageVersion'],
              'cpu_model':re.search(r'^model name\s*:\s*(.*)$',Path('/proc/cpuinfo').read_text(),re.M).group(1),
              'cpus':os.cpu_count(),'fixture_key':fixture,'started_unix_ns':time.time_ns(),
              'scope':'Read-only existing fixture. Fresh child processes and separately labeled persistent controls. Actual cache.Remote Get/Check with wrapped default transport; socket/body callbacks add observer cost. No repository fingerprint, execution or publication in timing.'}
    (out/'metadata.json').write_text(json.dumps(metadata,indent=2))
    clean={k:os.environ[k] for k in ('PATH','HOME')}
    clean['RECEIPT_SELF_CI_TOKEN']=os.environ['RECEIPT_SELF_CI_TOKEN']
    with tempfile.TemporaryDirectory(prefix='receipt-net-',dir=SOURCE/'.tools') as temporary:
        work=Path(temporary)
        clients={}
        for name,fragment in [('network','network_probe.go.inc'),('dns','network_dns.go.inc'),('tls','network_tls.go.inc')]:
            directory=work/name;directory.mkdir();source=directory/'probe.go';source.write_text((SOURCE/'scripts/profile'/fragment).read_text())
            run(['gofmt','-w',str(source)]);binary=directory/'probe'
            run(['go','build','-trimpath','-buildvcs=false','-o',str(binary),str(source)],cwd=SOURCE)
            clients[name]=binary
        # DNS driver writes only resolver settings and fixed numeric event records.
        driver=clients['dns'].parent/'run_probe.py';driver.write_text((SOURCE/'scripts/profile/network_dns.py').read_text())
        run(['python3',str(driver)])
        shutil.copy(driver.parent/'linux-probe.json',out/'dns.json')
        tcp=run(['python3',str(SOURCE/'scripts/profile/network_tcp.py')]);(out/'tcp.json').write_text(tcp.stdout)
        route=json.loads(run(['ip','-json','route','get','104.248.183.112']).stdout)[0]
        interface=route['dev']
        packets=[]
        tcpdump=subprocess.Popen(['sudo','-n','tcpdump','-i',interface,'-nn','-tt','-l','-s','96','host 104.248.183.112 and tcp port 443'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,start_new_session=True)
        def receive():
            for line in tcpdump.stdout:
                record=packet_record(line)
                if record is not None and len(packets)<30000:packets.append(record)
        reader=threading.Thread(target=receive,daemon=True);reader.start();time.sleep(.3)
        if tcpdump.poll() is not None:raise RuntimeError('TCP header observer did not start')
        rows=[]
        for block in range(12):
            cases=[('fresh','fetch',False),('preload','fetch',False),('pinned','fetch',False),('fresh','status',False),('fresh','fetch',True)]
            cases=cases[block%len(cases):]+cases[:block%len(cases)]
            for mode,endpoint,h1 in cases:
                args=[str(clients['network']),'-mode',mode,'-endpoint',endpoint,'-key',fixture]
                if h1:args+=['-h1']
                row=json.loads(run(args,env=clean).stdout);row.update(block=block,http1_control=h1)
                if row['failed'] or row['trace']['status']!=(200 if endpoint=='fetch' else 204):raise RuntimeError('expected read-only hit')
                rows.append(row)
        for mode in ('fresh','resume','reuse'):
            values=run([str(clients['network']),'-mode',mode,'-count','20','-key',fixture],env=clean)
            for line in values.stdout.splitlines():
                row=json.loads(line);row.update(block=12,http1_control=False,persistent_control=True)
                if row['failed']:raise RuntimeError('persistent control failed')
                rows.append(row)
        run(['sudo','-n','kill','-INT','--','-'+str(tcpdump.pid)])
        tcpdump.wait(timeout=10);reader.join(timeout=5)
        if reader.is_alive():raise RuntimeError('packet observer did not stop')
        (out/'tcp-packets.json').write_text(json.dumps({'scope':'TCP header text only, one public endpoint, snaplen96. No packet bodies retained. GRO/GSO can combine segments. Capture perturbs timing.','rows':packets},indent=2))
        fetches=[r for r in rows if r['endpoint']=='fetch']
        if len({r['decoded_sha256'] for r in fetches})!=1 or len({r['trace']['body_sha256'] for r in fetches})!=1:
            raise RuntimeError('decoded or body identity drift')
        (out/'requests.json').write_text(json.dumps(rows,indent=2))
        tls=[]
        public_env={k:v for k,v in clean.items() if k!='RECEIPT_SELF_CI_TOKEN'}
        for block in range(12):
            for preload in (False,True):
                args=[str(clients['tls']),'-n','1']
                if preload:args+=['-preload']
                for line in run(args,env=public_env).stdout.splitlines():tls.append(dict(json.loads(line),block=block,preload_control=preload))
            for line in run([str(clients['tls']),'-mode','roots'],env=public_env).stdout.splitlines():tls.append(dict(json.loads(line),block=block,root_only=True))
        for mode in ('full','resume','reuse'):
            for line in run([str(clients['tls']),'-mode',mode,'-n','20'],env=public_env).stdout.splitlines():tls.append(dict(json.loads(line),persistent_control=mode))
        if any(r.get('failed') for r in tls):raise RuntimeError('public TLS diagnostic failed')
        (out/'tls.json').write_text(json.dumps(tls,indent=2))
        # Separate perturbed CPU profile, authenticated identity/payload never involved.
        cpu=out/'tls-cpu.pprof'
        run([str(clients['tls']),'-mode','full','-n','40','-cpu-profile',str(cpu)],env=public_env)
        (out/'tls-cpu.txt').write_text(run(['go','tool','pprof','-top','-nodecount=40',str(clients['tls']),str(cpu)]).stdout)
        metadata['probe_sha256']={name:hashlib.sha256(path.read_bytes()).hexdigest() for name,path in clients.items()}
    metadata['completed']=True;metadata['ended_unix_ns']=time.time_ns()
    (out/'metadata.json').write_text(json.dumps(metadata,indent=2))


if __name__=='__main__':
    main()
