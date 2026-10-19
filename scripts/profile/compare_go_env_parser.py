#!/usr/bin/env python3
"""Compare two diagnostic Go env binaries using one isolated warm build cache.
Usage: compare_go_env_parser.py BEFORE AFTER NEW_OUTPUT
60 balanced randomized pairs; five warmup pairs; no outlier exclusions.
"""
import argparse,json,os,pathlib,random,re,resource,statistics,subprocess,tempfile,time
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('before',type=pathlib.Path)
parser.add_argument('after',type=pathlib.Path)
parser.add_argument('output',type=pathlib.Path)
options=parser.parse_args()
out=options.output.resolve();out.mkdir(parents=True,exist_ok=False)
root=out/'fixture';root.mkdir();(root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
env=dict(os.environ);env['GOCACHE']=str(out/'cache')
bins={'before':options.before.resolve(),'after':options.after.resolve()}
def norm(v):
 v=dict(v);prefix=os.path.join(v.get('GOTMPDIR') or env.get('TMPDIR') or tempfile.gettempdir(),'go-build')
 pattern=r'(-f(?:file|debug)-prefix-map='+re.escape(prefix)+r')[0-9]+(=/tmp/go-build)(?=$|[ \'\"])'
 v['GOGCCFLAGS']=re.sub(pattern,r'\1<scratch>\2',v['GOGCCFLAGS']);return v
reference=None
report={'scope':'two rebuilt diagnostic Go binaries; cache parser is the only algorithmic change; not a production Receipt benchmark','seed':20261019,'pairs':60,'warmups':5,'cache':'one isolated GOCACHE shared by both variants','rows':[],'completed':False,'all_normalized_outputs_equal':True}
def run(label):
 global reference
 a=resource.getrusage(resource.RUSAGE_CHILDREN);start=time.time_ns()
 p=subprocess.run(['/usr/bin/time','-l',str(bins[label]),'env','-json'],cwd=root,env=env,capture_output=True,text=True,check=True)
 end=time.time_ns();b=resource.getrusage(resource.RUSAGE_CHILDREN)
 values=norm(json.loads(p.stdout))
 if reference is None:reference=values
 assert reference==values,'normalized output changed'
 diagnostic=json.loads(next(line for line in p.stderr.splitlines() if line.startswith('{"')))
 compiler=[e for e in diagnostic['events'] if e['label']=='compiler_child_usage']
 return {'label':label,'elapsed_ms':(end-start)/1e6,'cpu_ms':1000*((b.ru_utime-a.ru_utime)+(b.ru_stime-a.ru_stime)),'rss_mb':int(re.search(r'(\d+)\s+maximum resident set size',p.stderr).group(1))/1e6,'compiler_children':len(compiler),'compiler_cpu_ms':sum(e['cpu_ns'] for e in compiler)/1e6,'pid':diagnostic['pid'],'cache_odd_fields':sum(e['label'].startswith('compiler_cache_odd_fields') for e in diagnostic['events']),'cache_validated':sum(e['label']=='compiler_cache_validated' for e in diagnostic['events'])}
for _ in range(5):
 for label in bins:run(label)
rng=random.Random(20261019);orders=[['before','after'],['after','before']]*30;rng.shuffle(orders)
(out/'results.json').write_text(json.dumps(report,indent=2))
for i,order in enumerate(orders):
 for label in order:
  row=run(label);row['pair']=i;row['order']=order;report['rows'].append(row)
report['summary']={label:{name:{'mean':statistics.fmean(r[name] for r in report['rows'] if r['label']==label),'median':statistics.median(r[name] for r in report['rows'] if r['label']==label),'max':max(r[name] for r in report['rows'] if r['label']==label)} for name in ['elapsed_ms','cpu_ms','rss_mb','compiler_children']} for label in bins}
report['completed']=True;(out/'results.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report['summary'],indent=2));print('all outputs equal',report['all_normalized_outputs_equal'])
