#!/usr/bin/env python3
"""macOS diagnostic overlay: SOURCE OUTPUT_DIR [--no-trace]; never modifies production source.
Builds a traced CLI, runs 60 Go and 40 generic warm hits, keeps all traces.
Output is local diagnostic data; inspect before publishing.
"""
import hashlib
import json
import os
from pathlib import Path
import random
import re
import subprocess
import sys
import time

def run(args, **kw):
    return subprocess.run(args, capture_output=True, text=True, check=True, **kw)

source = Path(sys.argv[1]).resolve()
out = Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)
tracing = "--no-trace" not in sys.argv[3:]
main = r'''package main
import (
 "encoding/json"
 "fmt"
 "os"
 "github.com/sidsharma96/receipt/internal/app"
 "runtime"
 "runtime/trace"
 "time"
)
func main() {
 if len(os.Args)<3 { panic("diagnostic CLI requires trace path before Receipt args") }
 var f *os.File
 var err error
 if os.Args[1] != "-" {
  f, err = os.Create(os.Args[1]); if err != nil { panic(err) }
  if err = trace.Start(f); err != nil { panic(err) }
 }
 var before, after runtime.MemStats
 runtime.ReadMemStats(&before)
 started := time.Now()
 code := app.Main(os.Args[2:], os.Stdout, os.Stderr)
 elapsed := time.Since(started)
 runtime.ReadMemStats(&after)
 if f != nil {
  trace.Stop()
  if err = f.Close(); err != nil { panic(err) }
 }
 data, err := json.Marshal(map[string]any{
  "type":"diagnostic_gc", "elapsed_ns":elapsed.Nanoseconds(),
  "collections":after.NumGC-before.NumGC, "pause_ns":after.PauseTotalNs-before.PauseTotalNs,
  "allocated_bytes":after.TotalAlloc-before.TotalAlloc, "gc_cpu_fraction":after.GCCPUFraction,
 })
 if err != nil { panic(err) }
 fmt.Fprintln(os.Stderr,string(data))
 os.Exit(code)
}
'''
(out/'main.go').write_text(main)
manifest = (source/'internal/fingerprint/manifest.go').read_text()
manifest = manifest.replace('"context"', '"context"\n "bytes"\n "runtime/trace"\n "time"')
needle = '''	c.Dir = dir
	b, err := c.Output()'''
replacement = r''' c.Dir = dir
 // Only diagnostic children get GC logging; Receipt's own environment stays fixed.
 c.Env = append(os.Environ(), "GODEBUG=gctrace=1")
 var diagnosticStderr bytes.Buffer
 c.Stderr = &diagnosticStderr
 region := trace.StartRegion(ctx, "child:"+args[0]+":"+args[1])
 started := time.Now()
 b, err := c.Output()
 elapsed := time.Since(started)
 region.End()
 var cpu int64
 if c.ProcessState != nil {
  cpu = c.ProcessState.UserTime().Nanoseconds()+c.ProcessState.SystemTime().Nanoseconds()
 }
 // Do not publish arbitrary child stderr; keep only Go runtime GC trace lines.
 var gcLines []string
 for _, line := range strings.Split(diagnosticStderr.String(), "\n") {
  if strings.HasPrefix(line,"gc ") && strings.Contains(line," ms clock,") {
   gcLines = append(gcLines,line)
  }
 }
 data, marshalErr := json.Marshal(map[string]any{
  "type":"diagnostic_child","program":args[0],"operation":args[1],
  "elapsed_ns":elapsed.Nanoseconds(),"cpu_ns":cpu,"gc_lines":gcLines,
 })
 if marshalErr != nil { panic(marshalErr) }
 fmt.Fprintln(os.Stderr,string(data))'''
assert manifest.count(needle)==1
(out/'manifest.go').write_text(manifest.replace(needle,replacement))
observe = (source/'internal/observe/observe.go').read_text()
observe = observe.replace('"time"', '"time"\n runtimetrace "runtime/trace"')
observe = observe.replace('start := time.Now()', 'region := runtimetrace.StartRegion(ctx,names[stage])\n start := time.Now()')
observe = observe.replace('return func() {\n\t\tt.mu.Lock()', 'return func() {\n        region.End()\n\t\tt.mu.Lock()')
(out/'observe.go').write_text(observe)
mapping = {str(source/'cmd/receipt/main.go'):str(out/'main.go'),
           str(source/'internal/fingerprint/manifest.go'):str(out/'manifest.go'),
           str(source/'internal/observe/observe.go'):str(out/'observe.go')}
(out/'overlay.json').write_text(json.dumps({'Replace':mapping}))
binary = str(out/'receipt-diagnostic')
run(['go','build','-overlay',str(out/'overlay.json'),'-trimpath','-buildvcs=false','-ldflags=-linkmode=external','-o',binary,'./cmd/receipt'],cwd=source)
root = out/'fixture'
root.mkdir()
run(['git','init','-q',str(root)])
(root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
(root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
(root/'.gitignore').write_text('ignored\n')
(root/'ignored').write_text('excluded')
for i in range(256):
    (root/f'input-{i}').write_bytes(bytes([i])*4096)
args = {'go':['run','--timings','--cache-dir',str(out/'cache'),'--','go','test','-count=1','./...'],
        'generic':['run','--timings','--cache-dir',str(out/'cache'),'--allow-generic','--','/bin/sh','-c','true']}
env = dict(os.environ)
report = {'harness_repository_revision':run(['git','rev-parse','HEAD'],cwd=Path.cwd()).stdout.strip(),
          'source_path':str(source), 'tracing':tracing,
          'source_sha256':{f:hashlib.sha256((source/f).read_bytes()).hexdigest() for f in ['cmd/receipt/main.go','internal/fingerprint/manifest.go','internal/observe/observe.go']}, 'counts':{'go':60,'generic':40},'warmups_per_case':5,
          'note':'Diagnostic overlay; child GODEBUG=gctrace=1 changes probe conditions. Not a speed comparison.',
          'records':[], 'completed':False}
(out/'results.json').write_text(json.dumps(report,indent=2))
for case in args:
    run([binary,str(out/'warmup.trace') if tracing else '-']+args[case],cwd=root,env=env)
    for _ in range(5):
        run([binary,str(out/'warmup.trace') if tracing else '-']+args[case],cwd=root,env=env)
schedule = ['go']*60+['generic']*40
random.Random(20261018).shuffle(schedule)
for i,case in enumerate(schedule):
    path = out/f'{i:03d}-{case}.trace'
    started = time.perf_counter_ns()
    p = run([binary,str(path) if tracing else '-']+args[case],cwd=root,env=env)
    external = time.perf_counter_ns()-started
    entries = [json.loads(s) for s in (p.stdout+p.stderr).splitlines() if s.startswith('{"')]
    timing = next(e for e in entries if e.get('type')=='receipt_timing')
    assert 'receipt: hit' in p.stderr
    assert not any(s['stage']=='execution' for s in timing['stages'])
    report['records'].append({'index':i,'case':case,'external_ns':external,'events':entries,'trace':path.name if tracing else None})
    if (i+1)%20==0:
        print('diagnostic hits',i+1,flush=True)
        (out/'results.json').write_text(json.dumps(report,indent=2))
report['completed']=True
(out/'results.json').write_text(json.dumps(report,indent=2))
print('completed',out/'results.json',flush=True)
