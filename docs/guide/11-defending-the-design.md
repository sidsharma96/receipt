# 11. Understand storage tradeoffs

Suppose the server has saved output file X, but loses power before saving the
database row that points to X. On restart, the file may exist without any result
referring to it. That is a different problem from a row referring to a missing
file: in the first case space is wasted; in the second, a result cannot be read.
Cleanup can reclaim the first kind of leftover; the second means a reader cannot obtain the saved output.

Start with the sequence from [Chapter 2](02-let-another-client-reuse-the-result.md):
Receipt prepares the output files, then commits the database record. A database
**transaction** groups database changes so they can be accepted or rolled back
together. It cannot undo a file written outside the database. A failed transaction can therefore leave a completed file behind.

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

## Compare alternatives against the same example

Storing output inside PostgreSQL would let a transaction commit both the result
and its output together. The leftover-file case would disappear from that save
path. The tradeoff is that every output byte becomes database storage and write
traffic. Receipt keeps large output in separate files, which can also be shared
by results with identical output. It must then coordinate publication, cleanup
and backups across the two stores. This compares the designs; it is not a claim
that one is faster for every workload.

## Why not give each key its own lock?

Suppose 10 MiB of storage budget remains and two uploads each need 8 MiB. These
numbers are illustrative. With independent locks for different keys, both uploads
could read the same free-space total and decide they fit. Together they need
16 MiB. Protecting individual results would not protect the shared budget.

Receipt makes uploads take turns through the budget check, file work and database
commit. The second upload sees the total after the first has changed it. The cost
is waiting even when their keys differ. Allowing overlap would require another
way to reserve shared space, plus coordination with cleanup so it cannot delete
files an upload is still preparing to reference.

Revisit this choice if measurements show writer waiting is a real user problem.
Then compare complete save and failure sequences, including retries and cleanup,
not just the time spent inserting one database row. The
[design reference](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) covers the current
coordination rules.

## Check your understanding

<!-- questions:start -->

### The file exists but the result does not

**Level:** Newcomer

An upload writes file X, then its database commit fails. Is X alone enough for a later client to find a saved result?

<details>
<summary>Answer</summary>

No. The database record connects the input key to its output. Without a committed record, the leftover file alone is not a reusable result. If no surviving record needs X, later collection can reclaim it.

**Learn more:** [Chapter 11](../guide/11-defending-the-design.md)

</details>

### Why two uploads for different keys wait for each other

**Level:** Practitioner

A reviewer asks: "Two uploads for different keys arrive together. Why does the second one wait?" Which shared resources do they both need, and why is a per-key lock not enough?

<details>
<summary>Answer</summary>

The two uploads share a storage budget and output files even when their result
keys differ. One exclusive lock makes their limit checks and publication take
turns. Each upload reads running totals rather than recounting the store.

Per-key locks would allow more overlap but would not by themselves protect the
shared budget or stop cleanup from deleting output about to receive a reference.
Those responsibilities need a replacement protocol before relaxing the lock.

**Learn more:** [11. Understand storage tradeoffs](../guide/11-defending-the-design.md#11-understand-storage-tradeoffs) · [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [`Postgres.checkBlobQuota`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L338-L381)

</details>

### Two uploads claim the last free space

**Level:** Expert

Two uploads for different keys each need 8 MiB, while 10 MiB remains. Each holds a separate per-key lock and reads the total before either updates it. Can both pass their checks?

<details>
<summary>Answer</summary>

Yes. Each sees enough room for itself, but together they need 16 MiB. A lock for each key does not protect their shared budget. Receipt’s exclusive storage lock makes the second upload check after the first commits. Any design allowing overlap needs another way to reserve the shared space and protect files from cleanup.

**Learn more:** [Chapter 11](../guide/11-defending-the-design.md)

</details>

<!-- questions:end -->
