# Expert questions

These questions probe what breaks Receipt, what an attacker or a crash can do, what the design costs as the store grows, and what you would change. Several are traps: the plausible answer from a general cache design is wrong for this code. Answer each one before opening it, then follow the links to the section that teaches it.

### The CI token in every key

**Level:** Expert

Receipt's own CI renews its `RECEIPT_TOKEN` before the old one expires. Nothing else changes. What happens to the CI cache hit rate, and why? What happened before 15 October 2026?

<details>
<summary>Answer</summary>

Today nothing happens to the hit rate. Receipt hashes every environment variable of the `receipt run` process into the manifest except `RECEIPT_TOKEN` and the names in `ignore_environment`. `environmentInputs` always skips the token, and `Run` removes it from the test's environment so that a test cannot depend on a value the key does not record. A new token therefore gives the same manifest and the same keys.

Before 15 October 2026 the token was hashed like any other variable. The manifest then contained SHA-256 of the token, which is exactly the `token_hash` the credential table stores, so each renewal changed every CI key and made every earlier result unreachable. Removing it changed, once, the key of every run made with the token set; runs without it kept their keys.

**Common wrong answer.** "Ignoring the token in the key is enough." Then a test that reads the token could behave differently under one key, so the child must lose the variable too.

**Learn more:** [D4. Why is `RECEIPT_TOKEN` kept out of the key?](../DESIGN-DEFENSE.md#d4-why-is-receipt_token-kept-out-of-the-key) · [`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L227-L240) · [`withoutToken`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L41-L47)

</details>

### Two machines, one commit

**Level:** Expert
<!-- check: guide/01-remember-a-successful-run.md -->

Alice and a CI runner check out the same commit and run `receipt run -- go test ./...`. Will they compute the same key? What decides it?

<details>
<summary>Answer</summary>

Usually not. The manifest records the absolute Git root and the absolute working directory, and each file input is named by its absolute path. The executable input records the absolute path of the resolved `go` binary and the SHA-256 of its bytes. Every environment variable is hashed by value, including `HOME`, `PATH`, `USER` and shell session variables such as `SHLVL`. The `go env -json` probe adds the Go version and settings. The keys match only if the checkout path, tool paths and bytes, environment values and platform are all identical.

Receipt's own CI gets cross-runner hits by building that sameness on purpose: a fixed checkout path, a fixed `HOME`, a small explicit environment that includes the runner image version, and declared extra inputs for its one Go module, `git`, `dash` and the client binary.

**Common wrong answer.** "Yes, because the key is derived from the repository's file contents." The file contents are only part of the manifest. Paths and the environment are in it too, so even two terminals on one laptop can disagree.

**Learn more:** [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [What is actually cached?](../RECEIPT-CI.md#what-is-actually-cached) · [`test_environment`](https://github.com/sidsharma96/receipt/blob/main/deploy/ci/self_test.py#L18-L28)

</details>

### The shared server is down

**Level:** Expert
<!-- check: guide/02-let-another-client-reuse-the-result.md -->

A developer has a valid local entry for a key and a `remote` configured. The shared server is unreachable. Does Receipt replay the local entry?

<details>
<summary>Answer</summary>

No. With a remote configured, a local hit is replayed only after `GET /v1/status/<key>` returns 204. Any other outcome, including a timeout, a connection error or a 5xx, makes `lookup` print "shared status unavailable or quarantined; executing instead of local replay" and run the tests. A 409 also writes a local quarantine marker. The rule is fail-closed: another client may have quarantined the key since this copy was saved, and the client cannot know that while the server is down. A successful run then tries to upload; that failure is reported and the local entry stays. To work offline with local hits, run without `--remote` and without a configured `remote`.

**Common wrong answer.** "Yes, the local cache is the fast path and the server is only a fallback." Local reuse is subordinate to the shared quarantine state whenever sharing is configured.

**Learn more:** [D9. Why does a local hit still ask the server?](../DESIGN-DEFENSE.md#d9-why-does-a-local-hit-still-ask-the-server) · [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L68-L102)

</details>

### A server without PostgreSQL

**Level:** Expert

Someone runs `receipt serve` without `--postgres`, using the disk store and the legacy shared token, and points clients at it. What changes for clients?

<details>
<summary>Answer</summary>

Three things. First, the disk store has no shared quarantine, so `GET /v1/status/<key>` and `POST /v1/conflicts/<key>` answer 501. `Remote.Check` treats 501 as an error, so every client with a remote configured stops replaying its local hits and runs the tests. A client with no local entry can still get a remote hit through `GET /v1/entries/<key>`. Second, there is no conflict detection. The server stores uploads with the same `Disk.Put` the client uses locally, which keeps an existing valid entry and returns success, so a differing upload gets 204 and the first writer wins silently. Third, there are no quotas, no retention and no `shared gc`.

**Common wrong answer.** "It behaves the same, only without scoped credentials." The legacy mode also loses the shared status check and conflict detection, which changes both hit behaviour and safety.

**Learn more:** [D9. Why does a local hit still ask the server?](../DESIGN-DEFENSE.md#d9-why-does-a-local-hit-still-ask-the-server) · [`cacheAPI.shared`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L263-L272) · [`putEntry`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L346-L369)

</details>

### Two CI runners race on one key

**Level:** Expert

Two CI runners miss on the same key at the same moment and both run `go test -count=1 ./...` successfully. What happens on the PostgreSQL server, and how does the key become usable again?

<details>
<summary>Answer</summary>

Both runners upload. The uploads take the exclusive lifecycle lock one after the other. The first inserts its record. The second computes the output digest, which is SHA-256 over the stdout hash, the stderr hash and the exit code. `go test` prints per-package timings such as `ok receipt/internal/app 1.234s`, so the stdout bytes almost always differ. `publishTx` sees a different digest, stores the new observation as evidence, sets `quarantined = TRUE`, commits and returns 409. The second client writes a local quarantine marker and removes its local entry. From then on every status check and fetch for that key returns 409, so nobody reuses it.

Nothing makes the key usable again. There is no command to clear quarantine, `shared prune` keeps quarantined keys and their evidence forever, and local markers are never pruned. The key stops mattering only when an input changes and produces a new key, or when an operator deletes the row and the marker by hand.

**Common wrong answer.** "The second upload overwrites the first, or is ignored because both passed." Receipt compares bytes, not test verdicts, and treats any disagreement as a broken input contract.

**Learn more:** [D11. What happens when two clients publish the same key?](../DESIGN-DEFENSE.md#d11-what-happens-when-two-clients-publish-the-same-key) · [G15. Quarantine cannot be undone](../DESIGN-DEFENSE.md#g15-quarantine-cannot-be-undone) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L267-L351)

</details>

### The cost of one upload

**Level:** Expert
<!-- check: guide/07-find-where-the-time-went.md -->

What does one shared upload cost as the store grows, what serialises, and what throughput ceiling follows? Was this deliberate?

<details>
<summary>Answer</summary>

Each `Postgres.Put` holds the global exclusive lifecycle lock for its whole duration. Under it, Receipt reads the policy row and the `.store-id` marker, reads one row of running totals, checks with at most two `lstat` calls whether the output files already exist, inserts the record and evidence and updates the totals in `publishTx`, writes and syncs the two output files, adds their bytes to the totals and commits. None of that depends on the store's size. Reads take the shared lock, so they wait behind any upload.

Before 15 October 2026 each upload instead walked every output file and ran `metadataUsage`, which scans every result and evidence row, three times: O(B + M + E) time. On one laptop the median upload grew from 33.4 ms on an empty store to 131.8 ms at about 4,045 results. With running totals, the same test gave 19 to 24 ms at every size. Because uploads run one at a time, throughput is still at most one divided by that latency, about 50 uploads per second on that laptop. The code says so directly: it deliberately serialises writers for a small deployment. The totals cost a recount at every server start and CLI command, and a crash after file writes can leave orphan files uncounted until that recount or the next `shared gc --apply`.

**Learn more:** [D15. What does one upload cost, and how does that grow?](../DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow) · [`readUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L208-L215) · [`reconcileUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L226-L237)

</details>

### Database connection lost mid-upload

**Level:** Expert
<!-- check: guide/03-delete-old-data-without-breaking-a-reader.md -->

An upload is writing output files when its request is cancelled, or its database connection drops. What stops `shared gc` in another process from deleting those files underneath it?

<details>
<summary>Answer</summary>

Two separate mechanisms. The transaction is begun with `context.WithoutCancel`, so a cancelled request context does not make `database/sql` roll back and release the advisory lock while file work is still running. The transaction ends only when the deferred `Rollback` or `Commit` runs, after the file work returns. Individual queries still use the request context.

If the connection itself dies, PostgreSQL releases the advisory lock at once, and a collector could acquire it. But every lifecycle operation also holds a `flock` on `blobs/.lifecycle.lock`, and that lock belongs to the open file, not the database session. The uploading process holds it until it closes the file, so the collector waits for the files. The tests `TestLifecycleCancellationKeepsFilesystemExclusion` and `TestDatabaseConnectionLossKeepsFilesystemExclusion` cover both cases. The limit is that `flock` coordinates one host only.

**Learn more:** [D12. Why does one lock serialize every shared writer?](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L72-L119) · [`lockBlobDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/lifecycle_unix.go#L15-L57)

</details>

### Is credential administration independent of storage?

**Level:** Expert
<!-- check: guide/05-give-callers-different-permissions.md -->

An operator issues a new credential while CI is uploading results. Does issuance interact with cache traffic at the lock level?

<details>
<summary>Answer</summary>

Issuance itself no longer does; one step of the command still does. Since 15 October 2026, `Postgres.Issue` and the credential-table migration take advisory lock `72656365697076`, the constant `credentialLock`, which differs from the storage `lifecycleLock` (`72656365697075`). Issuance therefore neither waits for in-flight reads and uploads nor makes new ones queue. The remaining interaction is earlier in the command: every `receipt auth` command calls `openShared`, which runs the storage migration and takes the exclusive lifecycle lock and its `flock` once. So `receipt auth issue` still waits once for the slowest in-flight storage operation, and a long `shared gc --apply` can still delay it. Authentication of existing tokens takes no lock and continues.

The credential lock exists so that two concurrent issuers cannot both count 1,023 rows and insert past the 1,024 cap. `TestCredentialIssuanceDoesNotWaitForStorageLock` holds a shared storage lock and checks that issuance still finishes.

**Common wrong answer.** "Issuance and storage share one lock, so renewal pauses all cache traffic." That was true until 15 October 2026. Only the storage initialisation in `openShared` still takes the storage lock.

**Learn more:** [D13. Which advisory lock keys exist, and what does each guard?](../DESIGN-DEFENSE.md#d13-which-advisory-lock-keys-exist-and-what-does-each-guard) · [G1](../DESIGN-DEFENSE.md#g1-receipt-auth-still-takes-the-storage-lock-once) · [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74)

</details>

### Credential lifecycle limits

**Level:** Expert

What limits apply to issued credentials, what happens after many renewals, and what happens to an upload in flight when its credential is revoked?

<details>
<summary>Answer</summary>

A credential lives between 1 second and 720 hours and carries one to four explicit scopes. The table may hold at most 1,024 rows, and that count includes expired and revoked rows. `Issue` counts every row and returns a capacity error at 1,024. The `auth` command offers issue, list and revoke, but no purge, so after roughly a thousand issuances, for example regular renewals plus short-lived smoke-test credentials, issuance fails until someone deletes rows in SQL.

Revocation sets `revoked_at` and is checked when each request authenticates. There is no permission cache, so the next request is refused with 401. An upload that already passed authentication is a snapshot of that moment and finishes. The test `TestRevocationDoesNotCancelAnAlreadyAuthenticatedUpload` records this.

**Common wrong answer.** "Expired and revoked credentials free their slots, and revocation cancels in-flight work." Neither is true here.

**Learn more:** [D18. How are credentials generated, stored and checked?](../DESIGN-DEFENSE.md#d18-how-are-credentials-generated-stored-and-checked) · [Bounds and operational costs](../SERVICE-CREDENTIALS.md#bounds-and-operational-costs) · [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74)

</details>

### Poisoning with a publish credential

**Level:** Expert
<!-- check: guide/04-check-data-before-accepting-it.md -->

An attacker holds a `publish`-scoped credential. What can they do to other users' test results, what limits the damage, and which CI rule exists because of this?

<details>
<summary>Answer</summary>

They can forge a passing result. The key is the hash of a manifest, so anyone who can reconstruct a victim's manifest can compute the key and upload a result with exit code 0 and any stdout they like. The server checks structure and consistency: strict JSON, size limits, the checksum, that the key matches the manifest and that the exit code is 0. It cannot tell whether tests ran. If nobody has stored that key yet, the forgery is accepted and every client that later misses locally fetches and replays a fake pass. The first writer wins, so a forgery placed early sticks.

Detection comes only from disagreement: an honest upload with different output for the same key, or a `--verify` run, quarantines it. Scopes let most callers hold `read` only. CI never gives private-cache credentials to pull-request code: the cached job runs only on pushes to trusted branches, and an independent uncached job always runs the full suite, so CI correctness never depends on the cache.

**Learn more:** [Part 3. Threat and failure model](../DESIGN-DEFENSE.md#part-3-threat-and-failure-model) · [Why keep the uncached job?](../RECEIPT-CI.md#why-keep-the-uncached-job) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L51-L73)

</details>

### What a read credential learns

**Level:** Expert

A caller holds only `read`. What can it learn from the manifests it fetches? Is "environment values are never stored in plaintext" a meaningful privacy guarantee?

<details>
<summary>Answer</summary>

Each fetched result carries the full manifest: the absolute repository and working-directory paths, which reveal user names and layout; the full command line; every input file path; the executable path; and the name of every environment variable with an unsalted SHA-256 of its value. An unsalted hash of a guessable value, such as `TZ=UTC`, a locale, an internal host name or a short password, can be confirmed offline by hashing candidates. Manifests made before 15 October 2026 also contain the SHA-256 of `RECEIPT_TOKEN`, which is exactly the `token_hash` stored in the credential table. The token cannot be recovered from it, because its secret part has 256 random bits, but it links those results to one credential. Newer manifests leave the token out.

So "never plaintext" means "not stored verbatim". It is not confidentiality for low-entropy values. The mitigations are `ignore_environment` and running tests in a small, explicit environment. There is no listing endpoint, so a reader must already know a key.

**Learn more:** [D4. Why is `RECEIPT_TOKEN` kept out of the key?](../DESIGN-DEFENSE.md#d4-why-is-receipt_token-kept-out-of-the-key) · [`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L227-L240) · [`digest`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L158-L161)

</details>

### Overload behaviour

**Level:** Expert

How does the server behave under overload? Name both admission layers, the status codes, the timeouts that apply, and the hidden limit that caps concurrency below `--max-inflight`.

<details>
<summary>Answer</summary>

Storage slots are a buffered channel sized by `--max-inflight`, default 16, range 1 to 128, and 2 in production. Acquisition does not block: a full channel answers 503 with `Retry-After: 1` and counts a rejection. In credential mode a second channel of the same size bounds authentication lookups; when it is full the answer is 503 "authentication busy", and a database error during authentication is also 503. A bad token gets 401 and a missing permission 403. Admitted storage work has a 25-second deadline, authentication has 2 seconds, the HTTP server has 5 s header, 30 s read, 30 s write and 60 s idle timeouts, and the client gives up after 10 seconds.

The hidden limit is the database pool: 16 open connections, shared by the server and every CLI command. Each storage operation pins one connection for its whole duration, so at most 16 run at once whatever `--max-inflight` says, and authentication can time out waiting for a connection. Receipt rejects rather than queues so clients fall back to running their tests within milliseconds; overload costs speed, not correctness.

**Learn more:** [D17. How does the server bound concurrent work?](../DESIGN-DEFENSE.md#d17-how-does-the-server-bound-concurrent-work) · [G4](../DESIGN-DEFENSE.md#g4-the-connection-pool-silently-caps-storage-concurrency-at-16) · [`SetMaxOpenConns`](https://github.com/sidsharma96/receipt/blob/main/internal/app/shared_command.go#L26-L33)

</details>

### Retention and evidence in maintenance

**Level:** Expert

What do `shared prune` and `shared gc` each delete? Is retention based on creation or last use, and why is it safe for gc to delete `.pending-` files?

<details>
<summary>Answer</summary>

Both commands are dry runs unless given `--apply`, and both take the exclusive lifecycle lock. `shared prune --older-than D`, 720 hours by default, deletes non-quarantined result rows whose `created_at` is older than the cutoff, together with their evidence. It never touches files, so their output files become orphans. Quarantined keys and their evidence are always kept. `created_at` is set by the first upload and never refreshed by a hit, so retention is first-in, first-out: a result used every day still expires 30 days after it was stored. The local cache, by contrast, evicts by file modification time, which a hit refreshes.

`shared gc` validates every record, building a map of referenced output hashes, and stops before deleting anything if one record is malformed. It then deletes every inventoried file not in the map, including `.pending-` temporary files. That is safe because the exclusive lock means no upload is in flight, so any pending file is a leftover from a crashed or failed write. The usual order is prune, then gc.

**Common wrong answer.** "Retention is least recently used." That is true of the local cache only.

**Learn more:** [D16. How does cleanup avoid deleting a file that is still needed?](../DESIGN-DEFENSE.md#d16-how-does-cleanup-avoid-deleting-a-file-that-is-still-needed) · [G14. Retention ignores use](../DESIGN-DEFENSE.md#g14-retention-ignores-use) · [`Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L470-L509)

</details>

### Why stop the service to back it up

**Level:** Expert

The backup script stops Receipt before running `pg_dump` and archiving the cache directory. `pg_dump` already gives a consistent snapshot. Why stop the service?

<details>
<summary>Answer</summary>

Because a result spans two stores. `pg_dump` is consistent only inside PostgreSQL. A `tar` of `/var/lib/receipt` taken while the server runs could miss an output file that a record committed during the copy refers to, or capture a half-written `.pending-` file. The backup does not take the lifecycle lock, so the only way to get one consistent cut across the database and the files is to stop every writer. The script also refuses to continue if any other `receipt` process is running, because a CLI maintenance command writes too. It then dumps the database and the roles, archives the state, binary and configuration, records checksums, and restarts the service in a `finally` block before encrypting and uploading. The measured stop-to-start interval in the R2 drill was 1.306 seconds.

**Common wrong answer.** "Stopping is unnecessary because `pg_dump` is transactionally consistent." The files are outside that transaction.

**Learn more:** [13 October: one-off R2 backup and restore](../DIGITALOCEAN-SETUP.md#13-october-one-off-r2-backup-and-restore) · [D21. How is the server deployed, and how would we recover it?](../DESIGN-DEFENSE.md#d21-how-is-the-server-deployed-and-how-would-we-recover-it) · [`pgrep`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/backup-r2-drill.py#L24-L28)

</details>

### Why the restore drill bind-mounts

**Level:** Expert
<!-- check: guide/09-follow-a-request-to-the-deployed-server.md -->

The restore drill bind-mounts the extracted backup over `/var/lib/receipt` inside a private mount namespace, instead of starting a test server against the extracted copy where it sits. What would fail otherwise, and why does that check exist?

<details>
<summary>Answer</summary>

The storage policy row in the restored database records the absolute blob directory, `/var/lib/receipt/cache/blobs`, and a random store identity. Every storage operation compares the server's blob directory with that path and the `.store-id` file with that identity. A server started against `/var/backups/.../extracted/var/lib/receipt/cache` would refuse every request with "blob directory does not match database storage policy". The binding exists so that one database's `shared gc` can never treat another store's files as unreferenced and delete them.

So the drill makes the restored files appear at the original path without changing what the live server sees. It runs under `unshare --mount --propagation private`, refuses to continue unless its mount namespace differs from PID 1's, and bind-mounts the restored directory there. It also uses a separate restored database and another loopback port, so production rows and the production port are never touched. The cost is that recovery onto a new machine must restore the files to the same absolute path, or the policy row must be edited by hand.

**Learn more:** [D21. How is the server deployed, and how would we recover it?](../DESIGN-DEFENSE.md#d21-how-is-the-server-deployed-and-how-would-we-recover-it) · [`Postgres.policy`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L171-L193) · [`mount --bind`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-check-20261012.sh#L7-L8)

</details>

### Scaling the Fly API to two machines

**Level:** Expert

Why does the Fly configuration have no `release_command`, and what happens if someone scales the Receipt API app to two machines on the same database?

<details>
<summary>Answer</summary>

Receipt's startup migration binds the blob directory: it records the path in PostgreSQL and writes a `.store-id` file onto the mounted volume. A Fly release machine has no volume, so migration must run in the serving machine at startup.

Each Fly machine gets its own volume. The second machine mounts an empty volume at the same path, `/data`, so the path check passes. Its volume has no `.store-id` file, so `Bind` creates one holding the same identity it reads from the database. The binding check passes on both machines. A result uploaded through machine A has its output file on A's volume. A fetch routed to machine B finds the record, fails to open the file, and returns a wrapped cache miss, which `getEntry` answers with 404. The client runs the tests and uploads again, which writes the file onto B's volume. If the digest matches, nothing else happens; if `go test` printed different timings, the key is quarantined for everyone. The quota check counts only local files, `shared gc` deletes only local unreferenced files, the advisory lock still serialises writers through the shared database, and `flock` protects nothing across machines. Data is not corrupted, but hits turn into re-runs and quarantines.

**Common wrong answer.** "The `.store-id` binding would detect the second volume and refuse to start." Both machines see the same path and get the same identity, so the binding passes.

**Learn more:** [What two Receipt Machines would actually do](../FLY-DEPLOYMENT.md#what-two-receipt-machines-would-actually-do) · [`getEntry`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L319-L325) · [`release_command`](https://github.com/sidsharma96/receipt/blob/main/deploy/fly/api.toml#L12)

</details>

### Exact semantics of verify and refresh

**Level:** Expert
<!-- check: guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md -->

What are the exit codes of `--verify` on a hit for a match, a mismatch and changed inputs? When, exactly, does `--refresh` quarantine a key?

<details>
<summary>Answer</summary>

On a hit, `--verify` runs the tests again and scans the inputs again. If the key changed between the two scans, it prints "verification inconclusive", quarantines nothing and exits 1. If the exit code, stdout or stderr differ byte for byte, or the output exceeded the capture limit, it writes a local quarantine marker, removes the local entry, reports the conflict's hashes to the server and exits with the new run's exit code, or 1 if the new run exited 0. If everything matches it exits 0. On a miss, `--verify` behaves like a normal run.

`--refresh` skips reuse but reads the local entry for the key. It quarantines in one case only: the fresh run fails, a cached success for the same key existed locally, and the inputs did not change during the run. A successful refresh removes the old local entry and stores the new one, even if the output differs. The upload that follows can still meet a different stored result on a PostgreSQL server, which answers 409 and makes the client quarantine locally. At most one of `--bypass`, `--refresh` and `--verify` may be given.

**Common wrong answer.** "A successful refresh whose output differs from the cached result is a conflict and quarantines locally." Locally it simply replaces the entry; only a failure after a recorded success quarantines.

**Learn more:** [D6b. What do `--verify`, `--refresh` and `--bypass` do?](../DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do) · [`verify`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L163-L191) · [`failed`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L193-L214)

</details>

### Replay is not a live run

**Level:** Expert

From the user's point of view, in what ways is a replayed result different from a live run?

<details>
<summary>Answer</summary>

The replay writes all of the saved stdout and then all of the saved stderr, so the original interleaving of the two streams is lost. The child ran with pipes, not a terminal, so there were no colours or terminal-dependent formatting to save. Its standard input was `/dev/null`. Durations and timestamps in the output are the original run's. Side effects do not happen: files the tests wrote, coverage reports, JUnit XML and network calls are absent. Receipt adds a "hit" line on stderr. Output over 8 MiB per stream was never stored, so such a run never replays. The exit code is always 0, because only successes are stored.

**Learn more:** [D6. Why cache only successes, replay exact bytes and never normalise output?](../DESIGN-DEFENSE.md#d6-why-cache-only-successes-replay-exact-bytes-and-never-normalise-output) · [`runSession.replay`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L141-L161)

</details>

### Ctrl-C during a run

**Level:** Expert

The user presses Ctrl-C while `receipt run` is executing tests. How are child processes cleaned up, what exit code is returned, and is anything stored? What would you change?

<details>
<summary>Answer</summary>

`Main` cancels its context on SIGINT or SIGTERM. The child was started in its own process group, so Ctrl-C from the terminal reaches only Receipt. On cancellation Receipt sends SIGTERM to the whole group, waits up to 5 seconds for the group to empty, and then sends SIGKILL to anything left. Pytest workers and compiled Go test binaries in that group get both signals; a grandchild that started its own group escapes. `WaitDelay` then gives the output pipes 2 seconds to close. `Run` reports 130 whenever Receipt cancelled the command, however it ended, and `run` checks the context and returns 130 before scanning the inputs again, recording history or publishing, so nothing is stored. On platforms other than Unix the process-group setup is a no-op and only the direct child is killed.

Before 15 October 2026 the group got SIGKILL at once, so tests had no chance to clean up, and every signal death, including a `SIGSEGV` crash, was reported as 130. Now a crash that Receipt did not cause reports 128 plus the signal number, such as 139. The cost is that a test ignoring SIGTERM delays Ctrl-C by up to 5 seconds.

**Learn more:** [D19. How is a running test stopped?](../DESIGN-DEFENSE.md#d19-how-is-a-running-test-stopped) · [`configureProcess`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process_unix.go#L16-L46)

</details>

### A build script one dependency away

**Level:** Expert
<!-- check: guide/08-check-what-a-rust-test-depends-on.md -->

A Rust package's own dependencies are clean, but one of its dependencies has a dev-dependency that uses a build script. Does Receipt refuse to cache `cargo test --frozen` for the root package?

<details>
<summary>Answer</summary>

No. Receipt walks the resolved graph from the root with a depth-first search. It follows normal and build dependency edges everywhere, but dev-dependency edges only from the root package, because Cargo builds a dependency's dev-dependencies only when testing that dependency itself. The package with the build script is never visited, so `cargoPackagePolicy` never sees it. If the build script were on a normal or build edge anywhere in the walked graph, Receipt would refuse reuse before looking up the cache and run Cargo uncached. Passing the check still does not prove the tests read nothing else: ordinary tests can open files or call services Receipt does not record.

**Common wrong answer.** "Any build script anywhere in `cargo metadata` output forces uncached execution." Only the packages reachable through the edges Cargo would compile for this test are checked.

**Learn more:** [D20. How does the Cargo adapter decide what it can trust?](../DESIGN-DEFENSE.md#d20-how-does-the-cargo-adapter-decide-what-it-can-trust) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoDependencies`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L128-L150)

</details>

### Why READ COMMITTED is enough

**Level:** Expert
<!-- check: guide/11-defending-the-design.md -->

`publishTx` counts usage, checks quotas and then inserts, all under READ COMMITTED. Why is that not a write-skew bug, and what would break if someone narrowed the global lock to per-key locks?

<details>
<summary>Answer</summary>

Every writer and every maintenance operation holds the exclusive lifecycle lock for its whole transaction. While one holds it, no other transaction changes the result, evidence or policy tables, so each READ COMMITTED statement sees the same data a snapshot would. Two uploads cannot both read 10 MiB of free budget and both add 8 MiB, because the second waits for the first to commit. Correctness comes from the lock, not from the isolation level. The `SELECT ... FOR UPDATE` in `publishTx` never waits today; it is kept so the row logic stays correct if the lock is narrowed.

With per-key locks, two uploads for different keys would run together. Both could pass the budget check and overshoot it, and a collector could decide output file X was unreferenced while an upload for another key was about to reference it. SERIALIZABLE would catch the database half of that, but it cannot see the filesystem, so it does not protect output files from the collector.

**Learn more:** [D14. Why is READ COMMITTED enough?](../DESIGN-DEFENSE.md#d14-why-is-read-committed-enough) · [D12](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L267-L351)

</details>

### Durability after a power loss

**Level:** Expert

The server loses power just after an upload commits, and later a client's laptop loses power just after `--verify` quarantines a key. What can be lost in each case?

<details>
<summary>Answer</summary>

On the server, output files are synced before they are linked into place and before their record commits, so a committed record never names bytes that were never written. `FileBlobs.Put` creates a new two-character directory through `MakeDirs`, which syncs the parent of every directory it creates, so the directory survives together with the file. Orphan files from uploads whose commit failed stay until `shared gc`.

On the laptop, `quarantine` writes the marker with a rename and then syncs `quarantine/`, and `Disk.Remove` syncs `entries/` after removing the entry, so neither change can be undone by a power loss after it returns. Before 15 October 2026 none of these directories were synced: a new shard directory could vanish while its record survived (a 404 and a re-upload), and the marker and the removal could both be undone, letting the conflicting result be replayed locally unless a remote's status check blocked it.

**Learn more:** [D7. How is a local result published so that a crash never leaves a partial entry?](../DESIGN-DEFENSE.md#d7-how-is-a-local-result-published-so-that-a-crash-never-leaves-a-partial-entry) · [`MakeDirs`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/dirsync.go#L24-L50) · [`quarantine`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L221-L229)

</details>

### Go's random build directory

**Level:** Expert

Without special handling, every `go test` run would miss. Why, what exactly does Receipt normalise, and what is the known edge case?

<details>
<summary>Answer</summary>

The `go env -json` probe output includes `GOGCCFLAGS`, which contains `-ffile-prefix-map=<tmp>/go-buildNNNN=/tmp/go-build`. The NNNN suffix names a scratch directory that Go creates afresh for each invocation, so the probe, and with it the key, would change every run. `stableGoEnvironment` replaces only the digits after the exact `<GOTMPDIR or temp dir>/go-build` prefix in `-ffile-prefix-map` and `-fdebug-prefix-map` with a fixed placeholder, and stores the result as the `resolved-go-environment-v2` input. Everything else is kept, and an unknown format is left unchanged, which fails towards misses rather than hiding a real input.

The edge case is that the pattern consumes the delimiter after a match, so in one pass a second prefix-map flag separated by a single space is not normalised. Until 15 October 2026 that caused extra misses, never wrong hits. `stableGoEnvironment` now repeats the replacement until nothing changes, so each flag is normalised. Current Go versions print a single such flag, so their keys did not change.

**Learn more:** [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [`stableGoEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_environment.go#L10-L37)

</details>

### Two requests in production

**Level:** Expert

Production runs with `--max-inflight 2`. What can an unauthenticated internet client do with that, and what happens to legitimate CI when it does?

<details>
<summary>Answer</summary>

Authentication slots equal `--max-inflight`, so production has two. A header of the wrong length is refused before taking a slot, but a stream of well-formed fake tokens of the right length takes both authentication slots while their database lookups run, and real callers get 503. No per-IP or Caddy rate limit exists. The damage is to speed, not correctness: CI treats 503 like any server failure and runs its tests locally, and the independent uncached job runs regardless. The fix is a separate authentication slot count and a per-IP rate limit in Caddy.

**Learn more:** [G5. Production admission is two requests](../DESIGN-DEFENSE.md#g5-production-admission-is-two-requests) · [Part 3. Threat and failure model](../DESIGN-DEFENSE.md#part-3-threat-and-failure-model) · [`cacheAPI.authenticate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L42-L77)

</details>

### What would you change first?

**Level:** Expert
<!-- check: guide/06-find-the-code-and-defend-a-decision.md -->

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

### Does a faster mean prove startup outliers are fixed?
<!-- check: guide/16-resolve-selected-git.md -->

**Level:** Expert

The new binary has a lower mean latency with a positive savings interval. Does
that establish that the rare 300 ms delay cannot happen again?

<details>
<summary>Answer</summary>

No. The controlled run estimates routine warm-hit cost on one host. A confidence
interval from those observations does not bound every scheduler, filesystem or
probe delay. The new binary still had slow hits. Diagnosing a particular rare
wait requires evidence covering that call's stage and time interval; mean savings
do not identify its kernel cause.

**Learn more:** [16. Resolve the selected Git once](../guide/16-resolve-selected-git.md)

</details>

### Do overlapping delays prove the same cause?
<!-- check: guide/16-resolve-selected-git.md -->

**Level:** Expert

A Go probe takes 1.675 seconds and a separate system sample takes 1.678 seconds
over nearly the same interval. Memory-compression counters also increase.
Have we proved that compression caused Go to wait?

<details>
<summary>Answer</summary>

No. The overlap suggests shared machine contention, but neither clock records
the native thread's waiting reason. The counters are machine-wide and span a
wider snapshot interval; the sampler itself adds work. A quiet-period comparison
with interleaved sampler-on/off blocks can test the association and observer
cost. A matching native thread-state trace is needed to identify the wait.

**Learn more:** [A longer local run reproduced the stalls](../guide/16-resolve-selected-git.md#a-longer-local-run-reproduced-the-stalls)

</details>

### What does a fast indexed plan fail to measure?
<!-- check: guide/17-measure-linux-client-and-server.md -->

**Level:** Expert

The droplet shows an indexed result lookup around 0.06 ms, but the remote-fetch
client takes hundreds of milliseconds. Does another database index solve that gap?

<details>
<summary>Answer</summary>

The plan covers PostgreSQL execution for that query and selected row; it does
not cover the full HTTP connection, metadata processing or result transfer.
The current point lookups already use primary-key indexes. Measure the other
boundaries before changing indexes; the tiny warm dataset does not establish
large-table or high-concurrency performance.

**Learn more:** [17. Measure the Linux client and cache server](../guide/17-measure-linux-client-and-server.md)

</details>

### Can we add the server handler time to client HTTP phases?
<!-- check: guide/18-trace-go-http.md -->

**Level:** Expert

An exact request ID matches a server handler duration to a traced client request. Is the server duration an extra piece to add to DNS, TCP, TLS, first-byte waiting and body reading?

<details>
<summary>Answer</summary>

No. The client waits while the server works, and server writes can overlap client response transfer. Adding both would count overlapping elapsed time twice. Matching IDs identifies the same request, but does not turn two clocks into an additive timeline.

**Learn more:** [18. Trace the actual Go HTTP client](../guide/18-trace-go-http.md)

</details>
