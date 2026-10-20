#!/usr/bin/env python3
"""Project kernel TCP metadata and match sockets across NAT using SYN-ACK ISNs."""
import json
from pathlib import Path
import re
import sys


def parse(text):
    rows=[]
    for line in text.splitlines():
        match=re.search(r' ([0-9]+\.[0-9]+): tcp:tcp_probe: .* dest=\S+:(\d+) .*',line)
        if not match:continue
        values={k:int(v,16 if v.startswith('0x') or k=='sock_cookie' else 10) for k,v in re.findall(r'(data_len|snd_nxt|snd_una|snd_cwnd|ssthresh|snd_wnd|srtt|rcv_wnd|sock_cookie)=(0x[0-9a-f]+|[0-9a-f]+)',line)}
        rows.append(dict(monotonic_ns=round(float(match[1])*1e9),client_port=int(match[2]),**values))
    return rows


def match(rows,requests):
    matched=[]
    for request in requests:
        if 'server_initial_seq' not in request:continue
        initial=(request['server_initial_seq']+1)%2**32
        cookies={r['sock_cookie'] for r in rows if r['snd_nxt']==r['snd_una']==initial}
        if len(cookies)!=1:continue
        cookie=next(iter(cookies))
        matched.append({'request':request,'kernel':[r for r in rows if r['sock_cookie']==cookie]})
    return {'scope':'Matched server socket by SYN-ACK initial sequence plus one, equal to initial snd_una/snd_nxt. Ports alone fail across runner NAT. This identifies a connection, not a request on a reused connection. tcp_probe observes state when processing incoming traffic, not every send.','matched_connections':len(matched),'rows':matched}


if __name__=='__main__':
    kernel=parse(Path(sys.argv[1]).read_text())
    print(json.dumps(match(kernel,json.loads(Path(sys.argv[2]).read_text())['rows']),indent=2))
