# 11. Defending the design

Suppose the server has saved output file X, but loses power before saving the
database row that points to X. On restart, the file may exist without any result
referring to it. That is a different problem from a row referring to a missing
file: in the first case space is wasted; in the second, a result cannot be read.
A design explanation should tell us which state can occur and how we recover.

Start with the sequence from [Chapter 2](02-let-another-client-reuse-the-result.md):
Receipt prepares the output files, then commits the database record. A database
**transaction** groups database changes so they can be accepted or rolled back
together. It cannot undo a file written outside the database. Therefore saying
“we use a transaction” does not finish the explanation.

```text
Output file X saved
  |
  +-- database commit succeeds
  |     result now refers to X
  |
  +-- database step fails
        X may have no reference
        later cleanup can reclaim X
```

This is an illustrative failure sequence. A retry must still check existing
files and shared state; the leftover file alone is not a saved result. Cleanup
must also avoid deleting X while another upload is about to reference it.
Receipt uses a shared coordination rule—a lifecycle lock—to make those
operations wait for each other. The cost is that shared writers take turns,
even when they are saving different keys.

A reviewer can now ask useful questions: why files and database rows instead of
putting all output inside PostgreSQL? Why one writer at a time instead of a lock
for each key? What evidence shows the retry and cleanup paths work? Those are
questions about the same concrete sequence, not a vocabulary test.

## Use the reference to answer one decision at a time

The [design defense](../DESIGN-DEFENSE.md) develops these questions in detail.
Its sections explain the mechanism, alternatives, costs, code and limits. Read
the section matching the decision you are trying to explain; you do not need to
memorize every operating-system primitive before continuing the book.

For example, `flock` and `fcntl` are different operating-system interfaces for
file locking. `fsync` requests that buffered changes reach durable storage. A
hard link gives an existing file another name. Each mechanism solves a smaller
part of publication or coordination; none makes the entire save operation
indivisible across files and the database. The reference explains the relevant
rules before comparing them.

When defending a choice, trace a normal request first, then stop it at a failure
point. Say what remains in each store, what a retry does, and which operation
must wait. Compare an alternative against that same example. Finally, identify
what would make us change the choice—for instance, measured writer waiting might
justify a more complicated coordination scheme. A new scheme would still have
to protect both storage budgets and files awaiting database references.

The reference also records current limits. Cached results expire 30 days after
they were first stored, however often they are used, and a quarantined key cannot
be cleared. Gaps fixed since the list was written, such as the CI token's hash
being part of every CI cache key, are recorded with their dates and tests. Treat
those dated records as evidence of a particular change, not a guarantee that
all possible failure cases have been tested.

## Check your understanding

<!-- questions:start -->

### Does an unchanged key prove the tests still pass?

**Level:** Newcomer

The key is the same as yesterday's. Does that prove the tests would produce the same output if you ran them today?

<details>
<summary>Answer</summary>

No. The key describes the inputs Receipt records, not every influence on a test. A test that asks a website for today's price, reads the clock, uses randomness or reads a file outside the recorded set can behave differently under the same key. The manifest type's own comment says it "does not prove output determinism". Receipt's answers to this gap are `--verify`, which reruns and compares, and quarantine, which stops reuse once two observations disagree. They expose some problems; they do not remove the uncertainty.

**Common wrong answer.** "Yes, the key covers all the inputs." It covers the declared inputs only.

**Learn more:** [Matching inputs do not prove matching behavior](../guide/01-remember-a-successful-run.md#matching-inputs-do-not-prove-matching-behavior) · [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [`Manifest`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L53-L61)

</details>

### Why two uploads for different keys wait for each other

**Level:** Practitioner

A reviewer asks: "Two uploads for different keys arrive together. Why does the second one wait?" Which shared resources do they both need, and why is a per-key lock not enough?

<details>
<summary>Answer</summary>

Both uploads may add files to the one shared blob directory, and both draw on the same storage budget. Two keys can also refer to the same blob, because identical output is stored once. The quota check must see a total that no other upload is changing at the same time, and `shared gc` must not delete a blob that an upload is about to reference. A lock per key coordinates neither of those decisions, since the shared quantities span keys.

Receipt meets both needs with one exclusive lifecycle lock for every upload. The cost is that uploads run one at a time. Each upload's own work no longer grows with the store, because it checks running totals instead of recounting (since 15 October 2026), but the throughput ceiling is still one upload at a time. That is a deliberate choice for a small deployment, not a high-throughput design.

**Learn more:** [11. Defending the design](../guide/11-defending-the-design.md#11-defending-the-design) · [D12. Why does one lock serialize every shared writer?](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`Postgres.checkBlobQuota`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L338-L381)

</details>

### Why READ COMMITTED is enough

**Level:** Expert

`publishTx` counts usage, checks quotas and then inserts, all under READ COMMITTED. Why is that not a write-skew bug, and what would break if someone narrowed the global lock to per-key locks?

<details>
<summary>Answer</summary>

Every writer and every maintenance operation holds the exclusive lifecycle lock for its whole transaction. While one holds it, no other transaction changes the result, evidence or policy tables, so each READ COMMITTED statement sees the same data a snapshot would. Two uploads cannot both read 10 MiB of free budget and both add 8 MiB, because the second waits for the first to commit. Correctness comes from the lock, not from the isolation level. The `SELECT ... FOR UPDATE` in `publishTx` never waits today; it is kept so the row logic stays correct if the lock is narrowed.

With per-key locks, two uploads for different keys would run together. Both could pass the budget check and overshoot it, and a collector could decide output file X was unreferenced while an upload for another key was about to reference it. SERIALIZABLE would catch the database half of that, but it cannot see the filesystem, so it does not protect output files from the collector.

**Learn more:** [D14. Why is READ COMMITTED enough?](../DESIGN-DEFENSE.md#d14-why-is-read-committed-enough) · [D12](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L268-L352)

</details>

<!-- questions:end -->
