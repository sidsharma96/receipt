# 7. Find where the time went

Alice uploads a small test result. It takes longer than she expects. Reading a
small file should be quick, but perhaps the request has not reached the file yet:
another writer or cleanup may hold the lock it needs.

Run the server with `--request-log` to give each request a server-generated ID and
one timing summary. The ID appears in its response header too. The ordinary
Receipt CLI does not show that header, so Alice needs a diagnostic client that
records the slow request's response headers. Its ID lets the operator find the
matching summary without logging her token or test output. The
[measurement guide](../MEASUREMENTS.md#find-one-server-request) demonstrates
capturing a response ID; a later health request cannot identify an earlier upload.

**Figure 6. Reading the request in parts, not guessing from its payload**

| Step | What happens |
|---|---|
| Credential check | Receipt checks whether this request may proceed. |
| Connection and locks | The request can wait here before opening any output file. |
| Metadata and files | Receipt validates the record and reads or writes its output files. |
| Response, then summary | The response reports its request ID; the handler's summary records measured stages. |

## Read one measurement carefully

In an experiment with the earlier upload-accounting algorithm, four clients repeatedly uploaded already-stored results
into a store with 128 descriptions. The mean database-lock call took about
55 milliseconds. That call includes a database round trip and time waiting for
other work to release the lock. It is inside the full request duration, not extra
time to add on top. [The measured bars](../FIGURES.md#figure-6-measured-wait)
show both on the same scale.

Some timers contain others: counting storage includes file enumeration, for
example. Adding every number would count some work twice. These summaries are
clues about where to investigate, not a complete accounting of every instruction.

Current uploads check running usage totals instead of recounting all stored
files. They still take turns under the exclusive storage lock while processing
their own output and committing database changes. Larger output can cost more,
and adding writers creates more waiters rather than parallel publication.

## Does Receipt help the real test command?

We ran a Python test selection with 15 passing tests. Five direct runs had a median
of 253 milliseconds. The first Receipt run took 592 milliseconds; it ran the
tests and scanned inputs before and afterward. Five saved-result hits had a
median of 127 milliseconds. They skipped pytest but still inspected inputs.

The [median](../GLOSSARY.md#median) is the middle duration after sorting the samples. These few ordered
samples, on one machine with warm filesystem caches, are not a promise for every
project. [The comparison](../FIGURES.md#figure-6-local-measurements) and
[repeatable commands](../MEASUREMENTS.md) let you inspect what was measured.

## What do diagnostics cost?

Each enabled timer reads the clock and updates a small record. The server writes
one summary after the request handler releases its storage locks. We deliberately
did not add a background queue: a slow destination can therefore delay handler
completion. The measured handler duration excludes the final log write.

These summaries omit secrets and captured output. That does not make all
stderr safe to publish: the test process can still print whatever it normally
prints. Diagnostics are also not a durable history of who submitted each result.

## Check your understanding

<!-- questions:start -->

### How do you find one slow request?

**Level:** Newcomer

Alice's diagnostic client captured the response headers from a slow upload. How
can the server operator find the timing for exactly that request? Would a later
health request's ID work instead?

<details>
<summary>Answer</summary>

The server must have `--request-log` enabled. Alice supplies the upload response's
`X-Receipt-Request-ID`, and the operator finds that ID in the server's standard-error
log. The stages separate credential checks, lock waiting and storage work.
The ordinary Receipt CLI does not expose the header; an HTTP diagnostic must
capture it. A health request gets its own ID, which cannot identify the upload.

**Learn more:** [7. Find where the time went](../guide/07-find-where-the-time-went.md) · [Read one measurement carefully](../guide/07-find-where-the-time-went.md#read-one-measurement-carefully)

</details>

### More writers behind one lock

**Level:** Practitioner

Four simultaneous uploads spend much of their time waiting for a lock. Why would adding more writer goroutines not make them finish faster, and what should you measure first?

<details>
<summary>Answer</summary>

Uploads take turns under one exclusive storage lock. Adding goroutines creates
more waiters unless the work done while holding that lock becomes shorter.
Inspect lock waiting separately from output reads, writes and database work.
Uploads check running totals; they do not inventory the whole store each time.

Any replacement must still prevent cleanup from deleting a file an upload is
about to reference and prevent two writers from both claiming the last free space.

**Learn more:** [Read one measurement carefully](../guide/07-find-where-the-time-went.md#read-one-measurement-carefully) · [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [Read the timings](../MEASUREMENTS.md#read-the-timings) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L73-L120)

</details>

### The cost of one upload

**Level:** Expert

Uploads now check running totals instead of recounting all stored files. Does
this make the whole upload constant-cost, or let several writers publish at once?

<details>
<summary>Answer</summary>

An upload holds the exclusive storage lock while it checks limits, prepares
metadata, writes or verifies output files and commits. It reads running totals
rather than scanning all stored entries. Its own output size and storage latency
still affect the cost.

Uploads are serialized, so increasing writer count does not create parallel
publication. Startup and collection recount usage. A failed commit can leave
unreferenced files that are charged at the next recount. See the selected
measurements for a scoped comparison with the earlier recount-per-upload design.

**Learn more:** [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [`readUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L209-L216) · [`reconcileUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L227-L238)

</details>

<!-- questions:end -->
