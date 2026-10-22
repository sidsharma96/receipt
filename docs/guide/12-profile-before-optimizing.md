# 12. Understand cache overhead

A second `receipt run -- go test -count=1 ./...` can skip the tests, but it still reads
inputs, checks the toolchain and validates a saved result. If the tests take only
a moment, that checking may cost more than running them directly.

In one investigation, five hits took a median of 359 ms. The fingerprint stage
had a median of 267 ms; lookup had a median of only 6 ms. Those are separate
medians, not parts of one request that can be added together. They suggested
investigating input checks before changing storage. They did not yet tell us
whether Git, file reads or toolchain probes were responsible.

The command was `go test -count=1 ./internal/strictjson` in Receipt's working
checkout, with warm caches. All five expected hits skipped test execution.
The [recorded scope and samples](../PERFORMANCE.md#file-hashing) explain the
limits. For your own workload, start with `--timings` and the
[direct-versus-cached comparison](../MEASUREMENTS.md#compare-reuse-with-running-the-tests).

## Follow the work

```text
Both paths start here:
  discover inputs -> read files
  -> probe tools -> build key

Local-only hit:
  read and check local result
  -> print output
  -> update bookkeeping

Remote hit, with no local entry:
  connect -> request
  -> receive and validate result
  -> attempt local save
  -> print output
  -> update bookkeeping
```

With sharing configured, even a local copy needs a server quarantine check, as
Chapter 2 explains. The local-only path above assumes no shared server. On a
remote hit, a failed local save is reported but does not prevent replay. If
printing later fails, a local copy may already exist. Bookkeeping updates the
entry's last-use time and the history used by `receipt explain`.

## Choose a measurement that answers the question

Elapsed time includes waiting. CPU time counts work performed by the processor.
For example, if an operation takes 20 ms but uses 2 ms of CPU, halving that CPU
work can save at most 1 ms in this simplified example. The remaining wait still
needs an explanation. These numbers illustrate the distinction, not a Receipt
benchmark.

A **benchmark** repeats a chosen operation and reports its cost. For example,
Receipt's file-input benchmark repeatedly describes one 8 MiB file. This isolates
file reading and hashing from the rest of a cache hit. It cannot tell us the cost
of running the whole CLI.

A **profile** attributes work inside the measured program to functions. A CPU
profile samples which functions are executing on the processor. An allocation
profile samples where memory is allocated—requested for new data. Use the first
to investigate processor work and the second to investigate temporary copies or
other memory use. A network trace instead records connection and request events.
A Go profile does not include the work of separately running Git or PostgreSQL
processes. A long wait with little CPU use needs timing or tracing evidence.

## Follow the allocation evidence to the code

The initial file-input allocation profile showed two large sources of allocated
bytes. This table selects and rounds the reported percentages; the function names
are from the implementation measured then. The file reader is
`os.readFileContents`; its caller is `fingerprint.fileInput`. `flat%` counts
allocations directly in that function; `cum%` also includes the functions it called:

| Function | flat % | cum % |
| --- | ---: | ---: |
| `readFileContents` | 49.9% | 49.9% |
| `fileInput` | 49.9% | 99.8% |

`fileInput` called the file-reading code, so its cumulative column includes that
code's allocation. Adding the two cumulative values would count the same work
twice. The earlier
[`fileInput` implementation](https://github.com/sidsharma96/receipt/blob/4fe7a9b600eec8245640b8df7338086189e00ba2/internal/fingerprint/manifest.go#L183-L205)
explains the split: reading the file
allocated one file-sized byte slice, then joining the permission prefix and file
contents allocated another. For a large file, those copies dominated allocated
bytes in this benchmark.

The allocation view is called `alloc_space` in Go's `pprof` tool. It estimates
bytes allocated throughout the profiled run, including objects later reclaimed.
A large total across many repetitions is not the amount of RAM needed at once.
The profile identified a place to reduce copying; it did not establish how much
faster a complete hit would become. The
[measurement guide](../MEASUREMENTS.md#profile-one-operation) shows how to collect
and read the same kind of profile from the current implementation.

## Read large files in chunks

Consider a file containing `abc` with permissions `0600`, a base-eight notation for permissions that allow only its owner to read and write it. Receipt hashes the
permission text, a zero-byte separator and the file bytes. It can feed them to
one hash calculation in pieces without changing their order:

```text
one large buffer:  [600, separator, abc] -> one hash
successive writes: [600, separator] then [ab] then [c] -> the same hash
```

The chunk boundaries are not additional input. Hashing each chunk separately and
combining the results would be a different algorithm.

Receipt uses a bounded buffer so a large file does not need a second whole-file
copy in memory. It still reads until the end; an earlier file-size observation
must not truncate a growing file. Reading in chunks saves temporary memory, but
it does not turn concurrently changing files into a stable snapshot.

Keeping a whole-file buffer was straightforward, but made temporary memory grow
with each file. Reading in chunks preserves the byte sequence while bounding
that buffer at 32 KiB. Simply skipping a file because its timestamp matches would
be cheaper still, but could miss changed contents. It would change the input
contract rather than improve the same calculation.

For source details, follow
[`hashFile`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/file_hash.go#L12).
The [file-hashing experiment](../PERFORMANCE.md#file-hashing) compares allocation
and timing while checking that the old and new calculations agree.

In the matched 8 MiB comparison, allocated bytes per operation fell from about
16.8 MB to 33.6 KB. The operation's median time fell from 13.24 ms to 9.20 ms.
But a separate complete-hit comparison had overlapping, noisy timings: its small
median change from 240.50 ms to 235.75 ms did **not** establish a reliable latency
improvement for users. Reading inputs, starting tool probes and the rest of the
command still cost time.

Compatibility mattered as much as allocation. Tests compared the old and new
hash calculations across file sizes and permissions; the complete-hit experiment
also checked that the new binary reused an entry written by the old one. Changing
hash bytes would have changed lookup keys, even if the tests still passed.

## Reuse only observations that remain valid

Receipt resolves the Git executable and reuses the initially discovered repository
root within one invocation. It still lists and reads input files freshly, and
rediscovers the root after test execution. A test can change files or repository
layout; reusing the earlier observations indefinitely would miss those changes.

A toolchain probe asks an installed tool for information such as its version or packages. These probes also run freshly. A Python environment can change while its
interpreter path stays the same. Remembering only an executable path or timestamp
would be cheaper, but would not establish that its packages remain unchanged.

Go's environment report contains a changing scratch-directory name. Receipt
normalizes only the known scratch-name pattern, while preserving the surrounding
flags. Removing arbitrary changing text would risk ignoring an actual compiler
setting. The goal is to remove irrelevant variation, not every reason for a miss.

## Account for memory and database work separately

Allocating 15 MB during a decode does not mean the process retains all 15 MB.
Go’s garbage collector reclaims memory that the program no longer uses, including temporary objects. A peak-memory measurement
answers a different question from bytes allocated over many operations.

Likewise, a database plan that scans a table is not automatically wrong. Counting
all rows may require reading them. An index is an extra lookup structure that helps find rows without scanning the whole table. It helps when it lets a query skip enough
work to repay its storage and update costs. Measure the complete operation:
finding rows faster does not remove the cost of deleting evidence or committing.

Shared uploads read running usage totals rather than inventorying the entire
store on each write. Cleanup and recounts still scan storage. Those occasional
operations can make other work wait, even though an ordinary upload avoids the
scan. See [locks and limits](../DESIGN-DEFENSE.md#locks-limits-and-cleanup).

## Separate network waiting from server work

A remote client may spend more time opening a connection and receiving bytes than
the server spends looking up a result. Match one client request to its server
summary with an [HTTP diagnostic that records the response ID](../MEASUREMENTS.md#find-one-server-request)
before deciding which side is slow. Reusing a connection can avoid setup,
but a short-lived CLI invocation usually starts a fresh process.

Compression can reduce transferred bytes while adding encoding and decoding work.
Whether it helps depends on response size and the connection. The
[network and compression examples](../PERFORMANCE.md#network-and-compression)
show why smaller responses did not establish a dependable improvement in the
measured deployment.

A different hosted runner may observe a faster path without a server change.
That is not evidence that a particular region would be better. A regional
comparison needs clients whose locations are deliberately controlled.

## Know when to stop

Keep an optimization only when it addresses a measured problem and preserves the
behavior users rely on. Compatibility tests can check recorded key bytes, but
cannot prove that every workload is deterministic. Repeat an experiment when a
result is small or noisy, and report failed attempts rather than selecting only
successful samples.

Receipt's current performance work is complete for now. Further changes should
follow a concrete user problem. The selected measurements explain the existing
choices; they are not a queue of optimizations waiting to be implemented.


## Check your understanding

<!-- questions:start -->

### Why can a cache hit still be expensive?

**Level:** Newcomer

The tests did not run, but a cached invocation was still slow. What work remains, and what should you measure first?

<details>
<summary>Answer</summary>

Receipt still discovers and reads inputs, probes the toolchain, reads and checks
the saved result, prints output and updates bookkeeping. Start with the timing
stages. Speeding up a database cannot fix a standalone local run dominated by
reading inputs.

**Learn more:** [Understand cache overhead](../guide/12-profile-before-optimizing.md)

</details>

### Is allocated memory the same as peak memory?

**Level:** Newcomer

A benchmark allocates about 15 MB each time it decodes a result. Does one hundred repetitions prove that the process needs 1.5 GB of RAM at once?

<details>
<summary>Answer</summary>

No. Allocation bytes count memory created over time; the garbage collector can reclaim earlier objects. Peak resident size measures a different quantity: memory resident in the process at its high point. Inspect both allocation profiles and process memory, and remember that separate PostgreSQL and child processes are not included in a Go heap profile.

**Learn more:** [Memory and database work](../guide/12-profile-before-optimizing.md#account-for-memory-and-database-work-separately)

</details>

### Why does streaming preserve the file hash?

**Level:** Newcomer

A file contains `abc`. Receipt used to hash the permission prefix and file bytes in one slice. It now writes the prefix, then `ab`, then `c` into one hash state. Why is the digest unchanged, and why must the old file size not limit the read?

<details>
<summary>Answer</summary>

The same SHA-256 state receives the same bytes in the same order; chunk boundaries are not additional input. Computing separate chunk digests and combining them would be different. The size hint may be stale if the file grows, so it sizes the temporary buffer but does not cap the total bytes read. Receipt reads until EOF. This preserves the hash calculation for stable files without making concurrent edits an atomic snapshot.

**Learn more:** [Streaming file hashing](../guide/12-profile-before-optimizing.md#read-large-files-in-chunks)

</details>

### Choosing a profiler

**Level:** Practitioner

An operation takes 20 ms elapsed but only 2 ms of CPU work. Would halving its CPU
work halve the elapsed time? Which kind of profile would instead help investigate
temporary file-sized copies in memory?

<details>
<summary>Answer</summary>

In that simplified example, halving CPU work saves at most 1 ms; waiting still
needs explanation. Timings or traces help locate waiting. An allocation profile
shows where memory is requested and can reveal large copies. A CPU profile
answers where processor work occurs. Neither includes work in separate Git or
database processes.

**Learn more:** [Choose a measurement](../guide/12-profile-before-optimizing.md#choose-a-measurement-that-answers-the-question)

</details>

### Reading an allocation profile

**Level:** Practitioner

In the earlier profile, `fileInput` had about 49.9% flat allocation and 99.8%
cumulative allocation. Its file-reading callee had about 49.9% flat allocation.
Why does adding the cumulative values mislead? What change did source inspection
suggest, and what behavior had to stay the same?

<details>
<summary>Answer</summary>

Cumulative allocation already includes called functions, so adding those rows
counts some allocations twice. Reading the whole file allocated one large slice;
joining it to the permissions prefix allocated another. Feeding the prefix and
successive file chunks into one hash state removed the need for those copies.
The hash still had to receive exactly the same bytes in the same order, so keys
and existing saved results remained compatible.

**Learn more:** [Follow the allocation evidence](../guide/12-profile-before-optimizing.md#follow-the-allocation-evidence-to-the-code)

</details>

### A faster operation inside a noisy command

**Level:** Expert

The 8 MiB file operation's median fell from 13.24 ms to 9.20 ms, while complete-hit
medians changed from 240.50 ms to 235.75 ms with overlapping ranges. Can we promise
users the same percentage speedup as the isolated operation? What did the
experiment establish more clearly?

<details>
<summary>Answer</summary>

No. File hashing is only part of the command, and the whole-hit samples do not
establish a reliable latency improvement. The clearer result is less temporary
allocation: about 16.8 MB to 33.6 KB per large-file operation. Compatibility checks
also supported preserving the hash calculation and reusing old entries. A user
latency claim needs repeated whole-command measurements of a representative
workload, including failures and timing variation.

**Learn more:** [Read large files in chunks](../guide/12-profile-before-optimizing.md#read-large-files-in-chunks)

</details>

<!-- questions:end -->
