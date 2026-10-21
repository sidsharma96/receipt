#!/usr/bin/env python3
"""Capture launch/exec/wait boundaries and optionally a macOS recording.
Usage: capture_git_outlier.py SOURCE NEW_OUTPUT [--record] [--limit N] [--threshold MS]
Stops at the first Git call above threshold, or limit cached Go hits.
Build overlays affect only a local diagnostic binary. Raw traces stay local.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time

def run(args, **kwargs):
    return subprocess.run(args,capture_output=True,text=True,check=True,**kwargs)

def build(source,out,events,git_binary='',trace_git=True):
    main = r'''package main
import (
 "encoding/json"
 "fmt"
 "os"
 "github.com/sidsharma96/receipt/internal/app"
 "runtime"
)
func main() {
 var before, after runtime.MemStats
 runtime.ReadMemStats(&before)
 code := app.Main(os.Args[1:],os.Stdout,os.Stderr)
 runtime.ReadMemStats(&after)
 b, err := json.Marshal(map[string]any{"type":"diagnostic_gc",
  "pid":os.Getpid(),"collections":after.NumGC-before.NumGC,
  "pause_ns":after.PauseTotalNs-before.PauseTotalNs})
 if err != nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) }
 fmt.Fprintln(os.Stderr,string(b))
 os.Exit(code)
}
'''
    manifest=(source/'internal/fingerprint/manifest.go').read_text()
    manifest=manifest.replace('"context"','"context"\n "bytes"\n "time"\n "syscall"')
    old='''	c.Dir = dir
	b, err := c.Output()'''
    new=r''' c.Dir = dir
 if args[0] == "git" && DIRECT_GIT != "" {
  c.Path = DIRECT_GIT
  c.Args[0] = DIRECT_GIT
 }
 if args[0] == "git" && TRACE_GIT {
  c.Env = append(os.Environ(), "GIT_TRACE2_EVENT="+EVENT_PATH)
 }
 var output diagnosticOutput
 var privateStderr bytes.Buffer
 c.Stdout = &output
 c.Stderr = &privateStderr
 started := time.Now()
 err := c.Start()
 launched := time.Now()
 pid := 0
 if err == nil {
  pid = c.Process.Pid
  err = c.Wait()
 }
 finished := time.Now()
 if err == nil && len(output.Bytes()) > 0 && output.first == 0 {
  return nil, fmt.Errorf("diagnostic stdout timestamp was not captured")
 }
 cpu := int64(0)
 if c.ProcessState != nil {
  cpu = c.ProcessState.UserTime().Nanoseconds()+c.ProcessState.SystemTime().Nanoseconds()
 }
 var usage *syscall.Rusage
 if c.ProcessState != nil { usage, _ = c.ProcessState.SysUsage().(*syscall.Rusage) }
 data, marshalErr := json.Marshal(map[string]any{
  "type":"diagnostic_child","parent_pid":os.Getpid(),"pid":pid,
  "program":args[0],"operation":args[1],"start_ns":started.UnixNano(),
  "start_return_ns":launched.UnixNano(),"wait_return_ns":finished.UnixNano(),
  "first_output_ns":output.first,"start_ms":float64(launched.Sub(started).Nanoseconds())/1e6,
  "wait_ms":float64(finished.Sub(launched).Nanoseconds())/1e6,
  "wall_ms":float64(finished.Sub(started).Nanoseconds())/1e6,
  "cpu_ms":float64(cpu)/1e6,
  "usage":usage,"failed":err != nil,
 })
 if marshalErr != nil { return nil, marshalErr }
 fmt.Fprintln(os.Stderr,string(data))
 b := output.Bytes()'''
    new=new.replace('EVENT_PATH',json.dumps(str(events))).replace('DIRECT_GIT',json.dumps(git_binary)).replace('TRACE_GIT',str(trace_git).lower())
    assert manifest.count(old)==1
    manifest=manifest.replace(old,new)
    manifest += r'''
// One stdout copier writes this value; Wait joins it before fields are read.
type diagnosticOutput struct {
 buffer bytes.Buffer
 first int64
}
func (d *diagnosticOutput) Bytes() []byte { return d.buffer.Bytes() }
func (d *diagnosticOutput) Write(p []byte) (int,error) {
 if d.first == 0 { d.first = time.Now().UnixNano() }
 return d.buffer.Write(p)
}
'''
    (out/'main.go').write_text(main)
    (out/'manifest.go').write_text(manifest)
    mapping={str(source/'cmd/receipt/main.go'):str(out/'main.go'),
             str(source/'internal/fingerprint/manifest.go'):str(out/'manifest.go')}
    (out/'overlay.json').write_text(json.dumps({'Replace':mapping}))
    binary=out/'receipt-diagnostic'
    run(['go','build','-overlay',str(out/'overlay.json'),'-trimpath','-buildvcs=false',
         '-ldflags=-linkmode=external','-o',str(binary),'./cmd/receipt'],cwd=source)
    return binary

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source',type=Path)
    parser.add_argument('output',type=Path)
    parser.add_argument('--record',action='store_true')
    parser.add_argument('--instrument',help='Use one instrument instead of the full System Trace template')
    parser.add_argument('--limit',type=int,default=600)
    parser.add_argument('--threshold',type=float,default=200)
    options=parser.parse_args()
    source=options.source.resolve(); out=options.output.resolve()
    out.mkdir(parents=True,exist_ok=False)
    events=out/'git-events'; events.mkdir()
    binary=build(source,out,events)
    root=out/'fixture';root.mkdir()
    run(['git','init','-q',str(root)])
    (root/'go.mod').write_text('module fixture\n\ngo 1.22\n')
    (root/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestValue(t *testing.T) {}\n')
    (root/'.gitignore').write_text('ignored\n')
    (root/'ignored').write_text('excluded')
    for i in range(256):(root/f'input-{i}').write_bytes(bytes([i])*4096)
    args=[str(binary),'run','--timings','--cache-dir',str(out/'cache'),'--','go','test','-count=1','./...']
    env=dict(os.environ)
    run(args,cwd=root,env=env)
    for _ in range(5):run(args,cwd=root,env=env)
    report={'threshold_ms':options.threshold,'limit':options.limit,'warmups':5,
      'recording':options.record,'recording_verified':False,
      'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),
      'source_hashes':{f:hashlib.sha256((source/f).read_bytes()).hexdigest() for f in ['cmd/receipt/main.go','internal/fingerprint/manifest.go']},
      'runs':[],'completed':False}
    (out/'results.json').write_text(json.dumps(report,indent=2))
    recorder=None
    recorder_log=None
    try:
        if options.record:
            recorder_log=open(out/'recorder.log','w')
            selector=['--instrument',options.instrument] if options.instrument else ['--template','System Trace']
            recorder=subprocess.Popen(['xcrun','xctrace','record']+selector+[
             '--all-processes','--time-limit','180s',
             '--output',str(out/'system.trace')],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,bufsize=1)
            for line in recorder.stdout:
                recorder_log.write(line);recorder_log.flush()
                print(line.strip(),flush=True)
                if 'Starting recording' in line:break
            else:raise RuntimeError('System Trace did not start')
        for i in range(options.limit):
            started=time.time_ns()
            p=run(args,cwd=root,env=env)
            finished=time.time_ns()
            records=[json.loads(l) for l in p.stderr.splitlines() if l.startswith('{"')]
            timing=next(e for e in records if e.get('type')=='receipt_timing')
            assert 'receipt: hit' in p.stderr and not any(s['stage']=='execution' for s in timing['stages'])
            report['runs'].append({'i':i,'start_ns':started,'end_ns':finished,'events':records})
            slow=[e for e in records if e.get('program')=='git' and e['wall_ms']>=options.threshold]
            if slow:
                report['trigger']=slow
                print('Captured slow Git call',json.dumps(slow),flush=True)
                break
            if (i+1)%50==0:print('hits',i+1,'max Git ms',max(e['wall_ms'] for r in report['runs'] for e in r['events'] if e.get('program')=='git'),flush=True)
            if recorder is not None and recorder.poll() is not None:raise RuntimeError('recording ended before target')
        report['completed']=True
    finally:
        report['target_finished_ns']=time.time_ns()
        (out/'results.json').write_text(json.dumps(report,indent=2))
        if recorder is not None:
            if recorder.poll() is None:recorder.send_signal(signal.SIGINT)
            try:
                remainder,_=recorder.communicate(timeout=90)
            except subprocess.TimeoutExpired:
                report['recorder_finalize_timeout']=True
                recorder.terminate()
                remainder,_=recorder.communicate(timeout=10)
            recorder_log.write(remainder);recorder_log.flush()
            print(remainder.strip(),flush=True)
            report['recorder_exit']=recorder.wait()
            recorder_log.close()
            (out/'results.json').write_text(json.dumps(report,indent=2))
    print('Saved',out/'results.json',flush=True)

if __name__=='__main__':
    main()
