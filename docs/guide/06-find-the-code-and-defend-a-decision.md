# 6. Find the code and defend a decision

The chapters explain behavior. To follow a change in the source, start with the part responsible for it. A Go package groups related source files. These packages run in one client or server process; a package is not a separate service.

## The main responsibilities

- internal/app reads the command and combines lookup, execution, verification, and publication. Start at [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L220-L322).
- internal/fingerprint describes inputs and calculates their key. Start at [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L100-L134) and [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L65-L76).
- internal/runner starts the command and captures output. Start at [`Run`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L49-L79).
- internal/cache stores results. Start at the [`Store`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L40-L44) interface; its other files implement HTTP, shared storage, and maintenance.
- internal/strictjson checks the declared JSON structure in [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61). Each decoded type owns its value rules.
- internal/auth defines service credentials and permissions in [`Principal`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L51-L60). Its PostgreSQL implementation, starting at [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L88), manages their lifetime.

These boundaries keep responsibilities identifiable. They do not make future work automatic. A cloud output store must still solve publication, deletion, and failure handling. Another project may need different permissions and account rules. Receipt's current internal packages are not a ready-made shared identity platform.

## Frequently performed work versus occasional work

People call frequently performed work the [hot path](../GLOSSARY.md#hot-path). In Receipt, input scanning and result lookup happen on normal commands. In credential mode, each server request also checks its credential. A cache hit avoids pytest, not all computation or disk access.

Occasional work is called the [cold path](../GLOSSARY.md#cold-path). Issuing credentials and collecting unused files fit here. Occasional does not mean cheap: cleanup can inspect the whole store and make other operations wait.

We have tested selected behaviors and failures, but have not established production latency or capacity. The [verification records](../PHASE-REVIEWS.md#verification-implementation-evidence-versus-documentation-checks) and each phase's evidence section say what actually ran. Their test names and execution counts are available for inspection rather than part of the introductory lesson.

## How to explain a choice to someone else

Start with the problem. Then describe the chosen behavior and one reasonable alternative. Finish with its cost, what we checked, and what would make us change our mind.

For cleanup, that becomes: a reader needs its output file to remain available; locks make cleanup wait; delaying deletion alone cannot establish that a paused reader has finished; the chosen lock can make other work wait; selected concurrency tests check this sequence. If measured waiting becomes unacceptable, we need a different protocol, not just a smaller timeout.

For permissions, it becomes: a stolen secret must stop working on later checks; fresh database lookups support that behavior; cached permissions can remain stale; queries cost work and depend on the database; selected tests cover denial and revocation. If queries become a measured bottleneck, any cache must state how long revoked access can remain accepted.

The full [nine-question reviews](../PHASE-REVIEWS.md) cover structures, architecture, coordination, pitfalls, costs, guarantees, alternatives, evidence, and reasons to revisit choices. Read them when you want to inspect a particular decision in depth.


## Check your understanding

<!-- questions:start -->

### Which package does what?

**Level:** Newcomer

You want to change how Receipt describes inputs. Which Go package do you open, and which packages hold the run flow, process execution and storage?

<details>
<summary>Answer</summary>

`internal/fingerprint` describes the inputs and calculates the key, starting at `Build` and `Manifest.Key`. `internal/app` reads the command line and combines lookup, execution, verification and publication, starting at `run`. `internal/runner` starts the test command and captures its output. `internal/cache` stores results locally, over HTTP and in shared storage. `internal/strictjson` checks JSON structure, and `internal/auth` defines credentials and permissions. All of these packages run inside one client or server process; a package is not a separate service.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L100-L134)

</details>

### The order of a run, from lookup to prune

**Level:** Practitioner

Walk through `receipt run -- uv run pytest` on a miss that succeeds, with a remote cache configured. In what order do lookup, execution, local storage, upload and pruning happen?

<details>
<summary>Answer</summary>

`run` parses flags and refuses more than one of `--bypass`, `--refresh` and `--verify`. It loads `.receipt.json`, resolves the cache directory and builds the manifest; if that fails, it runs the command uncached. `runSession.lookup` treats a locally quarantined key as a miss, then reads the local entry. With no local entry it asks the server with a GET, and a remote hit is stored locally and replayed.

On a miss, Receipt runs the command, streaming output while capturing a bounded copy. It builds the manifest again and calls the run stable only if that succeeds with the same key. It records the history file used by `receipt explain`. A successful, stable run goes to `runSession.publish`, which stores the result locally, uploads it to the server, and only then prunes the local cache. A failed upload is reported on stderr but does not change the exit code.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [D5. Why scan the inputs twice?](../DESIGN-DEFENSE.md#d5-why-scan-the-inputs-twice-and-what-can-the-two-scans-miss) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L220-L322) · [`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L216-L245)

</details>

### What would you change first?

**Level:** Expert

You may make three changes to Receipt before a second team starts using the shared cache. Which would you pick, and what does each cost?

<details>
<summary>Answer</summary>

A defensible choice, ordered by how soon it hurts:

1. Record a last-used time and expire by it instead of by `created_at`. Today a result used every day still expires 30 days after it was first stored, so the busiest results are deleted on schedule. The cost is a write on the read path, done outside the read lock or in batches, and a new crash argument for that timestamp.
2. Size the database pool from `--max-inflight`, or reject values above 16. Today the pool silently caps storage concurrency at 16 connections while the flag accepts 128, and authentication competes for the same connections. The cost is one more limit to configure and explain.
3. Add a scheduled, tested backup with a second copy of the recovery key. Today the only off-machine copy is a one-off archive from 13 October, and only one private key on one Mac can decrypt it. The cost is an operating task to schedule, monitor and rehearse.

(Keeping `RECEIPT_TOKEN` out of keys, giving credential issuance its own lock key and replacing per-upload scans with running totals were on this list until they were done on 15 October 2026.) Close runners-up are an administrator command to clear quarantine with a recorded reason and a purge command for expired credential rows before the 1,024 cap. Any answer that names the invariant each change must preserve, and its cost, is a good answer.

**Learn more:** [Part 4. Known gaps and pitfalls](../DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls) · [D15](../DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow) · [G21. Operations](../DESIGN-DEFENSE.md#g21-operations)

</details>

<!-- questions:end -->
