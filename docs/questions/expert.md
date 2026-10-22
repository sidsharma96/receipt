# Expert questions

Choose a question, predict the result, then open the answer. Links lead to the
relevant explanation and implementation.

### The CI token in every key

**Level:** Expert

Receipt's CI renews its credential. Nothing else changes. Does renewal alone invalidate its saved test results?

<details>
<summary>Answer</summary>

Renewal alone leaves the recorded inputs unchanged. Receipt excludes its own
`RECEIPT_TOKEN` from the key and removes it from the child command's environment.
Other environment changes may still cause a miss. Removing the variable is not
a sandbox against hostile same-user processes.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L228-L241) · [`withoutToken`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L42-L48)

</details>

### Two machines, one commit

**Level:** Expert

Alice and a CI runner check out the same commit and run `receipt run -- go test ./...`. Will they compute the same key? What decides it?

<details>
<summary>Answer</summary>

Usually not. The manifest records the absolute Git root and the absolute working directory, and each file input is named by its absolute path. The executable input records the absolute path of the resolved `go` binary and the SHA-256 of its bytes. Every environment variable is hashed by value, including `HOME`, `PATH`, `USER` and shell session variables such as `SHLVL`. The `go env -json` probe adds the Go version and settings. The keys match only if the checkout path, tool paths and bytes, environment values and platform are all identical.

Receipt's own CI gets cross-runner hits by building that sameness on purpose: a fixed checkout path, a fixed `HOME`, a small explicit environment that includes the runner image version, and declared extra inputs for its one Go module, `git`, `dash` and the client binary.

