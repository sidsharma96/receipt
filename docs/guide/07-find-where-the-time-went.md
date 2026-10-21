# 7. Find where the time went

Alice uploads a small test result. It takes longer than she expects. Reading a
small file should be quick, but perhaps the request has not reached the file yet:
another writer or cleanup may hold the lock it needs.

Run the server with --request-log to give each request a server-generated ID and
one timing summary. The ID appears in its response header too, so Alice can find
the matching summary without printing her token or test output into the log.

**Figure 6. Reading the request in parts, not guessing from its payload**

| Step | What happens |
|---|---|
| Credential check | Receipt checks whether this request may proceed. |
| Connection and locks | The request can wait here before opening any output file. |
| Metadata and files | Receipt reads and checks the stored result. |
| Response, then summary | The response reports its request ID; the handler's summary records measured stages. |

## Read one measurement carefully

In our local experiment, four clients repeatedly uploaded already-stored results
into a store with 128 descriptions. The mean database-lock call took about
55 milliseconds. That call includes a database round trip and time waiting for
other work to release the lock. It is inside the full request duration, not extra
time to add on top. [The measured bars](../PHASE-REVIEWS.md#figure-6-measured-wait)
show both on the same scale.

Some timers contain others: counting storage includes file enumeration, for
example. Adding every number would count some work twice. These summaries are
clues about where to investigate, not a complete accounting of every instruction.

## Does Receipt help the real test command?

We ran a CodeCraft selection with 15 passing tests. Five direct runs had a median
of 253 milliseconds. The first Receipt run took 592 milliseconds; it ran the
tests and scanned inputs before and afterward. Five saved-result hits had a
median of 127 milliseconds. They skipped pytest but still inspected inputs.

The [median](../GLOSSARY.md#median) is the middle duration after sorting the samples. These few ordered
samples, on one machine with warm filesystem caches, are not a promise for every
project. [The comparison](../PHASE-REVIEWS.md#figure-6-local-measurements) and
[repeatable commands](../MEASUREMENTS.md) let you inspect what was measured.

## What do diagnostics cost?

Each enabled timer reads the clock and updates a small record. The server writes
one summary after the request handler releases its storage locks. We deliberately
did not add a background queue: a slow destination can therefore delay handler
completion. The measured handler duration excludes the final log write.

These new summaries omit secrets and captured output. That does not make all
stderr safe to publish: the test process can still print whatever it normally
prints. Diagnostics are also not a durable history of who submitted each result.

## Check your understanding

<!-- questions:start -->

### How do you find one slow request?

**Level:** Newcomer

Alice's small upload took much longer than she expected. How can the server operator find the timing for exactly her request?

<details>
<summary>Answer</summary>

Start the server with `--request-log`. Each request then gets a server-generated ID, which the server returns in the `X-Receipt-Request-ID` response header, and the server writes one redacted timing summary per request to standard error. Alice reports the ID, and the operator finds the matching summary without anyone printing her token or test output. The summary splits the request into stages, such as the credential check and waiting for locks, so a long wait for the lock shows up separately from reading files.

**Learn more:** [7. Find where the time went](../guide/07-find-where-the-time-went.md) · [Read one measurement carefully](../guide/07-find-where-the-time-went.md#read-one-measurement-carefully)

</details>

### More writers behind one lock

**Level:** Practitioner

Four simultaneous uploads spend much of their time waiting for a lock. Why would adding more writer goroutines not make them finish faster, and what should you measure first?

<details>
<summary>Answer</summary>

Every upload takes the same exclusive lifecycle lock, so only one can do its work at a time. More goroutines only add more waiters. With `--request-log`, each request's summary separates time spent getting a database connection, waiting for the database lock and the file lock, running SQL, and reading or writing files. Measure what happens while the lock is held first. Inside it, an upload inventories every blob and sums the metadata tables, so its cost grows with the store.

A faster design would still need to keep cleanup from deleting files an upload is about to reference and keep two uploads from overspending the shared storage budget. Removing the lock without replacing those guarantees would trade waiting for wrong answers.

**Learn more:** [Read one measurement carefully](../guide/07-find-where-the-time-went.md#read-one-measurement-carefully) · [D15. What does one upload cost?](../DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow) · [Read the timings](../MEASUREMENTS.md#read-the-timings) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L73-L120)

</details>

### The cost of one upload

**Level:** Expert

What does one shared upload cost as the store grows, what serialises, and what throughput ceiling follows? Was this deliberate?

<details>
<summary>Answer</summary>

Each `Postgres.Put` holds the global exclusive lifecycle lock for its whole duration. Under it, Receipt reads the policy row and the `.store-id` marker, reads one row of running totals, checks with at most two `lstat` calls whether the output files already exist, inserts the record and evidence and updates the totals in `publishTx`, writes and syncs the two output files, adds their bytes to the totals and commits. None of that depends on the store's size. Reads take the shared lock, so they wait behind any upload.

Before 15 October 2026 each upload instead walked every output file and ran `metadataUsage`, which scans every result and evidence row, three times: O(B + M + E) time. On one laptop the median upload grew from 33.4 ms on an empty store to 131.8 ms at about 4,045 results. With running totals, the same test gave 19 to 24 ms at every size. Because uploads run one at a time, throughput is still at most one divided by that latency, about 50 uploads per second on that laptop. The code says so directly: it deliberately serialises writers for a small deployment. The totals cost a recount at every server start and CLI command, and a crash after file writes can leave orphan files uncounted until that recount or the next `shared gc --apply`.

**Learn more:** [D15. What does one upload cost, and how does that grow?](../DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow) · [`readUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L209-L216) · [`reconcileUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L227-L238)

</details>

<!-- questions:end -->

## Continue with CPU, memory and SQL plans

[Chapter 12](12-profile-before-optimizing.md) follows a measured local hit, inspects Go profiles, and compares PostgreSQL plans with and without a candidate retention index. It separates current behavior from proposed optimizations.
