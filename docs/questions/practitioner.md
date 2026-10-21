# Practitioner questions

Choose a question, predict the result, then open the answer. Links lead to the
relevant explanation and implementation.

### Why sort the inputs before hashing
<!-- check: guide/01-remember-a-successful-run.md -->

**Level:** Practitioner

Receipt builds a manifest and hashes it into the input key. What does the manifest contain, and what does Receipt do so that the same inputs always produce the same key?

<details>
<summary>Answer</summary>

The manifest records a format version, the absolute Git root, the absolute working directory, the platform as `GOOS/GOARCH`, the command arguments in order, and a list of inputs. Each input has a kind, a name and a SHA-256 hash. There are four kinds: `file` for discovered repository files and declared extra inputs, `environment` for each environment variable except Receipt's own `RECEIPT_TOKEN`, `executable` for the bytes of the resolved command binary, and `toolchain` for a runner probe such as the Python package list or `go env -json`.

The key is the SHA-256 of the manifest encoded as JSON. Inputs are discovered by walking a Go map, whose iteration order is random, so `Manifest.Key` sorts a copy of the inputs by kind and then by name before encoding. Go's JSON encoder writes struct fields in declaration order, so the rest of the encoding is fixed. The command keeps its original order because reordering arguments can change what the command does. The copy matters: sorting the caller's slice in place would rearrange inputs the caller still holds.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L66-L77)

</details>

### How Receipt finds and hashes files

**Level:** Practitioner

Which files become inputs, what exactly is hashed for each one, and what does that cost on every run?

<details>
<summary>Answer</summary>

Receipt runs `git ls-files -z --cached --others --exclude-standard` at the repository root. That lists tracked files and untracked files that Git does not ignore. It always adds `.receipt.json` if it exists, skips everything under `.receipt/` (the default local cache, which would otherwise be its own input), and adds each `extra_inputs` entry, walking directories recursively. Ignored files are not inputs unless you declare them.

For each file, `fileInput` hashes the octal permission bits, a zero byte, and the file's contents, so making a script executable changes the key. A tracked file that has been deleted hashes the constant `deleted`. A directory (which is how a Git submodule appears), a symlink, or any other non-regular file is an error, and the run continues uncached.

There is no modification-time cache. Every run reads and hashes every byte of every discovered file and the command binary, so the work grows with total repository size. A miss does this twice: once before execution and once after, to check that the inputs did not change during the run.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`discoverFiles`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L156-L202) · [`fileInput`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L204-L226)

</details>

### Why only some commands are cached by default

**Level:** Practitioner

Receipt accepts `uv run pytest`, `python -m pytest`, `pytest`, `go test` and a bounded `cargo test --frozen`, and refuses other commands unless you pass `--allow-generic`. Why not cache any command, and what does each supported command add beyond the repository files?

<details>
<summary>Answer</summary>

A cached result is only safe to reuse when Receipt can name the inputs that decide it. For the supported runners Receipt knows where hidden inputs live and records them with a probe. `uv run pytest` runs `uv run --no-sync python -c <probe>`, and `python -m pytest` runs the same probe with that interpreter. A bare `pytest` must start with a `#!` line naming an interpreter with no spaces, which Receipt then probes. `go test` records `go env -json`. Cargo goes through its own adapter, which records tool binaries, the dependency graph and dependency source bytes.

Any other command makes `toolchainProbe` return an error. `run` reports "inputs unavailable; running uncached" and executes the command without lookup or storage, so an unsupported command is never silently cached. `--allow-generic` is an explicit opt-in: the key then covers only files, environment and the executable's bytes, and the user accepts responsibility for any input outside those.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`toolchainProbe`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L280-L304) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L289-L297)

</details>

### What the Python and Go probes record

**Level:** Practitioner

What does the Python probe record and miss? Why does Receipt rewrite part of `go env -json` before hashing it, and how narrow is that rewrite?

<details>
<summary>Answer</summary>

The Python probe prints JSON containing `sys.version`, `sys.executable` and a sorted list of installed distribution names and versions from `importlib.metadata`. It records names and versions only, not the installed files, so an editable install, a locally patched package or a rebuild with the same version number does not change the key. The `uv` probe passes `--no-sync` so that probing does not install anything.

