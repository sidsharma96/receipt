#!/usr/bin/env python3
"""Validate and curate numeric HTTP trace artifacts and exactly matched server logs."""
import hashlib
import json
from pathlib import Path
import statistics
import sys
from linux_ci_baseline import distributions, percentile


def stats(values):
    return {'n':len(values),'mean':statistics.fmean(values),'median':statistics.median(values),
            'p95':percentile(values,.95),'min':min(values),'max':max(values)}


def phases(trace):
    events = trace['events']
    if len(events) >= 64 or any(b['ns'] < a['ns'] for a,b in zip(events,events[1:])):
        raise ValueError('truncated or nonmonotonic trace')
    def point(name):
        found = [e['ns'] for e in events if e['name'] == name]
        if len(found) != 1:
            raise ValueError('expected exactly one '+name+'; review retries/overlap before attribution')
        return found[0]
    pairs = {'dns_ms':('dns_start','dns_done'), 'tcp_ms':('connect_start','connect_done'),
             'tls_ms':('tls_start','tls_done'), 'write_to_first_byte_ms':('request_written','first_response_byte'),
             'first_byte_to_headers_ms':('first_response_byte','headers_returned'),
             'http_through_close_ms':('request_start','body_closed')}
    if trace['kind'] == 'entries':
        pairs.update(body_read_ms=('body_read_start','body_read_done'),decode_ms=('decode_start','decode_done'))
    result = {name:(point(end)-point(start))/1e6 for name,(start,end) in pairs.items()}
    if any(v < 0 for v in result.values()):
        raise ValueError('overlapping/reordered boundary requires explicit analysis')
    return result


def summarize(client, logs):
    if not client['completed'] or len(set(client['keys'].values())) != 1:
        raise ValueError('incomplete experiment or incompatible keys')
    rows = client['rows']
    if len(rows) != 120 or len({r['stdout_sha256'] for r in rows}) != 1:
        raise ValueError('missing calls or changed replay')
    index = {e['request_id']:e for e in logs['events']}
    matched = []
    for row in rows:
        if row['returncode'] or not row['hit'] or row['timing']['failed'] or any(s['stage']=='execution' for s in row['timing']['stages']):
            raise ValueError('not a successful hit')
        if row['case'].startswith('traced-'):
            if len(row['http']) != 1:
                raise ValueError('unexpected trace count')
            trace = row['http'][0]
            server = index[trace['request_id']]
            if server['status'] != trace['status'] or server['operation'] != ('fetch' if trace['kind']=='entries' else 'status'):
                raise ValueError('mismatched server event')
            measured = phases(trace)
            lookup = sum(s['ns'] for s in row['timing']['stages'] if s['stage']=='lookup')/1e6
            measured.update(server_handler_ms=server['duration_ns']/1e6,
                            lookup_outside_http_ms=lookup-measured['http_through_close_ms'])
            matched.append({'case':row['case'],'block':row['block'],'i':row['i'],
                            'request_id':trace['request_id'],'phases':measured,'server':server})
    by_case = {}
    examples = {}
    for case in ('checked-local','remote-fetch'):
        selected = [r for r in matched if r['case']=='traced-'+case]
        if len(selected) != 30:
            raise ValueError('trace count drift')
        by_case[case] = {name:stats([r['phases'][name] for r in selected]) for name in selected[0]['phases']}
        median = by_case[case]['http_through_close_ms']['median']
        example = min(selected,key=lambda r:abs(r['phases']['http_through_close_ms']-median))
        source = next(r for r in rows if r['case']==example['case'] and r['block']==example['block'] and r['i']==example['i'])
        examples[case] = {'client':source,'match':example}
    comparison = {}
    for case in ('checked-local','remote-fetch'):
        stock = {(r['block'],r['i']):r for r in rows if r['case']=='stock-'+case}
        traced = {(r['block'],r['i']):r for r in rows if r['case']=='traced-'+case}
        if set(stock) != set(traced) or len(stock)!=30:
            raise ValueError('unpaired calls')
        comparison[case] = {name:stats([traced[k][name]-stock[k][name] for k in stock])
                            for name in ('elapsed_ms','cpu_ms','rss_mb')}
    return {'run_id':client['run_id'],'source_commit':client['source_commit'],'calls':len(rows),'matched_calls':len(matched),
            'summary':distributions(rows),'phases':by_case,'paired_traced_minus_stock':comparison,
            'examples':examples,'matched':matched,
            'limitations':'Sequential warm hits on one runner; fresh CLI processes. Phase medians are not additive. Server time overlaps response transfer; never sum it with the client phases. Comparison includes path variability as well as diagnostic overhead. Body bytes are returned by net/http, not packet capture.'}


def main():
    raw,out = map(Path,sys.argv[1:])
    out.mkdir(parents=True,exist_ok=False)
    client_path = raw/'client/results.json'
    client = json.loads(client_path.read_text())
    summary = summarize(client,json.loads((raw/'server-logs.json').read_text()))
    before,after = [json.loads((raw/('server-'+n+'.json')).read_text()) for n in ('before','after')]
    for name in ('pid','start_ticks','binary_sha256'):
        if before[name] != after[name]:
            raise ValueError('server changed')
    summary['server_identity'] = {name:before[name] for name in ('pid','start_ticks','binary_sha256')}
    summary['client_sha256'] = hashlib.sha256(client_path.read_bytes()).hexdigest()
    (out/'client.json').write_bytes(client_path.read_bytes())
    (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
    print(json.dumps({'phases':summary['phases'],'comparison':summary['paired_traced_minus_stock']},indent=2))

if __name__ == '__main__':
    main()
