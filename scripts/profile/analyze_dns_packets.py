#!/usr/bin/env python3
"""Pair selected DNS metadata without retaining domain payloads or private data."""
import json
from pathlib import Path
import statistics
import sys


def summarize(rows):
    unique=[];last={}
    for row in sorted(rows,key=lambda r:float(r['frame.time_epoch'])):
        key=tuple(sorted((k,v) for k,v in row.items() if k!='frame.time_epoch'))
        stamp=round(float(row['frame.time_epoch'])*1e9)
        if key in last and stamp-last[key]<50000:continue
        last[key]=stamp;unique.append(row)
    paired=[]
    for request in unique:
        if request['dns.flags.response']!='False':continue
        candidates=[r for r in unique if r['dns.flags.response']=='True' and r['dns.id']==request['dns.id'] and r['dns.qry.type']==request['dns.qry.type'] and r['ip.src']==request['ip.dst'] and r['ip.dst']==request['ip.src'] and r['udp.srcport']==request['udp.dstport'] and r['udp.dstport']==request['udp.srcport'] and float(r['frame.time_epoch'])>=float(request['frame.time_epoch'])]
        if not candidates:continue
        response=min(candidates,key=lambda r:float(r['frame.time_epoch']))
        paired.append({'start_unix_ns':round(float(request['frame.time_epoch'])*1e9),'qtype':int(request['dns.qry.type']),
                       'leg':'stub' if request['ip.dst']=='127.0.0.53' else 'upstream',
                       'elapsed_ms':1000*(float(response['frame.time_epoch'])-float(request['frame.time_epoch'])),
                       'rcode':response['dns.flags.rcode'],'answers':response['dns.count.answers'],'ttl':response['dns.resp.ttl']})
    groups={}
    for leg,qtype in [('stub',1),('stub',28),('upstream',1),('upstream',28)]:
        values=[r['elapsed_ms'] for r in paired if r['leg']==leg and r['qtype']==qtype]
        if values:groups[leg+'/'+str(qtype)]={'n':len(values),'median_ms':statistics.median(values),'max_ms':max(values),'over50':sum(v>50 for v in values)}
    return {'scope':'Only public endpoint DNS metadata. Identical any-interface observer records within50us collapsed for counting, not classified as retries. Missing packets/queries remain possible; pairing covers captured packets only. Upstream timings include network and resolver service.','raw_rows':len(rows),'unique_rows':len(unique),'pairs':paired,'groups':groups}


if __name__=='__main__':print(json.dumps(summarize(json.loads(Path(sys.argv[1]).read_text())['rows']),indent=2))