`go env -json` includes `GOGCCFLAGS`, which contains `-ffile-prefix-map=<GOTMPDIR>/go-buildNNNN=/tmp/go-build`. Go chooses a new random `NNNN` scratch directory on every invocation, so without a rewrite every Go run would miss. `stableGoEnvironment` replaces only the digits after the exact `<GOTMPDIR>/go-build` prefix in `-ffile-prefix-map` and `-fdebug-prefix-map` flags with `<scratch>`. Everything else stays, and a format it does not recognise is left unchanged. That choice fails toward extra misses rather than toward hiding a real compiler setting. The input is named `resolved-go-environment-v2`. Go module dependencies are covered by `go.mod` and `go.sum` as repository files, not by the probe.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`stableGoEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_environment.go#L13-L36)

</details>

### What a hit gives back
<!-- check: guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md -->

**Level:** Practitioner

Which results can Receipt store, and on a hit, what exactly does the user get back?

<details>
<summary>Answer</summary>

Only successful runs are stored. `Validate` rejects any result whose exit code is not 0, so a failing run always executes again. A run is also not stored when its output exceeded the capture limit of 8 MiB per stream, when the inputs changed during execution, or when the key is quarantined.

A stored result holds the manifest, stdout bytes, stderr bytes, the exit code, the run's duration and its creation time. On a hit, `runSession.replay` prints a `receipt: hit` line with the saved duration to stderr, writes the saved stdout in full, then the saved stderr in full, touches the entry's modification time, and returns the saved exit code, which is 0. The tests do not run, so any files they would have written, network requests they would have made, and the original interleaving of stdout and stderr are not reproduced.

**Learn more:** [A hit: replay without execution](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution) · [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay) · [`runSession.replay`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L142-L162) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L74)

</details>

### The order of a run, from lookup to prune
<!-- check: guide/06-find-the-code-and-defend-a-decision.md -->

**Level:** Practitioner

Walk through `receipt run -- uv run pytest` on a miss that succeeds, with a remote cache configured. In what order do lookup, execution, local storage, upload and pruning happen?

<details>
<summary>Answer</summary>

`run` parses flags and refuses more than one of `--bypass`, `--refresh` and `--verify`. It loads `.receipt.json`, resolves the cache directory and builds the manifest; if that fails, it runs the command uncached. `runSession.lookup` treats a locally quarantined key as a miss, then reads the local entry. With no local entry it asks the server with a GET, and a remote hit is stored locally and replayed.

On a miss, Receipt runs the command, streaming output while capturing a bounded copy. It builds the manifest again and calls the run stable only if that succeeds with the same key. It records the history file used by `receipt explain`. A successful, stable run goes to `runSession.publish`, which stores the result locally, uploads it to the server, and only then prunes the local cache. A failed upload is reported on stderr but does not change the exit code.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L232-L335) · [`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L217-L246)

</details>

### How a local result is published

**Level:** Practitioner

Where does the local cache live, and how does `Disk.Put` publish an entry so that a crash never leaves a partial one? What happens if two processes publish the same key at once?

<details>
<summary>Answer</summary>

The cache defaults to `<git root>/.receipt`. A cache inside the repository must sit under `.receipt`, because file discovery skips that directory; anywhere else it would hash itself. Entries are `entries/<key>.json`, each a JSON envelope holding the payload and its SHA-256 digest. `history/` holds the last manifest per directory for `receipt explain`, and `quarantine/<key>` files mark keys that must not be reused.

`Disk.Put` writes the envelope to a `.pending-` temporary file in the same directory, calls `fsync` on it, then calls `os.Link` to create the final name, and finally `fsync`s the directory. A hard link either creates the name or fails because it already exists, and a reader can only ever see a complete file. If the link fails with "already exists", the first writer has won: `Put` reads the existing entry and succeeds only if it decodes and validates. It never overwrites, so a corrupt existing entry makes `Put` return an error rather than report success. A rename would have replaced the existing entry, which is the behaviour this design avoids.

