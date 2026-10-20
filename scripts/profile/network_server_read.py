#!/usr/bin/env python3
"""Read-only operator export: fixed Receipt timing records and executable identity."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys


def main():
    start,end=map(int,sys.argv[1:])
    if not 0<end-start<=900:raise RuntimeError('bounded interval required')
    raw=subprocess.check_output(['journalctl','-u','receipt','--since','@'+str(start),'--until','@'+str(end),'-o','json','--no-pager'],text=True,timeout=30)
    rows=[]
    for line in raw.splitlines():
        entry=json.loads(line)
        try:event=json.loads(entry.get('MESSAGE',''))
        except (TypeError,ValueError):continue
        if isinstance(event,dict) and set(event)=={'version','request_id','operation','status','duration_ns','stages'} and re.fullmatch('[a-f0-9]{32}-[a-f0-9]{1,16}',event['request_id']):rows.append(event)
    identity={name:subprocess.check_output(['systemctl','show',name,'-p','MainPID','-p','ActiveState'],text=True,timeout=10).splitlines() for name in ['receipt','caddy']}
    print(json.dumps({'rows':rows,'services':identity,'receipt_binary_sha256':hashlib.sha256(Path('/usr/local/bin/receipt').read_bytes()).hexdigest(),'scope':'Read-only fixed-field journal export; intervals overlap client waiting and receiving.'}))


if __name__=='__main__':main()
