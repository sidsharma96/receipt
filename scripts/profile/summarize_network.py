#!/usr/bin/env python3
"""Derive per-request stages, kernel handshake intervals and public TLS controls."""
import json
from pathlib import Path
import statistics
import sys


def stats(values):
    return {'n':len(values),'median':statistics.median(values),'mean':statistics.fmean(values),'min':min(values),'max':max(values)} if values else None


def events(row,name):return [e for e in row['trace']['events'] if e['name']==name]


def stages(row):
    pairs={'dns':('dns_start','dns_done'),'tcp':('connect_start','connect_done'),'tls':('tls_start','tls_done'),'first_byte':('request_written','first_response_byte'),'operation':('operation_start','operation_done')}
    result={}
    for name,(start,end) in pairs.items():
        a,b=events(row,start),events(row,end)
        if a and b:
            result[name+'_ms']=(b[-1]['ns']-a[0]['ns'])/1e6
            result[name+'_cpu_ms']=(b[-1]['cpu_us']-a[0]['cpu_us'])/1000
    reads,done=events(row,'body_read_start'),events(row,'body_read_done')
    if reads and done:
        result['body_ms']=(done[-1]['ns']-reads[0]['ns'])/1e6
        result['body_cpu_ms']=(done[-1]['cpu_us']-reads[0]['cpu_us'])/1000
        close=events(row,'body_close_start')[0]
        result['decode_gap_ms']=(close['ns']-done[-1]['ns'])/1e6
        result['decode_gap_cpu_ms']=(close['cpu_us']-done[-1]['cpu_us'])/1000
    result['root_load_ms']=row['root_load_ms']
    result['including_root_ms']=result['operation_ms']+row['root_load_ms']
    return result


def summarize(directory):
    directory=Path(directory)
    rows=json.loads((directory/'requests.json').read_text())
    server={r['request_id']:r for r in json.loads((directory/'server.json').read_text())['rows']} if (directory/'server.json').exists() else {}
    packets=json.loads((directory/'tcp-packets.json').read_text())['rows']
    syns=[p for p in packets if not p['incoming'] and p['flags']=='S']
    result=[]
    for row in rows:
        if row['failed']:raise ValueError('failed request')
        record={k:row.get(k,False) for k in ['mode','index','endpoint','block','http1_control','persistent_control']}
        record['metrics']=stages(row)
        record['resumed']=row['trace']['resumed'];record['reused']=row['trace']['reused']
        record['request_id']=row['trace']['request_id'];record['body_bytes']=row['trace']['body_bytes']
        if record['request_id'] in server:
            sr=server[record['request_id']]
            record['server']=sr
            record['metrics']['handler_ms']=sr['duration_ns']/1e6
            record['metrics']['first_byte_minus_handler_ms']=record['metrics']['first_byte_ms']-record['metrics']['handler_ms']
        connect=events(row,'connect_start')
        if connect:
            target=row['start_unix_ns']+connect[0]['ns']
            syn=min(syns,key=lambda p:abs(p['unix_ns']-target))
            if abs(syn['unix_ns']-target)>5e6:raise ValueError('no nearby SYN')
            replies=[p for p in packets if p['incoming'] and p['client_port']==syn['client_port'] and 'S' in p['flags'] and p['unix_ns']>=syn['unix_ns']]
            if not replies:raise ValueError('no SYN-ACK')
            record['client_port']=syn['client_port']
            record['server_initial_seq']=replies[0].get('seq')
            record['metrics']['wire_handshake_ms']=(replies[0]['unix_ns']-syn['unix_ns'])/1e6
            record['metrics']['connect_minus_wire_ms']=record['metrics']['tcp_ms']-record['metrics']['wire_handshake_ms']
        result.append(record)
    groups={}
    for record in result:
        name='/'.join([str(record[k]) for k in ['persistent_control','mode','endpoint','http1_control']])
        groups.setdefault(name,[]).append(record)
    group_stats={}
    for name,selected in groups.items():
        # First persistent-control request is cold; summarize warmed rows separately.
        use=[r for r in selected if not r['persistent_control'] or r['index']>0]
        keys=sorted({k for r in use for k in r['metrics']})
        group_stats[name]={'n':len(use),'resumed':sum(r['resumed'] for r in use),'reused':sum(r['reused'] for r in use),
                           'metrics':{key:stats([r['metrics'][key] for r in use if key in r['metrics']]) for key in keys}}
    dns=json.loads((directory/'dns.json').read_text());dns_groups={}
    for r in dns['rows']:
        name='/'.join([r['mode'],r['family'],str(r['rooted'])]);dns_groups.setdefault(name,[]).append(r)
    dns_stats={name:{'duration_ms':stats([(r['end_ns']-r['start_ns'])/1e6 for r in rr]),'no_answer_or_error':sum(r['failed'] for r in rr)} for name,rr in dns_groups.items()}
    tls=json.loads((directory/'tls.json').read_text());tls_groups={}
    for r in tls:
        if r['kind']=='request' and 'block' in r:
            name=str(r['preload_control']);tls_groups.setdefault(name,[]).append(r['tls_cost'])
    tls_stats={name:{k:stats([r[k] for r in rr]) for k in ['wall_ms','process_cpu_ms','allocated_bytes','allocations']} for name,rr in tls_groups.items()}
    roots=[r['cost'] for r in tls if r['kind']=='root_load' and r.get('root_only')]
    root_stats={k:stats([r[k] for r in roots]) for k in ['wall_ms','process_cpu_ms','allocated_bytes','allocations']}
    return {'metadata':json.loads((directory/'metadata.json').read_text()),'scope':'Request stages derive from each request before aggregation. Warm controls exclude their first cold request only. Paired residuals are not exact one-way network timers. Body excludes decode; roots preloading is included separately.','groups':group_stats,'rows':result,'dns':dns_stats,'public_tls':tls_stats,'root_only':root_stats,'matched_server_requests':sum('server'in r for r in result),'total_requests':len(result)}


if __name__=='__main__':print(json.dumps(summarize(sys.argv[1]),indent=2))
