# 12. Understand cache overhead

A second `receipt run -- go test ./...` can skip the tests, but it still reads
inputs, checks the toolchain and validates a saved result. If the tests take only
a moment, that checking may cost more than running them directly.

Start with `--timings` and a direct-versus-cached comparison, as described in
[Measure a slow run](../MEASUREMENTS.md). Choose the next tool from the slow stage,
not from the part of the implementation that happens to look complicated.

## Follow the work

```text
local-only hit: discover inputs -> read files -> probe tools -> build key
            -> read and check result -> print output -> save bookkeeping

remote hit: the same input checks -> connect -> request -> receive result
            -> validate -> print output -> save locally
```

With sharing configured, even a local copy needs a server quarantine check, as
Chapter 2 explains. The local-only path above assumes no shared server.

Elapsed time includes waiting. CPU time counts work performed by the processor.
For example, if an operation takes 20 ms but uses 2 ms of CPU, halving that CPU
work can save at most 1 ms in this simplified example. The remaining wait still
needs an explanation. These numbers illustrate the distinction, not a Receipt
benchmark.

A CPU profile samples the functions doing processor work. An allocation profile
shows where the program requests memory. A network trace records connection and
request events. Each answers a different question; a Go profile also excludes
work in separately running Git or database processes.

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

For source details, follow
[`hashFile`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/file_hash.go#L12).
The [file-hashing experiment](../PERFORMANCE.md#file-hashing) compares allocation
and timing while checking that the old and new calculations agree.

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
summary before deciding which side is slow. Reusing a connection can avoid setup,
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

### Does a sequential scan prove that an index is missing?

**Level:** Newcomer

A database plan reads a table sequentially. Is an index necessarily missing?

<details>
<summary>Answer</summary>

No. Reading all rows can be appropriate for a recount or a small table. An
index can help a selective lookup, but adds storage and update work. Measure the
complete operation: locating rows faster does not eliminate deletion, evidence
updates or commit costs.

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

<!-- questions:end -->
