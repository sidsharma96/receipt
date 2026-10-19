# 16. Resolve the selected Git once

You run `receipt run --timings -- go test -count=1 ./...` on a Mac. Your `PATH`
selects `/usr/bin/git`. That program is Apple's launcher: it finds the Git inside
the active developer tools, then starts it. Receipt needs Git twice on a hit:
once to find the root for configuration, and once to list files for hashing.
Previously both calls went through that launcher.

On 18 October 2026 we changed `run` and `explain` to choose discovery's Git once
per invocation. If the selected path is exactly `/usr/bin/git` on macOS, Receipt
asks `/usr/bin/xcrun --find git` for the developer Git and calls that path directly.
[Apple describes the launchers and developer-tool selection here](https://developer.apple.com/library/archive/technotes/tn2339/_index.html).
A custom Git or wrapper selected through `PATH` keeps its selected path.

## Follow one hit, then a miss

Follow the arrows in execution order. The resolver finds a program, while the
later Git commands observe the repository. These are different kinds of reuse.

```text
Before, with Apple's launcher:
settings -> /usr/bin/git -> selected Git -> root
inputs   -> /usr/bin/git -> selected Git -> fresh file list -> hash -> replay

After, in one invocation:
PATH lookup -> /usr/bin/git? -> xcrun --find git -> keep selected path
             custom path? --------------------> keep custom path
settings -> kept path -> root
inputs   -> kept path -> fresh file list -> hash -> replay

If tests execute:
execute user command -> kept path -> fresh root -> fresh file list -> hash
                     -> compare keys -> store only when stable
Next invocation -> select again
```

The root and file list still come from real Git commands. If tests create a
nested repository, the final scan must discover that changed root; keeping the
program's path does not authorize keeping its old answers. The command the user
asked Receipt to execute still goes through the normal runner. `--bypass` skips
selection entirely. Cache-management commands and standalone fingerprint callers
retain their existing behavior unless they explicitly attach a selection.

## Keep the user's choice

The guard in
[`developerGit`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/git.go#L38-L45):

```go
if platform != "darwin" || selected != "/usr/bin/git" {
	return selected
}
b, err := find(ctx)
if err != nil {
	return selected
}
```

`selected` is the path found by Go's ordinary `PATH` search. The exact comparison
matters: `/custom/bin/git` might be a script that sets configuration or checks
permissions before calling Git. Receipt must run that script. It also keeps a
custom symlink's spelling rather than following it to decide whether to bypass it.
On other operating systems there is no Apple resolution step.

For the eligible launcher, Receipt uses the system `xcrun`, rather than another
program named `xcrun` found through `PATH`. It inherits the existing environment,
including `DEVELOPER_DIR`, so an explicit developer-directory selection is honored.
It checks that the answer is an absolute executable path. A failed lookup or an
unusable answer keeps the original launcher; it does not pick another installed
Git. If that launcher then fails, the existing discovery error behavior applies.
Cancellation still reaches subprocesses. No global environment variable changes.

The small value kept in the context is a path string and any initial lookup error.
A context carries invocation settings and cancellation through related calls.
It is not a process-wide cache: another call to the CLI selects again. Within an
invocation, the chosen path is fixed, but executable bytes can still be replaced.
Changing the active developer tools while a test is running is outside the stable
selection assumption. We do not pin an open executable or create a filesystem
snapshot.

## Call that path without changing recorded inputs

The launch boundary in
[`commandOutput`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L78-L95):

```go
c := exec.CommandContext(ctx, path, args[1:]...)
c.Args[0] = args[0]
c.Dir = dir
b, err := c.Output()
```

`path` names the selected program. The argument list retains the original `git`
name and its flags, and `dir` remains the requested working directory. Git's
configuration and environment still apply. Selection is not serialized into the
manifest: the root, files, environment hashes, command executable identity,
probes and key format are unchanged.

Differential tests compare the full manifests and keys with and without selection,
from the repository root and a nested directory. Their fixture includes deleted
tracked files, ignored and extra inputs, spaces and a newline in filenames.
Existing CLI tests also preserve the custom wrapper's root/list call counts and
refuse storage after tests create a nested repository. The measured new binary
replayed entries created by the old binary, with equal full keys before and after
all measurements. Compatible keys establish input encoding compatibility; they
do not establish deterministic test output.

## Measure the resolver too

The earlier investigation called a preselected path. This experiment compares
actual old and new production binaries, including selection on every invocation.
The new `git_selection` timing span includes `PATH` lookup and, on this machine,
`xcrun`. It is inside total Receipt duration but outside fingerprint/discovery.
Do not add nested timing spans together or count just the faster file-list call.

| Warm hit, arithmetic means | Before | After | Saving |
| --- | ---: | ---: | ---: |
| Go elapsed | 103.04 ms | 91.70 ms | 11.34 ms (11.0%) |
| Go process-tree CPU | 97.20 ms | 88.02 ms | 9.18 ms |
| Go peak RSS | 17.641 MB | 17.717 MB | **increased 0.076 MB** |
| Generic elapsed | 57.56 ms | 44.06 ms | 13.50 ms (23.5%) |
| Generic process-tree CPU | 52.39 ms | 42.98 ms | 9.41 ms |
| Generic peak RSS | 17.599 MB | 17.714 MB | **increased 0.116 MB** |

The resolver cost about 11.00 ms on Go hits and 11.34 ms on generic hits.
It consumes part of the savings from bypassing two launcher resolutions. Initial
file-list discovery alone fell 17.72 to 7.25 ms for Go; that single stage is not
the whole optimization. We measured a latency/CPU improvement, not a RAM saving.

A CPU millisecond is time the process tree spent executing on CPUs; elapsed time
also includes waiting. Peak RSS is the operating system's maximum resident-memory
report from `/usr/bin/time -l`, not total allocations or simultaneous memory summed
across every process. Smaller startup work does not imply a proportional peak-RAM
reduction.

The experiment used three blocks of balanced randomized ABBA/BAAB quartets:
old/new/new/old or its reverse. Averaging the two observations of each binary
reduces a steadily changing background load's effect. A separate old/old control
measures noise when there is no code difference. All 600 measured calls were
hits, and no outlier was removed. The fixture held 256 files of 4 KiB plus a tiny
Go test; caches and binaries were outside the repository. These are warm local
measurements on one Apple M1, not cold-disk or fleet-wide capacity results.

The 95% resampling intervals for mean elapsed savings were 9.03–14.02 ms for Go
and 9.76–19.09 ms for generic. We resampled groups of five neighboring quartets
within their blocks to retain some short-term dependence. These intervals describe
this run under that statistical model; they do not cover every source of system
noise. The old/old elapsed difference was −2.36 ms, with an interval −6.88 to
1.63 ms that includes zero. It is a useful noise check, not a correction to
subtract from the main result. Every block showed a positive old/new saving.
The largest individual new Go hit still took 292.60 ms. Routine savings do not
prove that rare startup delays or non-Git waits have disappeared.

The [measurement record](../MEASUREMENTS.md#invocation-local-git-selection-18-october-2026)
contains intervals, controls, raw records and reproduction commands. The
[nine-question review](../PHASE-REVIEWS.md#selected-git-resolution-18-october-2026)
records verification and remaining cases.

## Why stop at selecting the executable?

Caching Git's file list could save another process, but tests or another editor
can create files. Keeping that answer would hide new inputs. Caching runtime
probe output has a similar freshness problem. Neither follows from this change.
A permanently saved developer path would avoid resolution cost on later runs,
but could ignore a changed developer directory. A hardcoded Xcode path would also
break users of the standalone Command Line Tools or another Xcode installation.

One selection per invocation fits the two Git calls on a hit and four on a miss
without retaining repository observations. Revisit it if Apple's launcher changes
semantics, a supported user selection produces different output, resolution cost
outgrows the saved startup work, or callers need to change tools within an
invocation. Go probes and hashing still have their own variable costs; this is
not a fix for every 300 ms outlier.

## Check your understanding

<!-- questions:start -->

### Does selecting Git once keep its old file list?

**Level:** Newcomer

Tests create a new file. Does keeping the selected Git path let Receipt reuse
the pre-test file list?

<details>
<summary>Answer</summary>

No. The saved value identifies the program to run. After execution Receipt asks
that program for the root and file list again, then reads and hashes the inputs.
A new invocation chooses its program again too.

**Learn more:** [16. Resolve the selected Git once](../guide/16-resolve-selected-git.md)

</details>

### Which Git paths may bypass the Apple launcher?

**Level:** Practitioner

Your PATH selects `/custom/bin/git`, a wrapper around Apple's Git. Should Receipt
follow that wrapper to choose a faster executable?

<details>
<summary>Answer</summary>

No. The wrapper may implement user behavior. Only the exact selected path
`/usr/bin/git` on macOS is eligible for system xcrun resolution. Other selected
paths, including custom symlinks, keep their spelling and behavior. Resolver
failure retains the original launcher.

**Learn more:** [16. Resolve the selected Git once](../guide/16-resolve-selected-git.md)

</details>

### Does a faster mean prove startup outliers are fixed?

**Level:** Expert

The new binary has a lower mean latency with a positive savings interval. Does
that establish that the rare 300 ms delay cannot happen again?

<details>
<summary>Answer</summary>

No. The controlled run estimates routine warm-hit cost on one host. A confidence
interval from those observations does not bound every scheduler, filesystem or
probe delay. The new binary still had slow hits. Diagnosing a particular rare
wait requires evidence covering that call's stage and time interval; mean savings
do not identify its kernel cause.

**Learn more:** [16. Resolve the selected Git once](../guide/16-resolve-selected-git.md)

</details>

<!-- questions:end -->