**Common wrong answer.** "Yes, because the key is derived from the repository's file contents." The file contents are only part of the manifest. Paths and the environment are in it too, so even two terminals on one laptop can disagree.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [What is actually cached?](../RECEIPT-CI.md#inputs-and-environment) · [CI environment helper](https://github.com/sidsharma96/receipt/blob/main/tools/selfci/main.go)

</details>

### The shared server is down

**Level:** Expert
<!-- check: guide/02-let-another-client-reuse-the-result.md -->

A developer has a valid local entry for a key and a `remote` configured. The shared server is unreachable. Does Receipt replay the local entry?

<details>
<summary>Answer</summary>

No. With a remote configured, a local hit is replayed only after `GET /v1/status/<key>` returns 204. Any other outcome, including a timeout, a connection error or a 5xx, makes `lookup` print "shared status unavailable or quarantined; executing instead of local replay" and run the tests. A 409 also writes a local quarantine marker. The rule is fail-closed: another client may have quarantined the key since this copy was saved, and the client cannot know that while the server is down. A successful run then tries to upload; that failure is reported and the local entry stays. To work offline with local hits, run without `--remote` and without a configured `remote`.

**Common wrong answer.** "Yes, the local cache is the fast path and the server is only a fallback." Local reuse is subordinate to the shared quarantine state whenever sharing is configured.

**Learn more:** [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L69-L103)

</details>

### A server without PostgreSQL

**Level:** Expert

Someone runs `receipt serve` without `--postgres`, using the disk store and the legacy shared token, and points clients at it. What changes for clients?

<details>
<summary>Answer</summary>

Three things. First, the disk store has no shared quarantine, so `GET /v1/status/<key>` and `POST /v1/conflicts/<key>` answer 501. `Remote.Check` treats 501 as an error, so every client with a remote configured stops replaying its local hits and runs the tests. A client with no local entry can still get a remote hit through `GET /v1/entries/<key>`. Second, there is no conflict detection. The server stores uploads with the same `Disk.Put` the client uses locally, which keeps an existing valid entry and returns success, so a differing upload gets 204 and the first writer wins silently. Third, there are no quotas, no retention and no `shared gc`.

**Common wrong answer.** "It behaves the same, only without scoped credentials." The legacy mode also loses the shared status check and conflict detection, which changes both hit behaviour and safety.

**Learn more:** [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`cacheAPI.shared`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L264-L273) · [`putEntry`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L347-L370)

</details>

### Two CI runners race on one key

**Level:** Expert

Two CI runners miss on the same key at the same moment and both run `go test -count=1 ./...` successfully. What happens on the PostgreSQL server, and how does the key become usable again?

<details>
<summary>Answer</summary>

Both runners upload. The uploads take the exclusive lifecycle lock one after the other. The first inserts its record. The second computes the output digest, which is SHA-256 over the stdout hash, the stderr hash and the exit code. `go test` prints per-package timings such as `ok receipt/internal/app 1.234s`, so the stdout bytes almost always differ. `publishTx` sees a different digest, stores the new observation as evidence, sets `quarantined = TRUE`, commits and returns 409. The second client writes a local quarantine marker and removes its local entry. From then on every status check and fetch for that key returns 409, so nobody reuses it.

Nothing makes the key usable again. There is no command to clear quarantine, `shared prune` keeps quarantined keys and their evidence forever, and local markers are never pruned. The key stops mattering only when an input changes and produces a new key, or when an operator deletes the row and the marker by hand.

**Common wrong answer.** "The second upload overwrites the first, or is ignored because both passed." Receipt compares bytes, not test verdicts, and treats any disagreement as a broken input contract.

**Learn more:** [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L268-L352)

</details>

### The cost of one upload

**Level:** Expert
<!-- check: guide/07-find-where-the-time-went.md -->

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

### Database connection lost mid-upload

**Level:** Expert

An upload is writing output files when its request is cancelled, or its database connection drops. What stops `shared gc` in another process from deleting those files underneath it?

<details>
<summary>Answer</summary>

Two separate mechanisms. The transaction is begun with `context.WithoutCancel`, so a cancelled request context does not make `database/sql` roll back and release the advisory lock while file work is still running. The transaction ends only when the deferred `Rollback` or `Commit` runs, after the file work returns. Individual queries still use the request context.

If the connection itself dies, PostgreSQL releases the advisory lock at once, and a collector could acquire it. But every lifecycle operation also holds a `flock` on `blobs/.lifecycle.lock`, and that lock belongs to the open file, not the database session. The uploading process holds it until it closes the file, so the collector waits for the files. The tests `TestLifecycleCancellationKeepsFilesystemExclusion` and `TestDatabaseConnectionLossKeepsFilesystemExclusion` cover both cases. The limit is that `flock` coordinates one host only.

**Learn more:** [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L73-L120) · [`lockBlobDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/lifecycle_unix.go#L15-L57)

</details>

### Is credential administration independent of storage?

**Level:** Expert

An operator issues a new credential while CI is uploading results. Does issuance interact with cache traffic at the lock level?

<details>
<summary>Answer</summary>

Issuance uses a separate lock from cache storage. Two issuers still coordinate
so they cannot both claim the final credential slot.

The administration command first opens shared storage. That initialization takes
the storage lock once, so `receipt auth issue` can wait behind an upload or
collection. The issuance step and the complete command have different boundaries;
calling the whole command independent of storage would hide that wait.

**Learn more:** [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74)

</details>

### Credential lifecycle limits

**Level:** Expert

What limits apply to issued credentials, what happens after many renewals, and what happens to an upload in flight when its credential is revoked?

<details>
<summary>Answer</summary>

A credential lives between 1 second and 720 hours and carries one to four explicit scopes. The table may hold at most 1,024 rows, and that count includes expired and revoked rows. `Issue` counts every row and returns a capacity error at 1,024. The `auth` command offers issue, list and revoke, but no purge, so after roughly a thousand issuances, for example regular renewals plus short-lived smoke-test credentials, issuance fails until someone deletes rows in SQL.

Revocation sets `revoked_at` and is checked when each request authenticates. There is no permission cache, so the next request is refused with 401. An upload that already passed authentication is a snapshot of that moment and finishes. The test `TestRevocationDoesNotCancelAnAlreadyAuthenticatedUpload` records this.

**Common wrong answer.** "Expired and revoked credentials free their slots, and revocation cancels in-flight work." Neither is true here.

**Learn more:** [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [Bounds and operational costs](../SERVICE-CREDENTIALS.md#storage-and-limits) · [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74)

</details>

### Poisoning with a publish credential

**Level:** Expert

An attacker holds a `publish`-scoped credential. What can they do to other users' test results, what limits the damage, and which CI rule exists because of this?

<details>
<summary>Answer</summary>

They can forge a passing result. The key is the hash of a manifest, so anyone who can reconstruct a victim's manifest can compute the key and upload a result with exit code 0 and any stdout they like. The server checks structure and consistency: strict JSON, size limits, the checksum, that the key matches the manifest and that the exit code is 0. It cannot tell whether tests ran. If nobody has stored that key yet, the forgery is accepted and every client that later misses locally fetches and replays a fake pass. The first writer wins, so a forgery placed early sticks.

Detection comes only from disagreement: an honest upload with different output for the same key, or a `--verify` run, quarantines it. Scopes let most callers hold `read` only. CI never gives private-cache credentials to pull-request code: the cached job runs only on pushes to trusted branches, and an independent uncached job always runs the full suite, so CI correctness never depends on the cache.

**Learn more:** [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [Why keep the uncached job?](../RECEIPT-CI.md#review-a-failed-run) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L74)

</details>

### What a read credential learns

**Level:** Expert

A caller holds only `read`. What can it learn from the manifests it fetches? Is "environment values are never stored in plaintext" a meaningful privacy guarantee?

<details>
<summary>Answer</summary>

A fetched result includes its manifest: checkout paths, command arguments,
input-file names, executable path and environment-variable names with value hashes.
Guessable environment values can be tested offline by hashing candidate values.
Hashes are not encryption.

Current clients omit `RECEIPT_TOKEN`; older saved manifests may contain its digest.
All readers share access to the same store, so only grant read permission to
callers trusted with these records and the test output.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L228-L241) · [`digest`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L158-L161)

</details>

### Overload behaviour

**Level:** Expert

How does the server behave under overload? Name both admission layers, the status codes, the timeouts that apply, and the hidden limit that caps concurrency below `--max-inflight`.

<details>
<summary>Answer</summary>

Storage slots are a buffered channel sized by `--max-inflight`, default 16, range 1 to 128, and 2 in production. Acquisition does not block: a full channel answers 503 with `Retry-After: 1` and counts a rejection. In credential mode a second channel of the same size bounds authentication lookups; when it is full the answer is 503 "authentication busy", and a database error during authentication is also 503. A bad token gets 401 and a missing permission 403. Admitted storage work has a 25-second deadline, authentication has 2 seconds, the HTTP server has 5 s header, 30 s read, 30 s write and 60 s idle timeouts, and the client gives up after 10 seconds.

The hidden limit is the database pool: 16 open connections, shared by the server and every CLI command. Each storage operation pins one connection for its whole duration, so at most 16 run at once whatever `--max-inflight` says, and authentication can time out waiting for a connection. Receipt rejects rather than queues so clients fall back to running their tests within milliseconds; overload costs speed, not correctness.

**Learn more:** [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [`SetMaxOpenConns`](https://github.com/sidsharma96/receipt/blob/main/internal/app/shared_command.go#L27-L34)

</details>

### Retention and evidence in maintenance

**Level:** Expert

What do `shared prune` and `shared gc` each delete? Is retention based on creation or last use, and why is it safe for gc to delete `.pending-` files?

<details>
<summary>Answer</summary>

Both commands are dry runs unless given `--apply`, and both take the exclusive lifecycle lock. `shared prune --older-than D`, 720 hours by default, deletes non-quarantined result rows whose `created_at` is older than the cutoff, together with their evidence. It never touches files, so their output files become orphans. Quarantined keys and their evidence are always kept. `created_at` is set by the first upload and never refreshed by a hit, so retention is first-in, first-out: a result used every day still expires 30 days after it was stored. The local cache, by contrast, evicts by file modification time, which a hit refreshes.

`shared gc` validates every record, building a map of referenced output hashes, and stops before deleting anything if one record is malformed. It then deletes every inventoried file not in the map, including `.pending-` temporary files. That is safe because the exclusive lock means no upload is in flight, so any pending file is a leftover from a crashed or failed write. The usual order is prune, then gc.

**Common wrong answer.** "Retention is least recently used." That is true of the local cache only.

**Learn more:** [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [`Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L471-L510)

</details>

### Why stop the service to back it up

**Level:** Expert

The backup script stops Receipt before running `pg_dump` and archiving the cache directory. `pg_dump` already gives a consistent snapshot. Why stop the service?

<details>
<summary>Answer</summary>

Because a result spans two stores. `pg_dump` is consistent only inside PostgreSQL. A `tar` of `/var/lib/receipt` taken while the server runs could miss an output file that a record committed during the copy refers to, or capture a half-written `.pending-` file. The backup does not take the lifecycle lock, so the only way to get one consistent cut across the database and the files is to stop every writer. The script also refuses to continue if any other `receipt` process is running, because a CLI maintenance command writes too. It then dumps the database and the roles, archives the state, binary and configuration, records checksums, and restarts the service in a `finally` block before encrypting and uploading. The measured stop-to-start interval in the R2 drill was 1.306 seconds.

**Common wrong answer.** "Stopping is unnecessary because `pg_dump` is transactionally consistent." The files are outside that transaction.

**Learn more:** [Backup and recovery](../DIGITALOCEAN-SETUP.md#backup-and-recovery) · [`pgrep`](https://github.com/sidsharma96/receipt/blob/88b6edf3c78c1f681735cb893c355b2ab45ae562/deploy/digitalocean/backup-r2-drill.py#L24-L28)

</details>

### Why the restore drill bind-mounts

**Level:** Expert

The restore drill bind-mounts the extracted backup over `/var/lib/receipt` inside a private mount namespace, instead of starting a test server against the extracted copy where it sits. What would fail otherwise, and why does that check exist?

<details>
<summary>Answer</summary>

The storage policy row in the restored database records the absolute blob directory, `/var/lib/receipt/cache/blobs`, and a random store identity. Every storage operation compares the server's blob directory with that path and the `.store-id` file with that identity. A server started against `/var/backups/.../extracted/var/lib/receipt/cache` would refuse every request with "blob directory does not match database storage policy". The binding exists so that one database's `shared gc` can never treat another store's files as unreferenced and delete them.

So the drill makes the restored files appear at the original path without changing what the live server sees. It runs under `unshare --mount --propagation private`, refuses to continue unless its mount namespace differs from PID 1's, and bind-mounts the restored directory there. It also uses a separate restored database and another loopback port, so production rows and the production port are never touched. The cost is that recovery onto a new machine must restore the files to the same absolute path, or the policy row must be edited by hand.

**Learn more:** [Backup and recovery](../DIGITALOCEAN-SETUP.md#backup-and-recovery) · [`Postgres.policy`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L172-L194) · [`mount --bind`](https://github.com/sidsharma96/receipt/blob/88b6edf3c78c1f681735cb893c355b2ab45ae562/deploy/digitalocean/restore-check-20261012.sh#L7-L8)

</details>

### Exact semantics of verify and refresh

**Level:** Expert

What are the exit codes of `--verify` on a hit for a match, a mismatch and changed inputs? When, exactly, does `--refresh` quarantine a key?

<details>
<summary>Answer</summary>

On a hit, `--verify` runs the tests again and scans the inputs again. If the key changed between the two scans, it prints "verification inconclusive", quarantines nothing and exits 1. If the exit code, stdout or stderr differ byte for byte, or the output exceeded the capture limit, it writes a local quarantine marker, removes the local entry, reports the conflict's hashes to the server and exits with the new run's exit code, or 1 if the new run exited 0. If everything matches it exits 0. On a miss, `--verify` behaves like a normal run.

`--refresh` skips reuse but reads the local entry for the key. It quarantines in one case only: the fresh run fails, a cached success for the same key existed locally, and the inputs did not change during the run. A successful refresh removes the old local entry and stores the new one, even if the output differs. The upload that follows can still meet a different stored result on a PostgreSQL server, which answers 409 and makes the client quarantine locally. At most one of `--bypass`, `--refresh` and `--verify` may be given.

**Common wrong answer.** "A successful refresh whose output differs from the cached result is a conflict and quarantines locally." Locally it simply replaces the entry; only a failure after a recorded success quarantines.

**Learn more:** [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay) · [`verify`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L164-L192) · [`failed`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L194-L215)

</details>

### Replay is not a live run

**Level:** Expert

From the user's point of view, in what ways is a replayed result different from a live run?

<details>
<summary>Answer</summary>

The replay writes all of the saved stdout and then all of the saved stderr, so the original interleaving of the two streams is lost. The child ran with pipes, not a terminal, so there were no colours or terminal-dependent formatting to save. Its standard input was `/dev/null`. Durations and timestamps in the output are the original run's. Side effects do not happen: files the tests wrote, coverage reports, JUnit XML and network calls are absent. Receipt adds a "hit" line on stderr. Output over 8 MiB per stream was never stored, so such a run never replays. The exit code is always 0, because only successes are stored.

**Learn more:** [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay) · [`runSession.replay`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L142-L162)

</details>

### Ctrl-C during a run

**Level:** Expert

The user presses Ctrl-C while `receipt run` is executing tests. How are child processes cleaned up, what exit code is returned, and is anything stored? What would you change?

<details>
<summary>Answer</summary>

Receipt cancels the command context and signals the Unix process group so child
processes can terminate together. It gives termination a grace period before
sending a kill signal to a remaining group. A process that escapes the group is
outside that mechanism.

A crash not caused by Receipt is reported using the signal-derived exit status.
Cancellation and crashes are not stored as successful results. Graceful shutdown
can take time when a process ignores the initial signal.

**Learn more:** [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay) · [`configureProcess`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process_unix.go#L16-L46)

</details>

### A build script one dependency away

**Level:** Expert
<!-- check: guide/08-check-what-a-rust-test-depends-on.md -->

A Rust package's own dependencies are clean, but one of its dependencies has a dev-dependency that uses a build script. Does Receipt refuse to cache `cargo test --frozen` for the root package?

<details>
<summary>Answer</summary>

No. Receipt walks the resolved graph from the root with a depth-first search. It follows normal and build dependency edges everywhere, but dev-dependency edges only from the root package, because Cargo builds a dependency's dev-dependencies only when testing that dependency itself. The package with the build script is never visited, so `cargoPackagePolicy` never sees it. If the build script were on a normal or build edge anywhere in the walked graph, Receipt would refuse reuse before looking up the cache and run Cargo uncached. Passing the check still does not prove the tests read nothing else: ordinary tests can open files or call services Receipt does not record.

**Common wrong answer.** "Any build script anywhere in `cargo metadata` output forces uncached execution." Only the packages reachable through the edges Cargo would compile for this test are checked.

**Learn more:** [Cargo](../CARGO.md) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoDependencies`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L128-L150)

</details>

### Why READ COMMITTED is enough

**Level:** Expert

`publishTx` counts usage, checks quotas and then inserts, all under READ COMMITTED. Why is that not a write-skew bug, and what would break if someone narrowed the global lock to per-key locks?

<details>
<summary>Answer</summary>

Every writer and every maintenance operation holds the exclusive lifecycle lock for its whole transaction. While one holds it, no other transaction changes the result, evidence or policy tables, so each READ COMMITTED statement sees the same data a snapshot would. Two uploads cannot both read 10 MiB of free budget and both add 8 MiB, because the second waits for the first to commit. Correctness comes from the lock, not from the isolation level. The `SELECT ... FOR UPDATE` in `publishTx` never waits today; it is kept so the row logic stays correct if the lock is narrowed.

With per-key locks, two uploads for different keys would run together. Both could pass the budget check and overshoot it, and a collector could decide output file X was unreferenced while an upload for another key was about to reference it. SERIALIZABLE would catch the database half of that, but it cannot see the filesystem, so it does not protect output files from the collector.

**Learn more:** [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L268-L352)

</details>

### Durability after a power loss

**Level:** Expert

The server loses power just after an upload commits, and later a client's laptop loses power just after `--verify` quarantines a key. What can be lost in each case?

<details>
<summary>Answer</summary>

Receipt syncs output files and directory entries before reporting publication
success. On the client, quarantine and entry removal also sync their directories.
These operations reduce the gap between an operation returning and its directory
change reaching durable storage, assuming the filesystem and device honor sync.

They do not make every file and database update one transaction. A failed database
commit can leave unreferenced output. Inspect failures and retry cleanup; do not
infer complete crash recovery from a single successful save.

**Learn more:** [Local storage](../DESIGN-DEFENSE.md#local-storage) · [`MakeDirs`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/dirsync.go#L24-L50) · [`quarantine`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L222-L230)

</details>

### Go's random build directory

**Level:** Expert

Why can a temporary directory in Go's environment report cause extra misses, and what may Receipt safely normalize?

<details>
<summary>Answer</summary>

Go's environment report can include compiler flags containing a fresh scratch
directory. Receipt normalizes only the numeric suffix in recognized prefix-map
flags, retaining the rest of the settings. Unknown forms remain unchanged and
may cause a miss rather than silently hiding an input.

The implementation uses a scanner for ordinary bounded strings and a regular
expression fallback for other cases. Both must preserve the same normalization
rules, including adjacent flags. This does not cache the toolchain report; it is
read freshly on each scan.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`stableGoEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_environment.go#L10-L37)

</details>

### Two requests in production

**Level:** Expert

Production runs with `--max-inflight 2`. What can an unauthenticated internet client do with that, and what happens to legitimate CI when it does?

<details>
<summary>Answer</summary>

Authentication slots equal `--max-inflight`, so production has two. A header of the wrong length is refused before taking a slot, but a stream of well-formed fake tokens of the right length takes both authentication slots while their database lookups run, and real callers get 503. No per-IP or Caddy rate limit exists. The damage is to speed, not correctness: CI treats 503 like any server failure and runs its tests locally, and the independent uncached job runs regardless. The fix is a separate authentication slot count and a per-IP rate limit in Caddy.

**Learn more:** [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [`cacheAPI.authenticate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L43-L78)

</details>

### What would you change first?

**Level:** Expert

You may make three changes to Receipt before a second team starts using the shared cache. Which would you pick, and what does each cost?

<details>
<summary>Answer</summary>

Start with a problem observed by users. For a second team, useful operational
work might be scheduled restore-tested backups, monitoring and a protected second
copy of the recovery key. Those protect the ability to recover the service rather
than reducing a benchmark number.

Application candidates include a reviewed quarantine-recovery command and
credential-row cleanup. Both need explicit policies: removing a quarantine marker
can re-enable a known conflict, and deleting credentials changes the retained
administrative history. There is no required optimization sequence.

**Learn more:** [Limits that affect users](../DESIGN-DEFENSE.md#limits-that-affect-users) · [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [Backup and recovery](../DIGITALOCEAN-SETUP.md#backup-and-recovery)

</details>

### The same files in another directory
<!-- check: guide/01-remember-a-successful-run.md -->

**Level:** Expert

Alice copies unchanged source files to a different working directory and runs the same command. Must Receipt reuse the original result?

<details>
<summary>Answer</summary>

No. The manifest records the working directory as well as files and command
arguments. Changing that description changes the key, even when file contents
match. A test can print its directory or read a file from its parent. Omitting
paths would allow more sharing but could reuse output from a different context.

**Learn more:** [Chapter 1](../guide/01-remember-a-successful-run.md)

</details>

### A lock released before file work ends
<!-- check: guide/03-delete-old-data-without-breaking-a-reader.md -->

**Level:** Expert

An upload loses its database connection while still writing an output file. The database releases its lock. Why must cleanup still wait?

<details>
<summary>Answer</summary>

The upload still holds the filesystem lock. Cleanup needs that lock too, so obtaining the database lock alone does not let it delete files. Once the upload finishes its protected file work and releases the filesystem lock, cleanup can proceed. This protects cooperating operations, not programs that ignore the locks.

**Learn more:** [Chapter 3](../guide/03-delete-old-data-without-breaking-a-reader.md)

</details>

### A matching checksum on false data
<!-- check: guide/04-check-data-before-accepting-it.md -->

**Level:** Expert

Someone can replace both a stored result and its checksum. They change the result and calculate a new matching checksum. Does checking the checksum prove the original tests passed?

<details>
<summary>Answer</summary>

No. The checksum establishes agreement between the stored bytes and the supplied hash. Someone who controls both can create that agreement. Validation can reject inconsistent or malformed data, but it does not prove that tests ran.

**Learn more:** [Chapter 4](../guide/04-check-data-before-accepting-it.md)

</details>

### Revocation while an upload waits
<!-- check: guide/05-give-callers-different-permissions.md -->

**Level:** Expert

An upload passes its credential check, then waits for storage. The operator revokes its credential before it resumes. Must this admitted upload stop? What about the next request?

<details>
<summary>Answer</summary>

The admitted upload may finish. Revocation changes later checks; it does not cancel work already admitted. The next request checks the database again and is refused once it sees the committed revocation. Caching old permission decisions would risk extending access further.

**Learn more:** [Chapter 5](../guide/05-give-callers-different-permissions.md)

</details>

### A new store behind the same interface
<!-- check: guide/06-find-the-code-and-defend-a-decision.md -->

**Level:** Expert

A new storage implementation provides the same methods as the existing store. Does this alone establish that it handles interrupted uploads and cleanup correctly?

<details>
<summary>Answer</summary>

No. Matching method signatures lets the caller invoke the implementation. It does not establish what happens if saving fails halfway, or if cleanup overlaps a read. Trace a save, a failure and a retry through the new store before assuming it can replace the old one.

**Learn more:** [Chapter 6](../guide/06-find-the-code-and-defend-a-decision.md)

</details>

### A backup beside the live data
<!-- check: guide/09-follow-a-request-to-the-deployed-server.md -->

**Level:** Expert

The server stores a backup on its own disk, then the entire server is lost. What does an external encrypted copy change, and what must the operator still have?

<details>
<summary>Answer</summary>

The external copy can survive losing the server. The operator still needs the private recovery key to decrypt it, and must restore both database records and output files. Having the archive does not by itself establish that a fresh machine can run the restored service.

**Learn more:** [Chapter 9](../guide/09-follow-a-request-to-the-deployed-server.md)

</details>

### A conflict report never reaches the server
<!-- check: guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md -->

**Level:** Expert

Verification writes the local quarantine marker and removes the saved entry, but its network report fails. Predict what this client and another client can reuse.

<details>
<summary>Answer</summary>

This client keeps its quarantine marker and executes instead of replaying that key. The shared server may still hold the earlier result without knowing about the conflict, so another client can still reuse it. A successful local write does not establish agreement between machines, and an ordinary rerun does not promise to repair the failed report.

**Learn more:** [Chapter 10](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md)

</details>

### Two uploads claim the last free space
<!-- check: guide/11-defending-the-design.md -->

**Level:** Expert

Two uploads for different keys each need 8 MiB, while 10 MiB remains. Each holds a separate per-key lock and reads the total before either updates it. Can both pass their checks?

<details>
<summary>Answer</summary>

Yes. Each sees enough room for itself, but together they need 16 MiB. A lock for each key does not protect their shared budget. Receipt’s exclusive storage lock makes the second upload check after the first commits. Any design allowing overlap needs another way to reserve the shared space and protect files from cleanup.

**Learn more:** [Chapter 11](../guide/11-defending-the-design.md)

</details>

### A faster operation inside a noisy command
<!-- check: guide/12-profile-before-optimizing.md -->

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
