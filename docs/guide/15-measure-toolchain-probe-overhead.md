# 15. Measure toolchain probe overhead

You run the same Go tests twice. Each time, `go env -json` may describe a different
temporary build directory in a compiler flag:

```text
First observation:  -ffile-prefix-map=/tmp/go-build123=/tmp/go-build
Second observation: -ffile-prefix-map=/tmp/go-build456=/tmp/go-build
Recorded form:      -ffile-prefix-map=/tmp/go-build<scratch>=/tmp/go-build
```

Receipt already replaces this particular numeric suffix so a temporary directory
name does not cause a cache miss. It keeps the root, flag kind, destination and
other settings. On 18 October 2026 we measured probe overhead and made this local
normalization cheaper. The expensive fresh probes still run. This chapter explains
both the improvement and why it does not establish a faster whole cache hit.

Follow-up (19 October): [Chapter 16's Go investigation](16-resolve-selected-git.md#what-the-next-go-investigation-found)
locates a repeated compiler query caused by a known upstream Go cache-reader bug.
A diagnostic fix removes that query on a stable fixture; it is not a Receipt
optimization and does not explain the original rare 300 ms delay.

## Separate asking the tool from processing its answer

A probe is a command Receipt runs to learn the current runtime environment.
For Go, Receipt starts `go env -json`, waits for it to exit, captures its output,
decodes the JSON, normalizes the recognized temporary-path flag, encodes the
settings again, then hashes them. The hash enters the manifest under the existing
`resolved-go-environment-v2` name.

```text
Receipt                 Go subprocess              Receipt
start probe ----------> collect current settings
wait <----------------- JSON output + exit status
                         |
                         +-- failure: no successful toolchain input
successful output -> decode -> normalize scratch suffix -> encode -> hash
```

The initial benchmark separated subprocess/output capture from local JSON
normalization. The former took a median 36.28 ms per operation; the latter took
0.048 ms. A millisecond is one thousand microseconds: the normalizer is a tiny
fraction of probe time. Optimizing it cannot remove tens of milliseconds from a
cache hit.

Further standalone process measurements gave these medians:

| Command | Elapsed time |
| --- | ---: |
| `go version` | 20.35 ms |
| `go env -json` | 48.33 ms |
| Python `-c pass` | 27.17 ms |
| Python importing the probe's modules | 45.26 ms |
| Python running the full package probe | 56.62 ms |

These ten-sample measurements include launching `/usr/bin/time`, the child process,
waiting and output capture. They are comparison points, not a decomposition to
subtract exactly: `go version` does different work from `go env`. The Python
installation had eight distributions. Large package environments may cost more.
They show why removing local allocations alone cannot solve probe latency.

## Replace repeated pattern compilation with exact checks

A regular expression is a pattern describing which text to match. The old code
built and compiled a pattern containing the observed temporary root on every
normalization. It then replaced matching suffixes repeatedly, because one pass
could miss an adjacent flag after consuming their shared separator.

For ordinary path prefixes, the new code scans for `-f`, checks the exact flag
and path prefix, consumes ASCII digits, then checks the exact destination and
separator. A `strings.Builder` collects unchanged text and the replacement. It
returns the original string if no recognized flag was found.

The final checks in
[`scratchDigits`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_scratch.go#L43-L63):

```go
if last == first || !strings.HasPrefix(flag[last:], destination) {
	continue
}
end := last + len(destination)
if end == len(flag) || scratchDelimiter(flag[end]) {
	return first, last
}
```

`first` is where the numeric suffix would begin; `last` is just after its digits.
Equal positions mean there were no digits. Only the original destination and
separator rule permit a replacement. For example, a suffix `123abc`, a different
root, or a destination `/elsewhere` stays unchanged. Changing meaningful compiler
settings must still change the key.

Separators are exactly the old ASCII choices: space, tab, single quote and double
quote, plus the start/end of the whole string. A newline is not interchangeable
with a space. This is a targeted string normalization, not a shell parser.
The scanner handles adjacent recognized flags without repeated passes.

The selection in
[`scratchFlags`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_scratch.go#L71-L86):

```go
if len(prefix) <= 4096 && utf8.ValidString(prefix) {
	return normalizeScratchFlags(flags, prefix), nil
}
```

The threshold selects an implementation; it does not reject longer paths.
Prefixes longer than 4096 bytes or containing invalid UTF-8 use the original
regexp algorithm, including its error behavior. This preserves unusual-input
handling instead of silently making a formerly refused path acceptable. Both
branches see the same cleaned path prefix. No shared state, worker or cached
probe output is introduced.

## Prove the recorded description stays the same

For a recognized flag, both algorithms replace only the same decimal digits with
`<scratch>`. Unknown flags remain unchanged. JSON decoding/encoding, all retained
settings, probe commands, output hashing and manifest serialization remain the
same. Consequently equal observed inputs still produce equal keys.

Tests compare the scanner directly with an independent copy of the old algorithm
across quotes, tabs, newlines, Unicode paths, adjacent flags, incorrect boundaries
and long paths. A 30-second differential fuzz run generated 98,733 executions;
“differential” means both implementations receive each generated input and their
answers are compared. Fuzzing samples possibilities rather than proving all of
them. An initial harness failure on invalid UTF-8 led to an explicit refusal
regression and the legacy fallback.

The CLI experiment created entries with unmodified `4cbf0d7`, then alternated ten
old/new pairs for Go, Python-module pytest and direct pytest. All **60 expected
hits** replayed without execution; full keys matched for each command form.
Python probe construction is unchanged and serves as a control. Existing tests
still exercise changed Go settings, unknown fields, fresh probe output,
cancellation and failures.

## Compare local savings with user-visible behavior

Six alternating process pairs ran 10,000 normalizations per process over real
`go env -json` output:

| Local normalization metric | Before | After |
| --- | ---: | ---: |
| Median time per normalization | 42.64 µs | 18.56 µs |
| Allocated bytes per normalization | 26,186 B | 8,308 B |
| Allocations per normalization | 161 | 91 |
| Benchmark-process CPU | 0.480 s | 0.225 s |
| Benchmark-process peak RAM | 15.97 MB | 15.97 MB |

The normalizer took about 56% less time and allocated about 68% fewer bytes in
this fixture. Process totals include benchmark startup, fixture probe and runtime
work; timed per-operation figures exclude that setup. Peak resident memory is a
maximum, so fewer allocations need not lower it.

Whole Go cache-hit medians were **98.69 → 103.86 ms**, CPU **0.080 → 0.085 s**, and
peak RAM **17.66 → 17.61 MB**. Latency ranges overlapped, and the unchanged probe
subprocess varied. Python/direct-pytest controls also varied. **No whole-hit
speedup or meaningful peak-RAM reduction is established.** The saved ~24 µs is too
small to separate from those fluctuations here. This is a local allocation/CPU
improvement, not a solution to the dominant subprocess cost.

The [measurement reference](../MEASUREMENTS.md#toolchain-probe-normalization-18-october-2026)
contains ranges, raw records, reproduction commands and scope. Cold storage,
energy use, syscall counts, large Python environments and other operating systems
were not measured.

## Keep freshness until a different contract is justified

An unchanged Python executable can have a changed package set. An unchanged Go
launcher can use changed settings. Caching probe results by executable hash would
miss these changes. Narrowing `go env` fields would change the information recorded
in keys. Disabling Python startup behavior could change which packages the probe
sees. None of these shortcuts belongs in a key-compatible optimization.

Parallel probing could overlap latency but retain the work, increase simultaneous
resource use, and require careful cancellation and error handling. Those options
were considered and deferred for this change. The next substantial probe project
needs representative environments and an explicit freshness/input contract; this
measurement does not justify a persistent daemon or cache on its own.

Revisit the scanner if Go changes its flag format or profiles no longer justify
its maintenance. Unknown forms should continue to remain unchanged. Stable keys
still do not prove deterministic test outputs or make observations atomic. The
[full review](../PHASE-REVIEWS.md#toolchain-probe-normalization-18-october-2026)
records all nine design questions and verification limits.

## Check your understanding

<!-- questions:start -->

### Why can a faster normalizer leave cache-hit latency unchanged?

**Level:** Newcomer

Local Go normalization falls from about 43 to 19 microseconds, but the fresh probe
takes tens of milliseconds. Why is a large whole-hit speedup unlikely?

<details>
<summary>Answer</summary>

One millisecond is one thousand microseconds. Saving about 24 microseconds removes
only a tiny part of the total work. The subprocess still runs, and variation in
its timing can be much larger than the saving. Local allocation evidence should
not be presented as an end-to-end latency improvement.

**Learn more:** [15. Measure toolchain probe overhead](../guide/15-measure-toolchain-probe-overhead.md)

</details>

### Which part of a scratch flag may change during normalization?

**Level:** Newcomer

A recognized compiler flag names `/tmp/go-build123`. Can Receipt discard the
whole flag to avoid temporary-path differences?

<details>
<summary>Answer</summary>

No. Only the numeric suffix in the recognized form becomes `<scratch>`. The root,
flag kind, destination and other settings remain. Removing the whole flag could
hide a meaningful change and permit reuse under a different compiler environment.
Unknown forms remain unchanged.

**Learn more:** [15. Measure toolchain probe overhead](../guide/15-measure-toolchain-probe-overhead.md)

</details>

### Does the scanner threshold reject long paths?

**Level:** Newcomer

The optimized path checks whether a prefix is at most 4096 bytes. What happens to
a longer prefix?

<details>
<summary>Answer</summary>

It uses the original regular-expression algorithm, including that algorithm's
errors. The threshold chooses an implementation; it does not introduce a new
path-length refusal. This keeps unusual input behavior while ordinary paths avoid
recompiling a pattern.

**Learn more:** [15. Measure toolchain probe overhead](../guide/15-measure-toolchain-probe-overhead.md)

</details>

<!-- questions:end -->
