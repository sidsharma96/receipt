# 14. Reuse the Git root within one invocation

You run `receipt run --timings -- go test -count=1 ./...` from a repository's
`nested/` directory. Before reading configuration, Receipt asks Git where the
working tree begins. Git returns the parent directory. Until this optimization,
Receipt asked the same question again before listing the files to hash.

On 18 October 2026 we removed that second initial question. Receipt now passes
the root it just found into the first fingerprint. It still asks Git for a fresh
file list. If tests execute, the final fingerprint still discovers the root again.
This chapter explains why those two kinds of reuse have different consequences.

Current update (18 October): [Chapter 15](15-measure-toolchain-probe-overhead.md)
measures fresh probe costs and reduces local Go normalization allocations. Probe
commands and freshness remain unchanged; no whole-hit speedup was established.

## Follow the calls before deleting one

Git runs as a separate process: the operating system starts a program, Receipt
waits for its answer, then reads the answer. Even a short answer has startup and
waiting costs. Ten direct measurements in this repository gave a median of
16.07 ms for `git rev-parse --show-toplevel`, which returns the root, and 20.47 ms
for `git ls-files -z --cached --others --exclude-standard`, which lists files.
These are elapsed times including process launch, not a profile of Git's internals.

The old cache-hit sequence contained **three Git processes**:

```text
Configuration: ask root -> read root/.receipt.json
Fingerprint:   ask root again -> list files -> read and hash inputs
Cache:         look up key -> replay saved output
```

The new sequence contains **two**:

```text
Configuration: ask root -> read root/.receipt.json
                         |
                         +-- pass the same root string --+
                                                       |
Fingerprint:                                    list files -> hash inputs
Cache:                                          look up key -> replay

If tests run: execute command -> ask root AGAIN -> list files AGAIN -> hash
                              -> compare keys -> publish only if stable
```

The shared value is just a directory string, such as `/work/project`. No file
names, file hashes or probe outputs are remembered from earlier invocations.
The settings lookup chooses the same repository for configuration and the first
fingerprint. A map still collects file paths without duplicates; this change
does not alter how that map is filled or sorted into the manifest.

## Pass an observation to the code that needs it

