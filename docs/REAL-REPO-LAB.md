# Real-repository lab

Started 9 October 2026 with Receipt `127bc8d`. This is an experiment record,
not a completed runner-support phase. No production Go code changed.

The lab lives at `/Users/siddharthsharma/Desktop/testing`. `repositories.json`
records the five upstream URLs and exact revisions. `repos/` contains source,
`envs/` the Python environment, `tools/` a separate Zig installation, `caches/`
build and Receipt caches, and `results/` per-run JSON records and stdout/stderr.
Some tools also use their normal user caches; this is not a sandbox. Raw test
logs are local debugging material, not redacted telemetry for publication.

## First get a passing test, then ask whether Receipt reused it

A command exiting successfully does not mean Receipt found a saved result.
For example, regex passed on every attempt, but Receipt printed “running
uncached” and ran Cargo each time. Its faster later runs are not Receipt hits.

The lab script records a direct run, a first wrapped run and a repeat. Its
timing summary records whether the child test process actually ran. It does
not use `--allow-generic` to conceal missing runner support. Unsupported
commands currently fall back to execution rather than failing the test command.

| Repository | Selected baseline | Observed outcome |
|---|---|---|
| pytest | `testing/test_mark.py` | 128 passed, 1 expected failure. Receipt saved and replayed the result. |
| Hugo | `go test -count=1 ./common/collections` | Passed. Receipt reran it and refused publication because the toolchain probe changed. |
| regex | `cargo test -p regex --quiet` | 319 passed, 2 ignored across three groups, including documentation tests. Receipt ran uncached. |
| Ghostty | terminal-library tests with `-Dtest-filter=parser` | A summary run reported 739 passed, 2 skipped across two test executables. Receipt ran uncached. This is not the full application suite. |
| Vue core | Vitest unit project, `packages/reactivity` | 445 passed, 4 skipped. Receipt ran uncached. No browser suite ran. |

These are subsets within substantial repositories, not full-suite certification.
Setup and some runs overlapped. One sample per step, compilation caches and
changing machine load make these smoke checks, not comparative benchmarks.

## What the real projects exposed

### Go's temporary path changes the key

Receipt currently hashes the output of `go env -json`. In Hugo's selected Go
toolchain, `GOGCCFLAGS` contains a generated path such as
`go-build462669098`. The next probe contains a different number, even when
the source is unchanged. Receipt compares its before/after keys, finds a
difference, and declines to save the success.

This is a false change report, not evidence that Hugo edited its source.
Two consecutive `go env GOGCCFLAGS` calls reproduced it. The next Go-support
change should distinguish generated scratch paths from meaningful compiler
settings, with tests proving that real setting changes still invalidate reuse.
Do not simply ignore all compiler flags.

### Environment changes can look like source changes

After restoring a deliberately edited pytest file, one run still missed.
Comparing the saved manifests identified `SHLVL`, the shell's nesting count,
as the sole changed input. The lab now explicitly sets it to `1` for controlled
runs. Receipt's production environment policy is unchanged. We must separately
decide whether any environment variables can safely be excluded in real use.

### Installation is part of the experiment

- pytest needs Git version history. The initial shallow clone produced version
  `0.1.dev1`, which its own minimum-version check rejected. Fetching history and
  tags, then rebuilding its editable installation, resolved this.
- Hugo declares Go 1.27.0. Go downloaded that toolchain into the lab's module
  cache; the machine's default Go installation was not replaced.
- Ghostty required release Zig 0.16.0. The installed development snapshot had
  incompatible build APIs. A separate release archive was downloaded from
  ziglang.org and its SHA-256 checked against the official download index.
- Vue pins pnpm 12.4.2. The installed pnpm 10 launcher could not execute its
  new launcher directly. Invoking the pinned package's `bin/pnpm.mjs` with Node
  installed dependencies. A frozen-lockfile forced reinstall supplied missing
  optional native bindings. Lifecycle scripts remained disabled; no lockfile
  was edited. Tests invoke the installed Vitest module with Node directly.

## Repeating a run

From any directory:

```sh
python3 /Users/siddharthsharma/Desktop/testing/run_lab.py pytest --receipt
```

Replace `pytest` with `hugo`, `regex`, `ghostty` or `vue-core`. Each invocation
writes a new result directory and normally uses a fresh Receipt cache. Use
`--cache-dir PATH` to deliberately reuse a cache and `--only-receipt` to run
one wrapped attempt. Each subprocess has a three-minute limit; timeout handling
terminates its process group. The script never installs dependencies or edits
test files. Inspect each record's exit status; the script itself is a recorder,
not a CI success gate.

The pytest experiment temporarily added an always-failing test to the selected
module. Receipt executed the changed suite and returned failure instead of
replaying the previous success. After restoring the original file under the
controlled environment, Receipt replayed the original success without executing
tests. The added test was removed afterward. The three controlled records are
`results/pytest-1791580873424569000/record.json` (baseline/save/replay),
`results/pytest-1791580931848306000/record.json` (failure) and
`results/pytest-1791580945742618000/record.json` (restored replay), relative to
the lab directory. This is
evidence for that source-change scenario, not proof that external dependencies,
editable packages, services or toolchain changes are all captured.

## Next implementation priorities

1. Go scratch-path correction completed below; broaden platform/toolchain coverage later.
2. Specify Cargo inputs before adding recognition: selected compiler, workspace,
   features, lockfile, local dependencies and build-script inputs.
3. Specify Node/Vitest inputs: runtime, resolved packages, configuration and
   non-watch execution. A lockfile alone does not prove installed bytes match.
