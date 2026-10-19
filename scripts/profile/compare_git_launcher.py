#!/usr/bin/env python3
"""macOS diagnostic: compare Apple Git launcher and selected Git with Trace2.
Usage: compare_git_launcher.py EXISTING_REPOSITORY NEW_OUTPUT_DIRECTORY
Keeps all 60 pairs per command; does not change Git configuration or PATH.
"""
import subprocess,os,time,json,random,statistics,resource,sys
from pathlib import Path
out=Path(sys.argv[2]).resolve()
out.mkdir(parents=True,exist_ok=False)
root=Path(sys.argv[1]).resolve()
direct=subprocess.run(['xcrun','--find','git'],capture_output=True,text=True,check=True).stdout.strip()
programs={'launcher':'/usr/bin/git','direct':direct}
commands={'root':['rev-parse','--show-toplevel'],'listing':['ls-files','-z','--cached','--others','--exclude-standard']}
schedule=[]
rng=random.Random(20261018)
for command in commands:
 orders=[['launcher','direct']]*30+[['direct','launcher']]*30
 rng.shuffle(orders)
 schedule.extend((command,order) for order in orders)
rng.shuffle(schedule)
rows=[]
# Both programs receive the same trace variable value. A fresh file is removed between calls.
trace=out/'current.json'
env=dict(os.environ,GIT_TRACE2_EVENT=str(trace))
for c in commands:
 for label in programs:
  for _ in range(3):
   if trace.exists():trace.unlink()
   subprocess.run([programs[label]]+commands[c],cwd=root,env=env,capture_output=True,check=True)
for i,(command,order) in enumerate(schedule):
 pair=[]
 for label in order:
  if trace.exists():trace.unlink()
  usage=resource.getrusage(resource.RUSAGE_CHILDREN)
  start=time.perf_counter_ns()
  p=subprocess.run([programs[label]]+commands[command],cwd=root,env=env,capture_output=True,check=True)
  elapsed=(time.perf_counter_ns()-start)/1e6
  after=resource.getrusage(resource.RUSAGE_CHILDREN)
  events=[json.loads(l) for l in trace.read_text().splitlines()]
  end=next(e for e in reversed(events) if e['event']=='atexit')
  pair.append({'label':label,'elapsed_ms':elapsed,'child_cpu_ms':1000*(after.ru_utime-usage.ru_utime+after.ru_stime-usage.ru_stime),
   'git_reported_ms':end['t_abs']*1000,'outside_git_reported_ms':elapsed-end['t_abs']*1000,
   'trace_events':events,'output_sha256':__import__('hashlib').sha256(p.stdout).hexdigest()})
 assert pair[0]['output_sha256']==pair[1]['output_sha256']
 rows.append({'i':i,'command':command,'order':order,'calls':pair})
(out/'comparison.json').write_text(json.dumps({'counts':'60 pairs per command, 240 measured calls, 12 excluded warmups','programs':programs,'rows':rows},indent=2))
for command in commands:
 for label in programs:
  rs=[r for p in rows if p['command']==command for r in p['calls'] if r['label']==label]
  print(command,label,{k:{'median':statistics.median(r[k] for r in rs),'max':max(r[k] for r in rs)} for k in ['elapsed_ms','child_cpu_ms','git_reported_ms','outside_git_reported_ms']})
