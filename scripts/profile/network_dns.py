#!/usr/bin/env python3
"""Bounded DNS-only experiment. Output stays beside this script. No credentials used."""
import argparse, json, os, pathlib, platform, subprocess, time
p=argparse.ArgumentParser();p.add_argument('--local-smoke',action='store_true');a=p.parse_args()
here=pathlib.Path(__file__).resolve().parent
version=subprocess.check_output(['go','version'],text=True).strip()
if not a.local_smoke and ('go1.22.12 linux/amd64' not in version):raise SystemExit('Requires pinned Go1.22.12 linux/amd64; --local-smoke labels a different host explicitly')
# Never print environment values. Use a clean environment for this standalone probe.
env={k:v for k,v in os.environ.items() if k in ('PATH','HOME','TMPDIR','SystemRoot')};env['GOTOOLCHAIN']='local';env['CGO_ENABLED']='0'
subprocess.run(['go','build','-o',str(here/'probe'),str(here/'probe.go')],env=None if a.local_smoke else env,check=True,timeout=60)
meta={'version':version,'platform':platform.system(),'resolver_environment_presence':{k:k in os.environ for k in ('LOCALDOMAIN','RES_OPTIONS','HOSTALIASES')},'scope':'Standalone DNS probe; no Receipt fingerprint or HTTP. Clean env. OS/upstream caches remain warm.'}
if platform.system()=='Linux':
 config=pathlib.Path('/etc/resolv.conf');meta['resolv_conf_target']=str(config.resolve());lines=config.read_text().splitlines();meta['resolver_settings']=[l for l in lines if l.split() and l.split()[0] in ('nameserver','options')];meta['search_suffix_count']=sum(len(l.split())-1 for l in lines if l.startswith('search '))
 meta['hosts_nss']=[l for l in pathlib.Path('/etc/nsswitch.conf').read_text().splitlines() if l.startswith('hosts:')]
 for tool,args in [('resolvectl',['--version']),('getconf',['GNU_LIBC_VERSION'])]:
  try:meta[tool]=subprocess.check_output([tool]+args,text=True,timeout=5).splitlines()[0]
  except (OSError,subprocess.SubprocessError):meta[tool]='unavailable'
rows=[];configs=[('default','ip',False),('instrumented-go','ip',False)]
if not a.local_smoke:configs += [('instrumented-go','ip4',False),('instrumented-go','ip6',False),('instrumented-go','ip',True)]
# Rotating order reduces simple order bias; 20 local or 60 CI processes, each <=8s DNS deadline.
for block in range(10 if a.local_smoke else 12):
 order=configs[block%len(configs):]+configs[:block%len(configs)]
 for mode,family,rooted in order:
  e=dict(env,GODEBUG='netdns=2');cmd=[str(here/'probe'),'-mode',mode,'-family',family]
  if rooted:cmd+=['-rooted']
  r=subprocess.run(cmd,env=e,text=True,capture_output=True,timeout=12)
  if r.returncode:raise SystemExit('probe failed; no raw diagnostic output retained')
  row=json.loads(r.stdout);row['block']=block
  row['resolver_diagnostics']=[line for line in r.stderr.splitlines() if line.startswith('go package net:')]
  rows.append(row);time.sleep(.05)
(here/('local-probe.json' if a.local_smoke else 'linux-probe.json')).write_text(json.dumps({'metadata':meta,'rows':rows},indent=2)+'\n')
print(json.dumps({'calls':len(rows),'failed':sum(r['failed'] for r in rows),'version':version}))
