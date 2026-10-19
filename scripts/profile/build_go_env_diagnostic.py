#!/usr/bin/env python3
"""Build an isolated diagnostic cmd/go from installed Go 1.27.2 source.
Usage: build_go_env_diagnostic.py GOROOT NEW_OUTPUT [--fix-cache-parser]
The optional cache change is a diagnostic experiment, not an installation patch.
"""
import argparse,json,pathlib,subprocess,os
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('goroot',type=pathlib.Path)
parser.add_argument('output',type=pathlib.Path)
parser.add_argument('--fix-cache-parser',action='store_true')
options=parser.parse_args()
root=options.goroot.resolve();out=options.output.resolve()
if not (root/'VERSION').read_text().startswith('go1.27.2\n'):
    raise SystemExit('This diagnostic overlay is pinned to Go 1.27.2 source')
out.mkdir(parents=True,exist_ok=False)
mapping={}
def edit(rel, substitutions):
 p=root/'src'/rel;s=p.read_text()
 for old,new in substitutions:
  assert s.count(old)==1,(rel,old,s.count(old))
  s=s.replace(old,new)
 target=out/(rel.replace('/','_'));target.write_text(s);mapping[str(p)]=str(target)
helper=r'''package base
import (
 "context"
 "encoding/json"
 "os"
 "os/exec"
 "runtime"
 "runtime/trace"
 "sync"
 "syscall"
 "time"
)
var diagLock sync.Mutex
var diagEvents []map[string]any
var diagTrace *os.File
func DiagMark(label string) {
 now:=time.Now()
 var r syscall.Rusage
 syscall.Getrusage(syscall.RUSAGE_SELF,&r)
 diagLock.Lock()
 diagEvents=append(diagEvents,map[string]any{"label":label,"unix_ns":now.UnixNano(),
 "user_us":r.Utime.Nano()/1000,"sys_us":r.Stime.Nano()/1000,
 "minor_faults":r.Minflt,"major_faults":r.Majflt,"voluntary":r.Nvcsw,"involuntary":r.Nivcsw})
 diagLock.Unlock()
 trace.Log(context.Background(),"stage",label)
}
func DiagChild(c *exec.Cmd){
 if c.ProcessState==nil{return}
 r,_:=c.ProcessState.SysUsage().(*syscall.Rusage)
 diagLock.Lock();defer diagLock.Unlock()
 diagEvents=append(diagEvents,map[string]any{"label":"compiler_child_usage","unix_ns":time.Now().UnixNano(),"pid":c.ProcessState.Pid(),"program":c.Path,"cpu_ns":c.ProcessState.UserTime().Nanoseconds()+c.ProcessState.SystemTime().Nanoseconds(),"usage":r})
}
func init(){DiagMark("base_init")}
func DiagTraceStart(){
 if path:=os.Getenv("RECEIPT_GO_TRACE");path!="" {
  f,err:=os.Create(path);if err!=nil{panic(err)}
  if err:=trace.Start(f);err!=nil{panic(err)}
  diagTrace=f
 }
}
func DiagFlush(){
 DiagMark("exit_begin")
 var m runtime.MemStats;runtime.ReadMemStats(&m)
 if diagTrace!=nil{trace.Stop();diagTrace.Close()}
 json.NewEncoder(os.Stderr).Encode(map[string]any{"type":"go_diagnostic","pid":os.Getpid(),"events":diagEvents,"gc_pause_ns":m.PauseTotalNs,"gc_count":m.NumGC})
}
'''
p=out/'base_diagnostic.go';p.write_text(helper);mapping[str(root/'src/cmd/go/internal/base/receipt_diagnostic.go')]=str(p)
edit('cmd/go/internal/base/base.go', [('func Exit() {','func Exit() {\n DiagFlush()')])
edit('cmd/go/main.go',[
 ('func main() {','func main() {\n base.DiagMark("main_enter"); base.DiagTraceStart()'),
 ('\ttelemetry.MaybeChild()', '\tbase.DiagMark("telemetry_child_begin"); telemetry.MaybeChild()'),
 ('\thandleChdirFlag()','\tbase.DiagMark("telemetry_open_done"); handleChdirFlag()'),
 ('\ttoolchain.Select()', '\tbase.DiagMark("toolchain_begin"); toolchain.Select(); base.DiagMark("toolchain_done")'),
 ('\tflag.Usage = base.Usage', '\tbase.DiagMark("telemetry_parent_done"); flag.Usage = base.Usage'),
 ('\ttelemetrystats.Increment()', '\tbase.DiagMark("telemetry_stats_begin"); telemetrystats.Increment(); base.DiagMark("telemetry_stats_done")'),
 ('\tcfg.CmdEnv = envcmd.MkEnv()', '\tbase.DiagMark("mk_env_begin"); cfg.CmdEnv = envcmd.MkEnv(); base.DiagMark("mk_env_done")'),
 ('\tcmd.Run(ctx, cmd, args)', '\tbase.DiagMark("cmd_run_begin"); cmd.Run(ctx, cmd, args); base.DiagMark("cmd_run_done")')])