**Learn more:** [Local storage](../DESIGN-DEFENSE.md#local-storage) · [`Disk.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L150-L194) · [`cacheDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L170-L188)

</details>

### How the local cache stays bounded

**Level:** Practitioner

How does local pruning choose which entries to delete, when does it run, and what does it not count?

<details>
<summary>Answer</summary>

`prune` lists `entries/*.json`, adds up their file sizes, sorts them by modification time with the oldest first, and removes entries until the total is at or below the limit. A replay updates the entry's modification time, so this approximates least-recently-used eviction. Pruning runs after every successful publish with `max_bytes` from `.receipt.json`, which defaults to 1 GiB, and on demand with `receipt cache prune --max-bytes`. Each run reads one directory and sorts it, so the cost grows with the number of entries.

Only `entries/` is counted. Files in `history/` and `quarantine/` are never counted or removed, so they grow without a bound. Keeping quarantine markers is deliberate: deleting one would let a key with contradictory evidence be replayed again.

**Learn more:** [Local storage](../DESIGN-DEFENSE.md#local-storage) · [`prune`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L369-L420)

</details>

### Why the shared server splits metadata and output

**Level:** Practitioner

The shared server keeps result records in PostgreSQL and output bytes in a blob directory on disk. Why not keep everything in PostgreSQL, or everything in files or object storage?

<details>
<summary>Answer</summary>

PostgreSQL holds what must change together under a transaction: which keys are visible, whether a key is quarantined, conflict evidence and the storage policy. The blob directory holds stdout and stderr as immutable files named by their SHA-256, stored at `blobs/<first two hex digits>/<hash>`.

Output can be up to 8 MiB per stream. Keeping it out of database rows keeps the metadata small and lets the metadata quota count only JSON records. Content addressing stores identical output once, and the quota check counts only bytes whose hash is not already present. Every read verifies a blob's length and hash. Files alone would lose the transactional decisions about conflicts and visibility. Object storage is not supported: maintenance needs a `ManagedBlobs` store that can be locked with `flock`, inventoried and safely deleted from, and the deployment is one small server.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`Postgres.checkBlobQuota`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L338-L381)

</details>

### Blobs before the commit
<!-- check: guide/02-let-another-client-reuse-the-result.md -->

**Level:** Practitioner

In what order does a shared upload write output files and the database record, and why that order? What does a shared read check before it returns a result?

<details>
<summary>Answer</summary>

`Postgres.Put` validates the result, takes the exclusive lifecycle lock, checks the blob quota and refuses a key that is already quarantined. `publishTx` then inserts the record that points at the blobs, inside the still-open transaction, and checks the metadata quotas. Next `Put` writes the stdout blob and the stderr blob, each with a temporary file, `fsync`, a hard link that refuses to overwrite and a directory `fsync`. Last, it commits.

The blobs are durable before the commit, and the record becomes visible only at the commit, so readers do not see the new record before its output is written. Later damage or file loss can still break a read. Committing the record before writing the blobs would let another client find a record whose output does not exist yet. Inserting the record before the blob writes, but committing after them, means a quota refusal writes no files. The cost is that a crash or a failed commit can still leave orphan blobs, which `shared gc` later removes. No single operation undoes both a file write and a database transaction.

`Postgres.Get` takes the shared lock, reads the record, digest and quarantine flag, and returns a conflict if the key is quarantined. It checks the record's version and key, fetches each blob while verifying its length and hash, and recomputes the output digest. A digest mismatch is reported as corruption; a missing blob is treated as a miss.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`Postgres.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L192-L243) · [`Postgres.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L147-L190)

</details>

### Two locks for one storage protocol

**Level:** Practitioner

Every shared storage operation enters through `Postgres.lifecycle`. Which locks does it take, which operations take them in shared or exclusive mode, and why use both a PostgreSQL lock and a file lock?

<details>
<summary>Answer</summary>

`lifecycle` takes a dedicated database connection and begins a READ COMMITTED transaction. In that transaction it calls `pg_advisory_xact_lock_shared` or `pg_advisory_xact_lock` on the key 72656365697075. It then takes `flock` with `LOCK_SH` or `LOCK_EX` on `.lifecycle.lock` in the blob directory. Reads (Get and Stats) take shared mode. Uploads, conflict reports, limit changes, `shared gc`, `shared prune` and policy initialisation take exclusive mode. Both locks are released when the transaction commits or rolls back.

The advisory lock coordinates every process that shares the database: server processes and maintenance commands. The file lock protects the files themselves. If the database connection dies, PostgreSQL releases the advisory lock, but the file lock stays held until the Go code closes it, so `shared gc` cannot delete files that an interrupted upload is still writing. The transaction also begins with `context.WithoutCancel`, so a cancelled request does not make `database/sql` roll back and release the lock while file work is still running.

**Learn more:** [Why does the filesystem need a lock?](../guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock) · [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L73-L120)

</details>

### What counts as the same observation

**Level:** Practitioner

When a second upload arrives for a key that already has a record, how does the server decide whether it agrees with the first, and what does `publishTx` do when it does not?

<details>
<summary>Answer</summary>

The server compares a digest: the SHA-256 of a JSON object holding the stdout hash, the stderr hash and the exit code. Duration and creation time are left out on purpose, because they differ on every run and do not describe the output.

`publishTx` checks the entry and metadata quotas only when the key is new, then runs `INSERT ... ON CONFLICT(key) DO NOTHING` and reads the stored digest and quarantine flag with `SELECT ... FOR UPDATE`. It records an evidence row for this (key, digest) pair unless the evidence quota is full. A conflict means the key was already quarantined, the call is a conflict report, or the stored digest differs from the new one. On a conflict it sets `quarantined = TRUE`, marks `evidence_capped` if evidence was dropped, commits, and returns a conflict, which HTTP turns into status 409. The first record stays in place but can no longer be read. A full evidence quota never prevents quarantine.

**Learn more:** [What if the clients disagree?](../guide/02-let-another-client-reuse-the-result.md#what-if-the-clients-disagree) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`ConflictReport.digest`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L52-L59) · [`Postgres.publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L268-L352)

</details>

### What puts a key in quarantine

**Level:** Practitioner

List every event that quarantines a key, locally and on the server. What does quarantine change for later runs?

<details>
<summary>Answer</summary>

Locally, Receipt writes the marker file `quarantine/<key>` in three cases. A `--verify` run finds a different exit code, stdout or stderr, or output over the capture limit. A `--refresh` run fails for a key that had a cached success, with stable inputs. Or the server answers 409 to a status check, a GET or an upload.

On the server, the `quarantined` column in `receipt_entries_v2` is set when an upload's digest differs from the stored one, or when a client sends a conflict report to `/v1/conflicts/<key>`.

A quarantined key is treated as a miss by the local lookup, is never stored again locally, and is refused by the server for reads and uploads. The reason is that two observations disagree for the same declared inputs, so either an input is undeclared or the test is nondeterministic. Nothing in Receipt removes the quarantine.

**Learn more:** [A conflict: the same key, different observations](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-conflict-the-same-key-different-observations) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L69-L103)

</details>

### Shape checks and value checks
<!-- check: guide/04-check-data-before-accepting-it.md -->

**Level:** Practitioner

A JSON result has every required field and no unknown ones. Is that enough for Receipt to accept it? Where are the two kinds of check made?

<details>
<summary>Answer</summary>

No. A well-formed result can still claim a nonzero exit code, carry an invalid hash, or name a key that does not match its manifest. Structure and meaning are separate checks.

`Result.UnmarshalJSON` first calls `strictjson.Object`, which rejects invalid UTF-8, duplicate fields, unknown fields, missing required fields, wrongly cased names, disallowed nulls and trailing data, decoding into a temporary value. It then calls `Validate`, which requires version 1, a valid manifest, a key equal to the manifest's hash, exit code 0, a nonnegative duration and streams within 8 MiB. Only after both pass does it assign the caller's value. Putting this in the decoding hook means every path that decodes a result, from HTTP or from a local file, gets the checks without each caller remembering them. Storage methods call `Validate` again for results built in code.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md#rules-shared-by-the-typed-boundaries) · [`Result.UnmarshalJSON`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/json.go#L9-L26) · [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61)

</details>

### Authentication versus permission
<!-- check: guide/05-give-callers-different-permissions.md -->

**Level:** Practitioner

Alice can download results but her upload is refused. Did authentication fail? How would the HTTP status tell you?

<details>
<summary>Answer</summary>

No. Her credential is valid; it does not include the `publish` scope that uploads need. `cacheAPI.serveScoped` first authenticates the credential. A missing, malformed, unknown, expired or revoked credential gets HTTP 401 with a `WWW-Authenticate: Bearer` header. Only then does it look up the scope the route requires and check it with `Principal.Allows`. A valid credential without that scope gets HTTP 403, and the request never reaches decoding or storage.

The scopes are `read`, `publish`, `report` and `metrics`, and none implies another. A credential that may publish but not read cannot download what it uploaded.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L25-L41)

</details>

### Two server authentication modes

**Level:** Practitioner

`receipt serve` can authenticate with one shared `RECEIPT_TOKEN` or with scoped credentials. Why do both exist, why does credential mode require `--postgres`, and can one server accept both?

<details>
<summary>Answer</summary>

The legacy mode compares the whole `Authorization` header in constant time against `Bearer` plus `RECEIPT_TOKEN`, which must be at least 24 characters. It needs no database, and every holder has every permission, including uploads and metrics. It suits one person or a quick local test.

Credential mode stores each credential as a row with its scopes, expiry and revocation time. Those rows live in PostgreSQL, so `serve` refuses `--credentials` without `--postgres`. The modes do not mix: `HandlerAuthorized` uses only the database authenticator and never falls back to the shared token, so a server in credential mode rejects the old token. A fallback would let anyone holding the old shared secret bypass scopes and revocation.

**Learn more:** [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [Start and administer locally](../SERVICE-CREDENTIALS.md#start-and-administer-locally) · [`serve`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L475-L499) · [`HandlerAuthorized`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L14-L23)

</details>

### How a credential is checked

**Level:** Practitioner

What does a Receipt credential look like, what does the database store, and what happens on each request? Why is plain SHA-256 acceptable here instead of bcrypt or argon2?

<details>
<summary>Answer</summary>

A token is `rcpt_<32 hex id>_<64 hex secret>`, 102 characters made from 48 random bytes. The database stores the id, the SHA-256 of the whole token, the subject label, the scopes, the expiry and the revocation time. The plaintext token is printed once, when it is issued.

On each request the server requires exactly one `Authorization` header of the exact length and parses the id with a regular expression. It then waits for an authentication slot and looks the row up by id with a 2-second timeout. The same query asks PostgreSQL whether the row is unrevoked and unexpired using `clock_timestamp()`, so the database clock decides expiry and a skewed server clock cannot extend a credential. Finally it compares hashes in constant time and revalidates the subject and scopes. There is no cache of earlier results, so a revocation applies to the next request.

Slow password hashes exist to make guessing low-entropy passwords expensive. The secret here is 32 random bytes, 256 bits, so guessing is already infeasible and one SHA-256 per request is enough.

**Learn more:** [Why does Receipt ask the database each time?](../guide/05-give-callers-different-permissions.md#why-does-receipt-ask-the-database-each-time) · [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [`generate`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L137-L144) · [`Postgres.Authenticate`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L89-L121)

</details>

### More writers behind one lock
<!-- check: guide/07-find-where-the-time-went.md -->

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

### A helper library with a build script
<!-- check: guide/08-check-what-a-rust-test-depends-on.md -->

**Level:** Practitioner

Alice's crate has no build script, but a helper library it depends on does. Should Receipt replay a previous success? Which dependencies does the Cargo adapter walk?

<details>
<summary>Answer</summary>

No. The helper's build script runs while this test compiles, and it could read files or environment that Receipt does not record. `cargoClosure` walks the resolved graph from `cargo metadata` starting at the selected package. `cargoPackagePolicy` refuses any package in that graph with a `custom-build` (build script) or `proc-macro` target, so Receipt runs Cargo uncached instead of looking up a result.

The walk follows normal and build dependencies of every package, but dev-dependencies only of the root package, because only the root's tests are compiled. A map of visited package ids makes each package checked once even when two arrows lead to it. The walk also refuses path dependencies outside the Git root, non-registry sources such as Git, and targets whose source lies outside their package. Passing these checks is not proof that every input is captured: a test can still read external files or services.

**Learn more:** [8. Check what a Rust test depends on](../guide/08-check-what-a-rust-test-depends-on.md#8-check-what-a-rust-test-depends-on) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoClosure`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L78-L126) · [`cargoPackagePolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L152-L179)

</details>

### What the Cargo adapter refuses

**Level:** Practitioner

Besides build scripts, what does the Cargo adapter refuse before it looks up a result, and why does each group of refusals exist?

<details>
<summary>Answer</summary>

The command must be `cargo test --frozen`, optionally with `--lib`, `-q` or `--quiet`, `--release`, `--all-features`, `--no-default-features`, `--manifest-path <path>` or `--features <list>`; any other or repeated option is refused. `--frozen` guarantees Cargo does not update `Cargo.lock` or fetch anything during the run.

A non-empty `RUSTC`, `RUSTDOC`, `RUSTFLAGS`, `RUSTDOCFLAGS`, `RUSTC_WRAPPER` or `RUSTC_WORKSPACE_WRAPPER` is refused, as is any other `CARGO_*` variable except `CARGO_HOME`, `CARGO_TARGET_DIR` and `CARGO_TERM_COLOR`. A `.cargo/config` or `.cargo/config.toml` in any ancestor directory or in `CARGO_HOME` is refused. These all change how code compiles without changing a recorded file. The tools must be rustup-managed, and Receipt hashes the resolved `cargo`, `rustc` and `rustdoc` binaries.

Dependencies must come from a registry. Receipt hashes `Cargo.lock` and every file of each selected registry crate, within a budget of 20,000 files and 256 MiB, because `Cargo.lock` does not change when someone edits a downloaded source file. Each refusal names an input Receipt cannot record, so it runs Cargo directly instead.

**Learn more:** [Accepted command grammar](../CARGO.md#accepted-command-grammar) · [Cargo](../CARGO.md) · [`parseCargo`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo.go#L23-L56) · [`cargoPolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_policy.go#L14-L53)

</details>

### What the client enforces about the connection
<!-- check: guide/09-follow-a-request-to-the-deployed-server.md -->

**Level:** Practitioner

Receipt itself speaks only plain HTTP. Where is TLS terminated in the DigitalOcean deployment, and what does the client refuse in order to protect its token?

<details>
<summary>Answer</summary>

On the droplet, Receipt listens on `127.0.0.1:7777`. Caddy terminates HTTPS for `receipt.sidsharma.dev`, obtains and renews its certificate, and forwards requests to that loopback port. Receipt's own port and PostgreSQL are not reachable from the internet.

`Remote.requestPath` refuses any remote URL that is not `https`, except `http` to `localhost`, `127.0.0.1` or `::1`. It also refuses URLs with a user name, a query string or a fragment. Its HTTP client never follows redirects, so the bearer token is not sent on to another host, and it has a 10-second timeout. An expired or revoked token is an application refusal (HTTP 401) on a working HTTPS connection, so the fix is a new credential, not a different certificate or an open database port.

**Learn more:** [Encryption is not permission](../guide/09-follow-a-request-to-the-deployed-server.md#encryption-is-not-permission) · [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [`Remote.requestPath`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L38-L61)

</details>

### Why two uploads for different keys wait for each other
<!-- check: guide/11-defending-the-design.md -->

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

### Two results share an output file
<!-- check: guide/03-delete-old-data-without-breaking-a-reader.md -->

**Level:** Practitioner

Results A and B both refer to file X. Cleanup removes A but keeps B. Should it remove X? Explain the lookup it needs.

<details>
<summary>Answer</summary>

No. Cleanup first records files needed by surviving results. B still needs X, so X’s hash remains in that lookup table and collection keeps the file. Deleting one result is not the same as deleting everything it referred to.

**Learn more:** [Chapter 3](../guide/03-delete-old-data-without-breaking-a-reader.md)

</details>
