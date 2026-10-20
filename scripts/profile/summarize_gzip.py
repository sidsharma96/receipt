#!/usr/bin/env python3
"""Validate and summarize the controlled gzip comparison, retaining every call."""
import hashlib
import json
from pathlib import Path
import statistics
import sys
from linux_ci_baseline import percentile


def stats(values):
    return {'n':len(values),'mean':statistics.fmean(values),'median':statistics.median(values),
            'p95':percentile(values,.95),'min':min(values),'max':max(values)}


def span(trace,start,end):
    def point(name):
        found=[e['ns'] for e in trace['events'] if e['name']==name]
        if len(found)!=1: raise ValueError('unsupported callback count: '+name)
        return found[0]
    value=(point(end)-point(start))/1e6
    if value<0: raise ValueError('negative span')
    return value


def summarize(client):
    if not client['completed'] or len(set(client['keys'].values()))!=1:
        raise ValueError('incomplete or incompatible keys')
    rows=client['rows']
    if len(rows)!=360 or len({r['stdout_sha256'] for r in rows})!=1:
        raise ValueError('call count or replay mismatch')
    hashes=set();sizes=set();summaries={}
    for row in rows:
        if row['returncode'] or not row['hit'] or row['timing']['failed'] or any(s['stage']=='execution' for s in row['timing']['stages']):
            raise ValueError('not a successful hit')
        if row['case'] not in ('identity','gzip') or len(row['http'])!=1:
            raise ValueError('unexpected case or trace')
        trace=row['http'][0];server=row['server'];compressed=row['case']=='gzip'
        if trace['status']!=200 or trace['uncompressed']!=compressed or trace['reused'] or trace['protocol']!='HTTP/2.0':
            raise ValueError('wrong HTTP response')
        if server['type']!='gzip_server' or server['gzip']!=compressed or server['failed'] or server['status']!=200:
            raise ValueError('wrong server response')
        if trace['bytes']!=server['decoded_bytes'] or trace['decoded_sha256']!=server['decoded_sha256']:
            raise ValueError('decoded result mismatch')
        if (server['encoded_bytes']<server['decoded_bytes'])!=compressed:
            raise ValueError('wrong encoded bytes')
        hashes.add(trace['decoded_sha256']);sizes.add(trace['bytes'])
    if len(hashes)!=1 or len(sizes)!=1: raise ValueError('payload drift')
    for delay in (0,12,32):
        group={};paired={};derived={}
        for case in ('identity','gzip'):
            selected=[r for r in rows if r['one_way_delay_ms']==delay and r['case']==case]
            expected={(b,i) for b in range(3) for i in range(20)}
            actual={(r['block'],r['i']) for r in selected}
            if len(selected)!=60 or actual!=expected: raise ValueError('missing planned pair')
            values=[]
            for row in selected:
                trace=row['http'][0]
                values.append({**{k:row[k] for k in ('elapsed_ms','cpu_ms','rss_mb','inblock','outblock')},
                  'encoded_bytes':row['server']['encoded_bytes'],'decoded_bytes':row['server']['decoded_bytes'],
                  'server_handler_ms':row['server']['handler_ns']/1e6,
                  'http_through_close_ms':span(trace,'request_start','body_closed'),
                  'lookup_outside_http_ms':sum(s['ns'] for s in row['timing']['stages'] if s['stage']=='lookup')/1e6-span(trace,'request_start','body_closed'),
                  'tcp_ms':span(trace,'connect_start','connect_done'),
                  'tls_ms':span(trace,'tls_start','tls_done'),
                  'first_byte_wait_ms':span(trace,'request_written','first_response_byte'),
                  'body_read_ms':span(trace,'body_read_start','body_read_done'),
                  'decode_ms':span(trace,'decode_start','decode_done')})
            batches=[r for r in client['server_batches'] if r['case']==case and r['one_way_delay_ms']==delay]
            if len(batches)!=3 or {r['block'] for r in batches}!={0,1,2}:
                raise ValueError('missing server batches')
            cpu=[];rss=[]
            for batch in batches:
                if batch['calls']!=20: raise ValueError('wrong server interval')
                delta=batch['after']['cpu_us']-batch['before']['cpu_us']
                if delta<0: raise ValueError('server resource clock reset')
                cpu.append(delta/20/1000)
                rss.append(batch['after']['max_rss_native']*1024/1e6)
            group[case]={name:stats([v[name] for v in values]) for name in values[0]}
            group[case]['server_cpu_ms_per_call']=stats(cpu)
            group[case]['server_peak_rss_mb']=stats(rss)
            derived[case]={(r['block'],r['i']):v for r,v in zip(selected,values)}
        for name in ('elapsed_ms','cpu_ms','rss_mb','first_byte_wait_ms','body_read_ms','decode_ms'):
            differences=[derived['gzip'][key][name]-derived['identity'][key][name] for key in sorted(derived['gzip'])]
            paired[name]={**stats(differences),'block_means':[statistics.fmean(derived['gzip'][(b,i)][name]-derived['identity'][(b,i)][name] for i in range(20)) for b in range(3)]}
        summaries[str(delay)]={'variants':group,'paired_gzip_minus_identity':paired}
    return {'run_id':client['run_id'],'source_commit':client['plan']['source_commit'],'calls':len(rows),
            'decoded_sha256':hashes.pop(),'decoded_bytes':sizes.pop(),'conditions':summaries,
            'scope':'One Linux runner, current Disk/Handler, private netem link, 0/12/32ms one-way delay. No packet loss/bandwidth cap. No live Caddy/PostgreSQL. Phase medians not additive. Body bytes exclude TLS/HTTP/TCP headers. Server RSS includes warmups; CPU excludes warmups and includes diagnostics/TLS. Fresh unpooled Go level-5 encoder per response.'}


def main():
    raw,out=map(Path,sys.argv[1:])
    out.mkdir(parents=True,exist_ok=False)
    source=raw/'results.json'
    client=json.loads(source.read_text())
    summary=summarize(client)
    summary['client_sha256']=hashlib.sha256(source.read_bytes()).hexdigest()
    (out/'client.json').write_bytes(source.read_bytes())
    (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
    for delay,condition in summary['conditions'].items():
        print('one-way delay',delay)
        for case,v in condition['variants'].items():
            print(case,{name:round(v[name]['median' if name=='elapsed_ms' else 'mean'],4) for name in ('elapsed_ms','cpu_ms','rss_mb','encoded_bytes','server_cpu_ms_per_call','server_peak_rss_mb','body_read_ms','first_byte_wait_ms','decode_ms')})
        print('paired elapsed',condition['paired_gzip_minus_identity']['elapsed_ms'])

if __name__=='__main__': main()
