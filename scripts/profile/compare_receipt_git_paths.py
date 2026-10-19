#!/usr/bin/env python3
"""Compare diagnostic Receipt replays through Apple launcher and selected Git.
Usage: compare_receipt_git_paths.py SOURCE FIXTURE NEW_OUTPUT_DIRECTORY
Fixed 500 pairs; balanced randomized order; no production/PATH changes.
"""
import sys,subprocess,json,os,random,time,re,resource
from pathlib import Path
from capture_git_outlier import build,run
out=Path(sys.argv[3]).resolve();out.mkdir(parents=True,exist_ok=False)
events=out/'git-events';events.mkdir()
source=Path(sys.argv[1]).resolve()
programs={}
direct=run(['xcrun','--find','git']).stdout.strip()
for label,selected in [('launcher',''),('direct',direct)]:
 directory=out/label;directory.mkdir()
 programs[label]=str(build(source,directory,events,git_binary=selected))
root=Path(sys.argv[2]).resolve()
args=['run','--timings','--cache-dir',str(out/'cache'),'--','go','test','-count=1','./...']
env=dict(os.environ)
run([programs['launcher']]+args,cwd=root,env=env)
keys={}
for label in programs:
 p=run([programs[label],'explain']+[a for a in args[1:] if a!='--timings'],cwd=root,env=env)
 keys[label]=re.search(r'key: ([a-f0-9]{64})',p.stdout).group(1)
assert keys['launcher']==keys['direct']
for label in programs:
 for _ in range(5):run([programs[label]]+args,cwd=root,env=env)
orders=[['launcher','direct']]*250+[['direct','launcher']]*250
random.Random(20261018).shuffle(orders)
report={'pairs':500,'seed':20261018,'warmups_per_version':5,'keys':keys,'git':direct,'runs':[],'completed':False}
(out/'results.json').write_text(json.dumps(report,indent=2))
for i,order in enumerate(orders):
 for label in order:
  start=time.time_ns()
  usage=resource.getrusage(resource.RUSAGE_CHILDREN)
  p=run([programs[label]]+args,cwd=root,env=env)
  end=time.time_ns()
  after=resource.getrusage(resource.RUSAGE_CHILDREN)
  es=[json.loads(l) for l in p.stderr.splitlines() if l.startswith('{"')]
  t=next(e for e in es if e.get('type')=='receipt_timing')
  assert 'receipt: hit' in p.stderr and not any(s['stage']=='execution' for s in t['stages'])
  report['runs'].append({'pair':i,'label':label,'order':order,'start_ns':start,'end_ns':end,
     'cpu_ms':1000*(after.ru_utime-usage.ru_utime+after.ru_stime-usage.ru_stime),'events':es})
 if (i+1)%50==0:
  print('pairs',i+1,'max Git ms',{label:max(e['wall_ms'] for r in report['runs'] if r['label']==label for e in r['events'] if e.get('program')=='git') for label in programs},flush=True)
  (out/'results.json').write_text(json.dumps(report,indent=2))
for label in programs:
 p=run([programs[label],'explain']+[a for a in args[1:] if a!='--timings'],cwd=root,env=env)
 assert re.search(r'key: ([a-f0-9]{64})',p.stdout).group(1)==keys[label]
report['completed']=True
(out/'results.json').write_text(json.dumps(report,indent=2))
print('completed',flush=True)