4. Handle Zig's compiler and dependency/build inputs explicitly.
5. Repeat source, dependency and toolchain changes; then expand to larger suites
   and measure runs sequentially with multiple samples.

The test runners print durations and other variable output. Exact-byte
verification can flag two successful executions as different. Any future output
comparison policy needs its own design; this experiment does not normalize
output or weaken quarantine.

## 9 October 2026: Go scratch-path correction

This maintenance fix changes the earlier Go outcome; it does not add a new
runner or claim complete Go dependency tracking. Hugo now saves and replays its
selected package tests.

### 1. What data structures are used, and why?

Go returns a JSON object of settings. Receipt decodes it into a
`map[string]string`: a setting name such as `CGO_CFLAGS` points to its value,
such as `-O2`. The map retains unfamiliar future fields too. A fixed struct
would need updating whenever Go adds a field; silently dropping those fields
could hide a meaningful change. JSON encoding sorts the map's keys before
Receipt hashes the result.

### 2. What is the flow and extension boundary?

Suppose Hugo passes, but Go describes its temporary workspace as `go-build123`
before the run and `go-build456` afterward. Previously that prevented saving.
Now Receipt recognizes the generated directory mapping and replaces just its
random numeric suffix with a fixed marker. The surrounding settings survive.

The abbreviated paths below illustrate the comparison, not actual measurements:

| Before a run | After a run | What Receipt compares |
|---|---|---|
| `go-build123 → /tmp/go-build` | `go-build456 → /tmp/go-build` | Both become `go-build<scratch> → /tmp/go-build` |
| `-O2` | `-O1` | Different; keep the change |
| `go1.27.0` | `go1.28.0` | Different; keep the change |
| Mapping to `/tmp/go-build` | Mapping to `/other` | Different; keep the change |

In `internal/fingerprint/manifest.go`, `executionInputs` gives the corrected
Go probe its own name:

```go
name = "resolved-go-environment-v2"
```

That name is part of the input record, so old Go entries cannot be mistaken for
observations made with the new rule. The manifest's JSON schema remains v1.
Python and generic commands keep their existing probe behaviour.

### 3. What primitives are used?

The existing Go subprocess supplies the settings. A regular expression—a
pattern for matching text—recognizes the specific generated compiler flag.
It must match the expected root directory, numeric suffix and destination.
No extra subprocess, lock, goroutine, network request or dependency was added.

### 4. What can go wrong?

Removing all compiler flags would make unrelated builds look identical.
Replacing every temporary-looking path would be similarly broad. The helper
touches only `GOGCCFLAGS` and only the known generated mapping. Unrecognized
forms remain unchanged, possibly producing another miss. Relative temporary
roots and future Go quoting/path formats may need further handling; they are
not silently treated as supported. Tests cover spaces and regex punctuation
in an absolute temporary root.

### 5. What are the hot/cold paths and costs?

The helper runs whenever a Go fingerprint is built: normally once on a hit,
twice around execution on a miss. It decodes a small settings object, scans
one flag string and encodes the object again. All source scanning, executable
reading and the Go subprocess remain. This does not make fingerprinting free.
There is no new maintenance path, and no isolated overhead benchmark yet.

### 6. What is guaranteed, assumed or excluded?

Recognized scratch suffixes no longer change the Go probe identity. Other
settings remain recorded. We trust the selected Go executable to describe its
environment. This is not a proof that every external module, compiler binary,
service or file read by a test is captured. Stable keys still do not promise
identical output; output comparison and quarantine are unchanged.

### 7. What alternatives were considered?

For this fix, dropping `GOGCCFLAGS` was rejected because it would also discard
meaningful flags. Selecting only a fixed list of Go environment fields was
rejected because new fields could go unnoticed. The narrow transformation
preserves more information, at the cost of needing updates if Go changes its
generated-path format. This is a decision for this correction, not a claim
about the original implementation's design history.

### 8. What evidence and limits remain?

Unit checks cover changing scratch suffixes, both supported flag kinds,
compiler flags, compiler name, toolchain version, unknown fields, destinations,
unrecognized strings and malformed/missing probe data. A real Go fingerprint
test checks repeated keys, source edits and a compiler-setting change.

The lab's Hugo `common/collections` package executed and saved, then replayed.
Adding an always-failing test produced a miss and exit 1. Removing that test
restored the original hit. The temporary test was deleted; upstream files were
not otherwise changed. The record directories are
`hugo-1791582485516519000`, `hugo-1791582514851894000` and
`hugo-1791582535392339000` beneath the lab's `results/` directory.
Changing `CGO_CFLAGS` to `-O1` also forced execution and succeeded, recorded in
`hugo-1791582536129588000`. The setting was passed only to that process; no
user-wide Go configuration was changed.

`make check`, `make race` and `make build` passed on macOS arm64. These runs did
not use PostgreSQL; database integration tests were skipped. Real Windows and
Linux execution, a real toolchain upgrade, relative temporary roots and full
Hugo-suite behaviour were not established. Lab timings are smoke observations,
not performance distributions. The separately downloaded Go 1.27.0 ran Hugo;
Receipt's automated suite used Go 1.22.1.

### 9. When would we revisit the decision?

Revisit the pattern when a supported Go version emits a new mapping form or
another volatile field. Revisit dependency capture when external module or
compiler changes can escape the recorded inputs. Revisit cost only after
sequential, repeated measurements show the probe is significant. Cargo,
Vitest and Zig support remain separate work.
