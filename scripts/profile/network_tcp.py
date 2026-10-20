#!/usr/bin/env python3
"""Bounded public, credential-free connect probe; also usable on a Linux CI runner."""
import datetime,json,platform,socket,struct,time,sys
HOST='127.0.0.1' if sys.argv[1:]==['--loopback-validation'] else 'receipt.sidsharma.dev'
rows=[]
start=time.monotonic_ns()
addrs=socket.getaddrinfo(HOST,443,type=socket.SOCK_STREAM)
dns_ms=(time.monotonic_ns()-start)/1e6
unique=list(dict.fromkeys((a[0],a[4][0]) for a in addrs))
for family,address in unique[:4]:
 for i in range(6):
  s=socket.socket(family,socket.SOCK_STREAM);s.settimeout(3)
  r={'family': 'ipv4' if family==socket.AF_INET else 'ipv6','destination':address,'i':i}
  t=time.monotonic_ns()
  try:
   s.connect((address,443));r['connect_ms']=(time.monotonic_ns()-t)/1e6;r['ok']=True
   if platform.system()=='Linux':
    info=s.getsockopt(socket.IPPROTO_TCP,socket.TCP_INFO,256)
    r['tcp_info_len']=len(info)
    r['tcp_info']={name:struct.unpack_from('=I',info,offset)[0] for name,offset in [('rto_us',8),('snd_mss',16),('rcv_mss',20),('unacked',24),('lost',32),('retrans',36),('rtt_us',68),('rttvar_us',72),('snd_cwnd',80),('total_retrans',100),('min_rtt_us',148)] if len(info)>=offset+4}
  except OSError as e:r.update(ok=False,error_class=type(e).__name__,errno=e.errno)
  finally:s.close()
  rows.append(r)
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'system':platform.system(),'release':platform.release(),'machine':platform.machine(),'scope':'TCP connect only; no TLS, HTTP, credentials or payload; sequential fresh sockets','dns_ms':dns_ms,'rows':rows},indent=2))