The initial fingerprint call in
[`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L333):

```go
m, e := fingerprint.BuildAtRoot(ctx, root, dir, command, cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
```

`root` came from the configuration lookup in this same invocation. `dir` remains
the user's working directory, so running from a subdirectory retains its original
identity. The new entry point skips only root discovery; it delegates to the same
file enumeration, file hashing, environment collection and executable/probe code.
No manifest field or encoding changes.

When tests have executed, the call remains
[`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L333):

```go
after, e := fingerprint.Build(ctx, dir, command, cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
stable := e == nil && after.Key() == key
```

`Build` performs fresh root discovery. For example, a command might create a new
Git repository inside `nested/`. Afterward, Git reports that nested directory as
the root. The final key differs, and Receipt refuses to store the result. A test
exercises this case. Reusing the initial root after execution would lose that
check, so the new API explicitly forbids retaining the root across execution or
across invocations.

The contract assumes the repository topology stays stable while configuration
and the initial fingerprint are collected. A concurrent rename or replacement
can still race these observations, just as file edits can race hashing. This
optimization does not make a filesystem snapshot. It removes one observation;
it promises compatible keys for the same stable repository, not identical
behavior under every concurrent mutation.

## Keep Git responsible for the file list

Suppose you create `new_test.py` between two invocations. Reusing a previously
stored file list could omit that input. Receipt instead runs `git ls-files` on
each fingerprint, so the new untracked, nonignored file enters the key. Git also
continues to decide which files are ignored. Explicit extra inputs and the
configuration-file inclusion rule remain unchanged.

Replacing Git with a directory walk would require reproducing its index and
ignore behavior. A tracked file deleted from disk must still be listed so that
Receipt can record its existing deleted-file marker. Filenames containing spaces
or newlines must remain intact; Git's `-z` separates names with a zero byte rather
than a newline. Removing the redundant root query avoids changing any of those
rules. Listing failures, missing extra inputs and unsupported symlinks still
prevent successful fingerprint construction.

The `discovery` timing stage now normally reports one call in the initial CLI
fingerprint: file listing plus extra-input enumeration. A standalone `Build`,
including the post-run scan, still reports two calls because it also resolves the
root. The earlier configuration root query remains outside `fingerprint` and
`discovery`. Compare whole Receipt duration too; the diagnostic labels do not
cover every operation and nested stages must not be added to their parents.

## Initial ten-pair experiment (historical)

The comparison created entries with the unmodified CLI from commit `97b833e`,
then alternated ten old/new pairs for each command form. All **60 expected hits**
replayed without executing tests. Full keys matched for a generic command at the
root, the same command in a nested directory, and `go test`.

| Cache-hit case | Median latency before → after | Process CPU before → after | Peak RAM before → after |
| --- | ---: | ---: | ---: |
| Generic command, root | 70.42 → 52.43 ms | 0.06 → 0.04 s | 17.69 → 17.60 MB |
| Generic command, nested | 74.49 → 55.01 ms | 0.06 → 0.04 s | 17.79 → 17.61 MB |
| Go tests | 182.50 → 123.21 ms | 0.135 → 0.105 s | 18.05 → 17.80 MB |

The root generic case improved by about 26% in this warm-cache fixture, with
nonoverlapping latency ranges. Its discovery stage fell from 36.35 to 19.02 ms.
RAM is essentially unchanged: dropping a sequential child process need not
lower the largest memory peak. CPU totals have coarse resolution; do not treat
the displayed hundredths of a second as precise per-call savings.

The Go measurements were noisy, with overlapping latency ranges and changes in
unchanged probe work. Its discovery median fell from 40.82 to 22.02 ms, but the
larger whole-hit difference cannot all be attributed to this change. The raw
records, ranges and reproduction instructions are in the
[measurement reference](../MEASUREMENTS.md#git-root-reuse-18-october-2026).
No cold-device, network-filesystem or power measurements were made. We removed
one Git invocation; we did not measure exact filesystem syscall savings.

## Repeat with controls before attributing the gain

In the larger rerun, one Go hit took 107.256 ms with the old binary and
91.594 ms with the new one: a saving of 15.662 ms. Another pair could differ
because the operating system made one process wait longer. Repeating the pair
helps measure this variation; it does not automatically remove it.

On 18 October we first measured 60 Go pairs and 60 generic pairs with balanced,
randomized run order. We also measured 30 pairs using **the old binary for both
labels**. This is a control: any apparent saving there cannot come from our
code change. Go saved an average 14.44 ms, but the control showed an apparent
7.78 ms slowdown, including one 288 ms hit. We retained every observation.

That result motivated one fixed follow-up. Each comparison used four calls:

~~~text
Order A: old -> new -> new -> old
Order B: new -> old -> old -> new

Compare average(old's two times) − average(new's two times).
Both labels are equally early and late within the group.
~~~

If the machine gradually slows during those four calls, the ordering cancels
a straight-line trend. It cannot cancel a sudden pause. We randomized and
balanced the two orders across three blocks, warmed both binaries first, and
used identical builds of the two original revisions. The later probe
optimization was absent. The follow-up measured 60 Go groups, 30 generic groups
and 60 same-binary control groups.

| Follow-up result | Average saving | Approximate 95% interval |
| --- | ---: | ---: |
| Go whole hit | 16.29 ms | 12.15 to 20.80 ms |
| Go discovery | 17.19 ms | 15.19 to 19.95 ms |
| Go work outside discovery | −0.89 ms | −4.34 to 2.15 ms |
| Go process CPU | 15.37 ms | 13.42 to 17.49 ms |
| Generic whole hit | 17.33 ms | 14.46 to 20.87 ms |
| Same-binary Go control | 1.36 ms | −1.87 to 4.92 ms |

A negative saving means the second label was slower. An interval spanning zero
does not clearly distinguish a saving from no change in this experiment.
The intervals come from repeatedly resampling groups of nearby observations;
they describe uncertainty under that method's assumptions, not guaranteed
bounds for every machine. The first control even produced an interval excluding
zero despite identical code. Its long pauses and the few available groups made
that interval unreliable as a universal precision claim.

The follow-up control includes zero, but its block averages still vary.
Our defensible conclusion is **roughly 15–17 ms saved per warm hit here**, chiefly
from removing one Git query. Direct root queries took a median 15.72 ms.
The Go total saving now agrees with discovery's contribution; work outside
discovery shows no clear gain. The original apparent 59.29 ms whole-hit reduction
cannot be credited to this optimization.

Go mean peak RAM was 17.794 → 17.746 MB, with no clear change. Generic peak RAM
fell about 0.175 MB, under 1%. CPU time measures processor work, while latency
also includes waiting; neither is a direct hardware-cycle or energy measurement.
All 900 measured calls across both protocols replayed old cache entries, and
full keys matched at both ends. Stable keys still do not prove deterministic
test outputs.

The [measurement reference](../MEASUREMENTS.md#controlled-git-discovery-rerun-18-october-2026)
preserves both raw datasets, the protocol change, arithmetic checks, machine
conditions and reproduction commands. The host was not isolated and caches
were warm. More samples improved this estimate; cold storage, other machines
and background-load effects still need separate evidence. The
[dated review amendment](../PHASE-REVIEWS.md#controlled-git-discovery-rerun-18-october-2026)
records the decisions and limits without rewriting the original review.

## Choose the next question from evidence

Fresh listing and runtime probes remain. A longer-lived root cache could save
more queries but would need invalidation when directories or Git configuration
change. Parallel discovery could overlap waiting but would add coordination and
would not remove work. Those alternatives were deferred for this change.

Revisit file-list parsing if large repositories show allocation pressure; revisit
Git listing or probe startup if profiles show those dominate representative
workloads. Preserve the existing input rules and post-execution scan. Same input
keys allow old entries to replay; they do not prove test output is deterministic.
The full [nine-question review](../PHASE-REVIEWS.md#git-root-reuse-18-october-2026)
records evidence and remaining gaps.

## Check your understanding

<!-- questions:start -->

### Why can the first fingerprint reuse the Git root?

**Level:** Newcomer

Receipt just found `/work/project` to load configuration. Which work can its first
fingerprint reuse, and which work must still happen?

<details>
<summary>Answer</summary>

It can reuse that root string within this invocation, assuming the repository
topology stays stable during initial collection. It must still ask Git for the
current file list and hash the current inputs. This removes one root query, not
the discovery of changed files. It is not a filesystem snapshot.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>

### Why discover the root again after tests run?

**Level:** Newcomer

A command creates a new Git repository in its nested working directory. What
should the final fingerprint do?

<details>
<summary>Answer</summary>

It should ask Git for the root again. The new nested root changes the manifest,
so the before/after keys differ and Receipt refuses to store the result. Reusing
the initial root after execution would weaken that check.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>

### Why did removing a process barely change peak RAM?

**Level:** Newcomer

Receipt starts one fewer Git process, and the measured hit is faster. Why might
maximum resident memory remain almost unchanged?

<details>
<summary>Answer</summary>

Peak memory measures the largest reported footprint, not the sum of memory used
over time. The removed Git process ran sequentially; another part of the run can
still set the same peak. Saving a process can reduce CPU and waiting without
reducing that maximum.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>

### What does a same-binary timing control tell us?

**Level:** Practitioner

The old/new Go comparison appears faster, but comparing the old binary with
itself also produces a nonzero difference. Can we credit the whole old/new
difference to the code change?

<details>
<summary>Answer</summary>

No. Identical code cannot explain a control difference, so scheduling, waiting
or other variation is affecting the measurement. Keep those observations,
inspect the distribution and improve the comparison controls. Mirrored
old/new/new/old groups cancel linear drift within the four positions, but not
sudden pauses. A follow-up control interval including zero is reassuring;
it does not prove every estimated saving is unbiased or portable.

**Learn more:** [Repeat with controls before attributing the gain](../guide/14-reuse-the-git-root.md#repeat-with-controls-before-attributing-the-gain)

</details>

<!-- questions:end -->
