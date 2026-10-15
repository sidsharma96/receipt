# 11. Defending the design

The chapters so far follow one test run and explain what happens. A reviewer
asks a different kind of question: why this choice, what else could work, what
it costs, and what breaks when it fails. Suppose someone asks why Receipt uses
`flock` and not `fcntl` locks, or what happens if the machine loses power
between saving output file X and saving the database record that points to it.
The answer needs the operating-system and database rules underneath the code.

The [design defense](../DESIGN-DEFENSE.md) is organised as those questions, in the
order a reviewer would ask them. It starts from first principles: content
hashing, file locks, `fsync` and hard links, PostgreSQL transactions and
advisory locks, and bearer tokens. Then it walks each decision: the cache key,
local publication, the shared store's locks, the cost of an upload, credentials,
the threat model and the deployment. Each section names the alternatives, the
cost with measured numbers where they exist, the failure behaviour and links
to the exact lines of code.

It also lists the known gaps honestly. For example, cached results expire 30
days after they were first stored, however often they are used, and a
quarantined key cannot be cleared. Gaps fixed since the list was written, such
as the CI token's hash being part of every CI cache key, are recorded with
their dates and tests.

## Check your understanding

<!-- questions:start -->

### Does an unchanged key prove the tests still pass?

**Level:** Newcomer

The key is the same as yesterday's. Does that prove the tests would produce the same output if you ran them today?

<details>
<summary>Answer</summary>

No. The key describes the inputs Receipt records, not every influence on a test. A test that asks a website for today's price, reads the clock, uses randomness or reads a file outside the recorded set can behave differently under the same key. The manifest type's own comment says it "does not prove output determinism". Receipt's answers to this gap are `--verify`, which reruns and compares, and quarantine, which stops reuse once two observations disagree. They expose some problems; they do not remove the uncertainty.

**Common wrong answer.** "Yes, the key covers all the inputs." It covers the declared inputs only.

**Learn more:** [Matching inputs do not prove matching behavior](../guide/01-remember-a-successful-run.md#matching-inputs-do-not-prove-matching-behavior) · [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [`Manifest`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L52-L60)

</details>

### Why two uploads for different keys wait for each other

**Level:** Practitioner

A reviewer asks: "Two uploads for different keys arrive together. Why does the second one wait?" Which shared resources do they both need, and why is a per-key lock not enough?

<details>
<summary>Answer</summary>

Both uploads may add files to the one shared blob directory, and both draw on the same storage budget. Two keys can also refer to the same blob, because identical output is stored once. The quota check must see a total that no other upload is changing at the same time, and `shared gc` must not delete a blob that an upload is about to reference. A lock per key coordinates neither of those decisions, since the shared quantities span keys.

Receipt meets both needs with one exclusive lifecycle lock for every upload. The cost is that uploads run one at a time and each does work that grows with the store. That is a deliberate choice for a small deployment, not a high-throughput design.

**Learn more:** [11. Defending the design](../guide/11-defending-the-design.md#11-defending-the-design) · [D12. Why does one lock serialize every shared writer?](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`Postgres.checkBlobQuota`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L303-L338)

</details>

### Why READ COMMITTED is enough

**Level:** Expert

`publishTx` counts usage, checks quotas and then inserts, all under READ COMMITTED. Why is that not a write-skew bug, and what would break if someone narrowed the global lock to per-key locks?

<details>
<summary>Answer</summary>

Every writer and every maintenance operation holds the exclusive lifecycle lock for its whole transaction. While one holds it, no other transaction changes the result, evidence or policy tables, so each READ COMMITTED statement sees the same data a snapshot would. Two uploads cannot both read 10 MiB of free budget and both add 8 MiB, because the second waits for the first to commit. Correctness comes from the lock, not from the isolation level. The `SELECT ... FOR UPDATE` in `publishTx` never waits today; it is kept so the row logic stays correct if the lock is narrowed.

With per-key locks, two uploads for different keys would run together. Both could pass the budget check and overshoot it, and a collector could decide output file X was unreferenced while an upload for another key was about to reference it. SERIALIZABLE would catch the database half of that, but it cannot see the filesystem, so it does not protect output files from the collector.

**Learn more:** [D14. Why is READ COMMITTED enough?](../DESIGN-DEFENSE.md#d14-why-is-read-committed-enough) · [D12](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L232-L314)

</details>

<!-- questions:end -->
