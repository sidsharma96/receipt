#!/usr/bin/env python3
"""Join actual Go/Caddy/Receipt requests and bracket server resource windows."""
import hashlib
import json
from pathlib import Path
import sys
from summarize_gzip import stats,span


def resources(samples,events,all_events):
    # Caddy timestamps are server-local log completion times. Never subtract CI clocks.
    start=min(e['ts']-e['duration'] for e in events)-1
    end=max(e['ts'] for e in events)+1
    before=[s for s in samples if s['unix_ns']/1e9<=start]
    after=[s for s in samples if s['unix_ns']/1e9>=end]
    if not before or not after: raise ValueError('resource capture does not bracket requests')
    left=before[-1];right=after[0]
    selected=[s for s in samples if left['unix_ns']<=s['unix_ns']<=right['unix_ns']]
    result={}
    for unit in ('caddy','receipt','postgresql@16-main'):
        a=left['services'][unit];b=right['services'][unit]
        if (a['pid'],a['start_ticks'])!=(b['pid'],b['start_ticks']) or b['cpu_us']<a['cpu_us']:
            raise ValueError('server process or resource counter changed')
        result[unit]={'cpu_ms':(b['cpu_us']-a['cpu_us'])/1000,
                      'cpu_ms_per_measured_hit':(b['cpu_us']-a['cpu_us'])/1000/len(events),
                      'sampled_max_rss_mb':max(s['services'][unit]['rss_kib'] for s in selected)*1024/1e6,
                      'sampled_max_cgroup_mb':max(s['services'][unit]['cgroup_bytes'] for s in selected)/1e6,
                      'rss_before_mb':a['rss_kib']*1024/1e6,'rss_after_mb':b['rss_kib']*1024/1e6}
    ids={e['request_id'] for e in events}
    unmatched=[e for e in all_events if left['unix_ns']/1e9<=e['ts']<=right['unix_ns']/1e9 and e['request_id'] not in ids]
    return {'start_ns':left['unix_ns'],'end_ns':right['unix_ns'],'samples':len(selected),
            'unmatched_caddy_requests':len(unmatched),'services':result,
            'scope':'Matched request envelope plus one-second padding, rounded outward to 100ms samples. CPU includes background work/traffic and TLS; not an isolated compression CPU cost. RSS is sampled and shared across variants in long-lived processes.'}


def summarize(client,server):
    if not client['completed'] or len(set(client['keys'].values()))!=1 or len(client['rows'])!=180:
        raise ValueError('incomplete experiment or incompatible keys')
    if server['metadata']['original_sha256']!=server['restoration']['restored_sha256'] or server['restoration']['caddy_active']!='active':
        raise ValueError('original Caddy configuration not restored')
    for unit in ('caddy','receipt'):
        if server['final_binary_sha256'][unit]!=server['metadata'][unit+'_binary_sha256']:
            raise ValueError('deployed binary changed')
    caddy={e['request_id']:e for e in server['caddy']}
    receipt={e['request_id']:e for e in server['receipt']}
    joined=[]
    for row in client['rows']:
        if row['returncode'] or not row['hit'] or row['timing']['failed'] or any(s['stage']=='execution' for s in row['timing']['stages']):
            raise ValueError('not a successful hit')
        if len(row['http'])!=1: raise ValueError('unexpected request count')
        trace=row['http'][0];case=row['case']
        if case not in ('identity','gzip') or trace['status']!=200 or trace['reused'] or trace['protocol']!='HTTP/2.0' or trace['uncompressed']!=(case=='gzip'):
            raise ValueError('unexpected response/encoding')
        edge=caddy[trace['request_id']];backend=receipt[trace['request_id']]
        if edge['status']!=200 or backend['status']!=200 or backend['operation']!='fetch' or edge['encoding']!=('gzip' if case=='gzip' else ''):
            raise ValueError('server response mismatch')
        if (edge['size']<trace['bytes'])!=(case=='gzip') or (case=='identity' and edge['size']!=trace['bytes']):
            raise ValueError('wrong encoded byte accounting')
        measured={'elapsed_ms':row['elapsed_ms'],'cpu_ms':row['cpu_ms'],'rss_mb':row['rss_mb'],
                  'encoded_bytes':edge['size'],'decoded_bytes':trace['bytes'],'caddy_ms':edge['duration']*1000,
                  'receipt_ms':backend['duration_ns']/1e6,'dns_ms':span(trace,'dns_start','dns_done'),
                  'tcp_ms':span(trace,'connect_start','connect_done'),
                  'tls_ms':span(trace,'tls_start','tls_done'),'first_byte_wait_ms':span(trace,'request_written','first_response_byte'),
                  'body_read_ms':span(trace,'body_read_start','body_read_done'),'decode_ms':span(trace,'decode_start','decode_done'),
                  'http_ms':span(trace,'request_start','body_closed')}
        measured['lookup_outside_http_ms']=sum(s['ns'] for s in row['timing']['stages'] if s['stage']=='lookup')/1e6-measured['http_ms']
        joined.append({'case':case,'block':row['block'],'i':row['i'],'request_id':trace['request_id'],
                       'decoded_sha256':trace['decoded_sha256'],'stdout_sha256':row['stdout_sha256'],
                       'metrics':measured,'caddy':edge,'receipt':backend})
    if len({r['decoded_sha256'] for r in joined})!=1 or len({r['stdout_sha256'] for r in joined})!=1 or len({r['metrics']['decoded_bytes'] for r in joined})!=1:
        raise ValueError('decoded result or replay drift')
    batches=[];by_case={}
    for case in ('identity','gzip'):
        selected=[r for r in joined if r['case']==case]
        if len(selected)!=90: raise ValueError('missing calls')
        by_case[case]={name:stats([r['metrics'][name] for r in selected]) for name in selected[0]['metrics']}
        for block in range(3):
            rows=[r for r in selected if r['block']==block]
            if len(rows)!=30 or {r['i'] for r in rows}!=set(range(30)): raise ValueError('missing block coverage')
            batches.append({'case':case,'block':block,'calls':len(rows),
                            'latency_ms':stats([r['metrics']['elapsed_ms'] for r in rows]),
                            'resource_window':resources(server['samples'],[r['caddy'] for r in rows],server['caddy'])})
    return {'run_id':client['run_id'],'source_commit':client['plan']['source_commit'],'calls':len(joined),
            'decoded_sha256':joined[0]['decoded_sha256'],'decoded_bytes':joined[0]['metrics']['decoded_bytes'],
            'variants':by_case,'batches':batches,'matched':joined,
            'server_metadata':server['metadata'],'restoration':server['restoration'],
            'limitations':'One Internet runner, batched variant order rather than randomized adjacent calls. Server windows include background work. No packet trace. No independent per-variant server RSS peaks; same long-lived processes and pools. HTTP body bytes exclude transport headers.'}


def main():
    raw,out=map(Path,sys.argv[1:]);out.mkdir(parents=True,exist_ok=False)
    source=raw/'client/results.json';server_source=raw/'server.json'
    report=summarize(json.loads(source.read_text()),json.loads(server_source.read_text()))
    report['client_sha256']=hashlib.sha256(source.read_bytes()).hexdigest()
    report['server_sha256']=hashlib.sha256(server_source.read_bytes()).hexdigest()
    (out/'client.json').write_bytes(source.read_bytes());(out/'server.json').write_bytes(server_source.read_bytes())
    (out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({k:report[k] for k in ('variants','batches')},indent=2))

if __name__=='__main__': main()
