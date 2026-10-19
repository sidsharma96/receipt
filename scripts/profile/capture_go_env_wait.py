#!/usr/bin/env python3
"""Localize Go env delays with stage markers and optional Go runtime tracing.
Usage: capture_go_env_wait.py STOCK_GO DIAGNOSTIC_GO NEW_OUTPUT [--trace]
Raw traces and full event records stay local; this is not a speed benchmark.
"""
import argparse,hashlib,json,os,pathlib,re,resource,shutil,subprocess,tempfile,time
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('stock_go',type=pathlib.Path)
parser.add_argument('diagnostic_go',type=pathlib.Path)
parser.add_argument('output',type=pathlib.Path)
parser.add_argument('--trace',action='store_true')
parser.add_argument('--limit',type=int,default=1200)
parser.add_argument('--threshold',type=float,default=200)
options=parser.parse_args()
if options.limit<1 or options.threshold<=0:raise SystemExit('limit/threshold must be positive')
out=options.output.resolve();out.mkdir(parents=True,exist_ok=False)
root=out/'fixture';root.mkdir();(root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
stock=options.stock_go.resolve();binary=options.diagnostic_go.resolve();env=dict(os.environ)
def norm(values):
 v=dict(values);prefix=os.path.join(v.get('GOTMPDIR') or os.environ.get('TMPDIR') or tempfile.gettempdir(),'go-build')
 pattern=r'(-f(?:file|debug)-prefix-map='+re.escape(prefix)+r')[0-9]+(=/tmp/go-build)(?=$|[ \'\"])'
 v['GOGCCFLAGS']=re.sub(pattern,r'\1<scratch>\2',v['GOGCCFLAGS']);return v
original=json.loads(subprocess.run([str(stock),'env','-json'],cwd=root,env=env,capture_output=True,text=True,check=True).stdout)
smoke=subprocess.run([str(binary),'env','-json'],cwd=root,env=env,capture_output=True,text=True,check=True)
new=json.loads(smoke.stdout);differences=[k for k in norm(original) if norm(original)[k]!=norm(new).get(k)]
if differences:raise RuntimeError('diagnostic output differs: '+repr(differences))
for _ in range(5):subprocess.run([str(binary),'env','-json'],cwd=root,env=env,capture_output=True,check=True)
report={'purpose':'diagnostic stage localization, not production speed comparison','limit':options.limit,'threshold_ms':options.threshold,'warmups':5,'runtime_trace':options.trace,'normalized_output_equal':True,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'rows':[],'completed':False}
(out/'results.json').write_text(json.dumps(report,indent=2))
for i in range(options.limit):
 path=out/'latest.trace';child_env=dict(env)
 if options.trace:child_env['RECEIPT_GO_TRACE']=str(path)
 usage=resource.getrusage(resource.RUSAGE_CHILDREN);start=time.time_ns()
 p=subprocess.Popen([str(binary),'env','-json'],cwd=root,env=child_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 stdout,stderr=p.communicate();end=time.time_ns();after=resource.getrusage(resource.RUSAGE_CHILDREN)
 if p.returncode:raise RuntimeError(stderr.decode())
 diagnostic=json.loads(stderr);assert diagnostic['pid']==p.pid
 if norm(json.loads(stdout))!=norm(original):raise RuntimeError('environment output changed')
 row={'i':i,'pid':p.pid,'start_ns':start,'end_ns':end,'elapsed_ms':(end-start)/1e6,'cpu_ms':1000*((after.ru_utime-usage.ru_utime)+(after.ru_stime-usage.ru_stime)),'diagnostic':diagnostic}
 report['rows'].append(row)
 if options.trace and row['elapsed_ms']==max(r['elapsed_ms'] for r in report['rows']):shutil.copyfile(path,out/'largest.trace')
 if row['elapsed_ms']>=options.threshold:
  report['trigger']=row
  if options.trace:path.rename(out/'slow.trace')
  print('slow',row['elapsed_ms'],'CPU',row['cpu_ms'],'PID',p.pid,flush=True);break
 if (i+1)%200==0:print('calls',i+1,'max ms',max(r['elapsed_ms'] for r in report['rows']),flush=True)
report['completed']=True
(out/'results.json').write_text(json.dumps(report,indent=2)+'\n')
worst=max(report['rows'],key=lambda r:r['elapsed_ms']);print('calls',len(report['rows']),'max',worst['elapsed_ms'],'CPU',worst['cpu_ms'],'gc pause',worst['diagnostic']['gc_pause_ns'],flush=True)
events=[e for e in worst['diagnostic']['events'] if 'user_us' in e];deltas=[{'from':a['label'],'to':b['label'],'elapsed_ms':(b['unix_ns']-a['unix_ns'])/1e6,'cpu_ms':((b.get('user_us',0)+b.get('sys_us',0))-(a.get('user_us',0)+a.get('sys_us',0)))/1000} for a,b in zip(events,events[1:]) if 'user_us' in a and 'user_us' in b]
print('longest intervals',json.dumps(sorted(deltas,key=lambda d:d['elapsed_ms'],reverse=True)[:10],indent=2));print('child',json.dumps([e for e in worst['diagnostic']['events'] if e['label']=='compiler_child_usage'],indent=2))