edit('cmd/go/internal/envcmd/env.go',[
 ('func runEnv(ctx context.Context, cmd *base.Command, args []string) {','func runEnv(ctx context.Context, cmd *base.Command, args []string) {\n base.DiagMark("env_begin")'),
 ('\tenv = append(env, ExtraEnvVars(moduleLoader)...)','\tbase.DiagMark("extra_env_begin"); env = append(env, ExtraEnvVars(moduleLoader)...); base.DiagMark("extra_env_done")'),
 ('\tif err := fsys.Init(); err != nil {','\tbase.DiagMark("fsys_begin"); if err := fsys.Init(); err != nil {'),
 ('\t// Do we need to call ExtraEnvVarsCostly','\tbase.DiagMark("fsys_done")\n\t// Do we need to call ExtraEnvVarsCostly'),
 ('\t\twork.BuildInit(moduleLoader)','\t\tbase.DiagMark("build_init_begin"); work.BuildInit(moduleLoader); base.DiagMark("build_init_done")'),
 ('\t\tenv = append(env, ExtraEnvVarsCostly(moduleLoader)...)','\t\tbase.DiagMark("costly_begin"); env = append(env, ExtraEnvVarsCostly(moduleLoader)...); base.DiagMark("costly_done")'),
 ('\t// print\n','\tbase.DiagMark("output_begin")\n\t// print\n'),
 ('\t\tprintEnvAsJSON(env, *envChanged)','\t\tprintEnvAsJSON(env, *envChanged); base.DiagMark("output_done")'),
 ('\tb := work.NewBuilder("", ld.VendorDirOrEmpty)','\tbase.DiagMark("builder_begin"); b := work.NewBuilder("", ld.VendorDirOrEmpty); base.DiagMark("builder_done")'),
 ('\tcppflags, cflags, cxxflags, fflags, ldflags, err := b.CFlags(&load.Package{})','\tbase.DiagMark("cflags_begin"); cppflags, cflags, cxxflags, fflags, ldflags, err := b.CFlags(&load.Package{}); base.DiagMark("cflags_done")'),
 ('\tcmd := b.GccCmd(".", "")','\tbase.DiagMark("gcc_cmd_begin"); cmd := b.GccCmd(".", ""); base.DiagMark("gcc_cmd_done")')])
edit('cmd/go/internal/work/exec.go',[
 ('func (b *Builder) gccSupportsFlag(compiler []string, flag string) bool {','func (b *Builder) gccSupportsFlag(compiler []string, flag string) bool {\n base.DiagMark("gcc_supports_flag_begin"); defer base.DiagMark("gcc_supports_flag_done")'),
 ('func (b *Builder) gccCompilerID(compiler string) (id cache.ActionID, ok bool) {','func (b *Builder) gccCompilerID(compiler string) (id cache.ActionID, ok bool) {\n base.DiagMark("gcc_compiler_id_begin"); defer base.DiagMark("gcc_compiler_id_done")'),
 ('\tdata, _, err := cache.GetBytes(cache.Default(), key)','\tbase.DiagMark("compiler_cache_read_begin"); data, _, err := cache.GetBytes(cache.Default(), key); base.DiagMark("compiler_cache_read_done")'),
 ('\t\tif len(stats)%2 != 0 {', '\t\tif len(stats)%2 != 0 {\n base.DiagMark(fmt.Sprintf("compiler_cache_odd_fields_%d",len(stats)))'),
 ('\t\tcopy(id[:], data[len(data)-len(id):])', '\t\tbase.DiagMark("compiler_cache_validated"); copy(id[:], data[len(data)-len(id):])'),
 ('\ttoolID, exe2, err := b.gccToolID(compiler, "c")','\tbase.DiagMark("compiler_probe_begin"); toolID, exe2, err := b.gccToolID(compiler, "c"); base.DiagMark("compiler_probe_done")')])
edit('cmd/go/internal/work/buildid.go',[
 ('\tout, err := cmd.CombinedOutput()', '\tbase.DiagMark("compiler_child_begin"); out, err := cmd.CombinedOutput(); base.DiagChild(cmd); base.DiagMark("compiler_child_done")')])
fixed=options.fix_cache_parser
if fixed:
 p=out/'cmd_go_internal_work_exec.go'
 s=p.read_text()
 old=r'stats := strings.Split(string(data[:len(data)-len(id)]), "\x00")'
 new=r'stats := strings.Split(strings.TrimSuffix(string(data[:len(data)-len(id)]), "\x00"), "\x00")'
 assert s.count(old)==1
 s=s.replace(old,new)
 old='for i := 0; i+2 <= len(stats); i++ {'
 assert s.count(old)==1
 s=s.replace(old,'for i := 0; i+2 <= len(stats); i += 2 {')
 p.write_text(s)
(out/'overlay.json').write_text(json.dumps({'Replace':mapping},indent=2))
build_env=dict(os.environ);build_env['GOMODCACHE']=str(out/'build-mod-cache')
subprocess.run([str(root/'bin/go'),'build','-overlay',str(out/'overlay.json'),'-trimpath','-buildvcs=false','-ldflags=-linkmode=external -X runtime.defaultGOROOT='+str(root),'-o',str(out/('go-fixed-parser' if fixed else 'go-diagnostic')),'cmd/go'],check=True,env=build_env)
print('built',out/('go-fixed-parser' if fixed else 'go-diagnostic'))
