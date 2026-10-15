# Glossary

This page is a reminder, not a substitute for the chapters. Each entry says what the term means in Receipt in one or two sentences and links to the section that teaches it. Entries are in alphabetical order. Command-line flags appear under the name of the mode they select, such as "Verify mode" for `--verify`.

### Admission slot

An admission slot is one place in a fixed-size buffered channel that a server request must take before it does storage work. When every slot is taken, Receipt answers HTTP 503 with `Retry-After: 1` at once instead of queueing the request; the `--max-inflight` flag sets the number of slots (default 16, range 1 to 128).

Learn more: [D17. How does the server bound concurrent work?](DESIGN-DEFENSE.md#d17-how-does-the-server-bound-concurrent-work)

### Advisory lock

An advisory lock is a PostgreSQL lock on a number that the application chooses, not on a table row. Receipt takes one in shared or exclusive mode at the start of each shared-storage transaction, and PostgreSQL releases it when that transaction ends.

Learn more: [F6. PostgreSQL transactions, row locks and advisory locks](DESIGN-DEFENSE.md#f6-postgresql-transactions-row-locks-and-advisory-locks)

### Authentication

Authentication answers the question "is this credential valid?": it exists, has not expired and has not been revoked. A request that fails it receives HTTP 401.

Learn more: [Two questions before storage](guide/05-give-callers-different-permissions.md#two-questions-before-storage)

### Authorization

Authorization answers the question "may this valid credential perform this operation?". A valid credential without the needed scope receives HTTP 403, and the request never reaches storage.

Learn more: [Two questions before storage](guide/05-give-callers-different-permissions.md#two-questions-before-storage)

### Bearer token

A bearer token is a secret that grants access to whoever presents it, sent in the HTTP `Authorization` header. Receipt's credentials and its legacy shared token are both bearer tokens, so they need encrypted transport.

Learn more: [F9. Bearer tokens, TLS termination and reverse proxies](DESIGN-DEFENSE.md#f9-bearer-tokens-tls-termination-and-reverse-proxies)

### Blob

A blob is a stored sequence of bytes. On the shared server, each stdout or stderr capture is a blob file named by the SHA-256 of its content, so two results with identical output share one file.

Learn more: [Why keep descriptions and output separately?](guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately)

### Build script

A build script is a program Cargo compiles and runs while building a package. It can read files and settings Receipt cannot name, so the Cargo adapter refuses reuse when the selected dependency graph contains one.

Learn more: [8. Check what a Rust test depends on](guide/08-check-what-a-rust-test-depends-on.md)

### Bypass mode

`receipt run --bypass` runs the command directly without building a manifest, looking up a result or saving one. It cannot be combined with `--verify` or `--refresh`.

Learn more: [D6b. What do `--verify`, `--refresh` and `--bypass` do?](DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do)

### Cache

A cache is storage for work you hope to reuse. Receipt has a local cache directory, normally `.receipt` in the repository, and an optional shared cache on a server.

Learn more: [1. Remember a successful run](guide/01-remember-a-successful-run.md)

### Cache hit

A cache hit is finding a usable saved result for the current input key. Receipt then replays the saved output and does not start the test command.

Learn more: [A hit: replay without execution](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution)

### Cache miss

A cache miss means no usable saved result exists, so Receipt runs the command. A quarantined key, or a local hit that cannot pass the shared status check, also counts as a miss.

Learn more: [A miss: run the tests](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-miss-run-the-tests)

### Caddy

Caddy is the web server on the DigitalOcean droplet. It accepts HTTPS connections for `receipt.sidsharma.dev`, obtains and renews the certificate, and forwards each request to Receipt on `127.0.0.1:7777`.

Learn more: [9. Follow a request to the deployed server](guide/09-follow-a-request-to-the-deployed-server.md)

### Cargo

Cargo is Rust's build and package tool. Receipt supports a narrow form of `cargo test --frozen` and checks the tools, the dependency graph, the lockfile and registry source bytes before reuse.

Learn more: [8. Check what a Rust test depends on](guide/08-check-what-a-rust-test-depends-on.md)

### Checksum

A checksum is a hash stored beside data so a later reader can detect damage. Receipt's local entry envelope stores the SHA-256 of its payload; this exposes accidental changes but not a forger who replaces both.

Learn more: [What does a checksum tell us?](guide/04-check-data-before-accepting-it.md#what-does-a-checksum-tell-us)

### CI

CI, continuous integration, is a service that runs a project's tests automatically, here GitHub Actions. Receipt's own CI has a cached job that uses the shared cache and an independent uncached job.

Learn more: [Watch the test, not the old PASS message](guide/09-follow-a-request-to-the-deployed-server.md#watch-the-test-not-the-old-pass-message)

### CMS

CMS, Cryptographic Message Syntax, is the OpenSSL format used to encrypt the backup archive to a recovery certificate. Only the matching private key, kept on the Mac, can decrypt it.

Learn more: [D21. How is the server deployed, and how would we recover it?](DESIGN-DEFENSE.md#d21-how-is-the-server-deployed-and-how-would-we-recover-it)

### Cold path

The cold path is occasional work, such as issuing credentials or collecting unused files. Occasional does not mean cheap: collection can scan the whole store and make other operations wait.

Learn more: [Frequently performed work versus occasional work](guide/06-find-the-code-and-defend-a-decision.md#frequently-performed-work-versus-occasional-work)

### Conflict

A conflict is two observations that disagree for the same input key: different stdout, stderr or exit code. Receipt reacts by quarantining the key; it does not decide which observation was right.

Learn more: [A conflict: the same key, different observations](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-conflict-the-same-key-different-observations)

### Content addressing

Content addressing names stored data by a hash of its bytes. Equal content gets the same name, so Receipt stores identical output once and can check a blob by rehashing it on read.

Learn more: [F1. Cryptographic hashes and content addressing](DESIGN-DEFENSE.md#f1-cryptographic-hashes-and-content-addressing)

### Credential

A credential is the secret a client presents to the server. In credential mode each one is a database row with a subject label, scopes, an expiry time and an optional revocation time; the database keeps only a hash of the token.

Learn more: [5. Give callers different permissions](guide/05-give-callers-different-permissions.md)

### Dependency graph

A dependency graph records which packages need which others: an arrow from A to B means A needs B. The Cargo adapter walks this graph from the selected package before lookup.

Learn more: [8. Check what a Rust test depends on](guide/08-check-what-a-rust-test-depends-on.md)

### Digest

A digest is the hash value a hash function produces. On the shared server, a result's output digest is the SHA-256 of its stdout hash, stderr hash and exit code, and two uploads are "the same observation" when their digests match.

Learn more: [D11. What happens when two clients publish the same key?](DESIGN-DEFENSE.md#d11-what-happens-when-two-clients-publish-the-same-key)

### DNS

DNS, the Domain Name System, maps names such as `receipt.sidsharma.dev` to IP addresses. Cloudflare answers that lookup; it does not carry the cache request itself.

Learn more: [9. Follow a request to the deployed server](guide/09-follow-a-request-to-the-deployed-server.md)

### Dry run

A dry run reports what a maintenance command would change without changing anything. `receipt shared prune` and `receipt shared gc` are dry runs unless you add `--apply`.

Learn more: [The simple choice has a cost](guide/03-delete-old-data-without-breaking-a-reader.md#the-simple-choice-has-a-cost)

### Environment variable

An environment variable is a named text setting a process inherits, such as `PATH` or `LANG`. Receipt records every environment variable of its own process as a SHA-256 of the value, except its own `RECEIPT_TOKEN` and names listed in `ignore_environment`.

Learn more: [D4. Why is `RECEIPT_TOKEN` kept out of the key?](DESIGN-DEFENSE.md#d4-why-is-receipt_token-kept-out-of-the-key)

### Envelope

The envelope is the JSON wrapper of a local cache entry: a `payload` holding the result and a `digest` holding the SHA-256 of that payload. Reading an entry checks that the two still agree.

Learn more: [D7. How is a local result published so that a crash never leaves a partial entry?](DESIGN-DEFENSE.md#d7-how-is-a-local-result-published-so-that-a-crash-never-leaves-a-partial-entry)

### Exit code

An exit code is the number a process returns when it ends; zero means success. Receipt saves only results with exit code zero and replays that code on a hit.

Learn more: [A miss: run the tests](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-miss-run-the-tests)

### Extra inputs

`extra_inputs` in `.receipt.json` lists files or directories Receipt should record in addition to the files Git reports. Directories are walked recursively.

Learn more: [D1. What exactly goes into the cache key?](DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key)

### Flock

`flock` is a Unix call that places a shared or exclusive lock on an open file. Receipt holds one on the blob directory's `.lifecycle.lock` file for the length of each shared-storage operation, so file exclusion survives a lost database connection.

Learn more: [F5. File locks: flock versus fcntl](DESIGN-DEFENSE.md#f5-file-locks-flock-versus-fcntl)

### Fsync

`fsync` asks the operating system to write a file's data, or a directory's entries, from memory to the disk before returning. Receipt fsyncs temporary files and their directory when it publishes an entry or blob.

Learn more: [F4. Durability: the page cache and fsync](DESIGN-DEFENSE.md#f4-durability-the-page-cache-and-fsync)

### Garbage collection

Garbage collection, `receipt shared gc`, deletes blob files that no surviving database record refers to, including leftover temporary files. It runs under the exclusive lifecycle lock and refuses to continue if any record is corrupt.

Learn more: [D16. How does cleanup avoid deleting a file that is still needed?](DESIGN-DEFENSE.md#d16-how-does-cleanup-avoid-deleting-a-file-that-is-still-needed)

### Generic mode

`receipt run --allow-generic` lets Receipt cache a command outside its supported list. The key then covers only files, environment and the executable's bytes, so you accept responsibility for declaring every other input.

Learn more: [D1. What exactly goes into the cache key?](DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key)

### Goroutine

A goroutine is a function running concurrently inside a Go program, much cheaper than an operating-system thread. The server handles each request in its own goroutine.

Learn more: [7. Find where the time went](guide/07-find-where-the-time-went.md)

### Hard link

A hard link is a directory entry that names an existing file. Receipt publishes an entry by linking a finished temporary file to its final name; the link fails if the name already exists, so the first writer wins and nothing is overwritten.

Learn more: [F3. Files, inodes, directory entries and hard links](DESIGN-DEFENSE.md#f3-files-inodes-directory-entries-and-hard-links)

### Hash

A hash is a short, fixed-length value calculated from some bytes; changing the bytes changes the value. Hashing is not encryption: Receipt uses hashes to compare and name content, not to hide it.

Learn more: [1. Remember a successful run](guide/01-remember-a-successful-run.md)

### Hot path

The hot path is work that happens on normal commands: scanning inputs, calculating the key and looking up a result, plus a credential check per request in credential mode. A hit avoids running the tests, not this work.

Learn more: [Frequently performed work versus occasional work](guide/06-find-the-code-and-defend-a-decision.md#frequently-performed-work-versus-occasional-work)

### HTTP status code

An HTTP status code is the number at the start of a response that names its category. Receipt uses 204 for an allowed status check, 401 for a bad credential, 403 for a missing scope, 404 for a miss, 409 for a conflict, 503 for overload and 507 for a full store.

Learn more: [Two questions before storage](guide/05-give-callers-different-permissions.md#two-questions-before-storage)

### HTTPS

HTTPS is HTTP carried over TLS, which encrypts the connection and checks a certificate for the hostname. Receipt's client refuses plain HTTP to any host other than the local machine.

Learn more: [Encryption is not permission](guide/09-follow-a-request-to-the-deployed-server.md#encryption-is-not-permission)

### Input key

The input key is the SHA-256 of the manifest's JSON encoding. It is the name under which Receipt looks for a saved result; equal keys mean equal recorded inputs, not proven equal behaviour.

Learn more: [D2. How does the manifest become a key every time the same way?](DESIGN-DEFENSE.md#d2-how-does-the-manifest-become-a-key-every-time-the-same-way)

### JSON

JSON is a text format for objects, lists, strings and numbers. Receipt uses it for manifests, results, configuration and its HTTP API.

Learn more: [Why use a struct and slices?](guide/01-remember-a-successful-run.md#why-use-a-struct-and-slices)

### Lifecycle lock

The lifecycle lock is Receipt's pair of locks around every shared-storage operation: a PostgreSQL advisory lock plus a `flock` on the blob directory. Reads take it shared; uploads, reports, limit changes and cleanup take it exclusive.

Learn more: [D12. Why does one lock serialize every shared writer?](DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer)

### Lock

A lock coordinates operations that use the same resource: one operation waits until another releases it. Shared mode lets several readers hold it together; exclusive mode admits one holder.

Learn more: [Why does the filesystem need a lock?](guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock)

### Manifest

The manifest is Receipt's description of a run: format version, repository root, working directory, platform, command arguments and a sorted list of inputs. Its hash is the input key.

Learn more: [Why use a struct and slices?](guide/01-remember-a-successful-run.md#why-use-a-struct-and-slices)

### Map

A map is a Go lookup table from keys to values. Receipt uses maps to avoid recording a path twice, to remember visited Cargo packages and to hold the set of blob hashes that cleanup must keep.

Learn more: [3. Delete old data without breaking a reader](guide/03-delete-old-data-without-breaking-a-reader.md)

### Median

The median is the middle value after sorting a set of samples. The measurement chapters report medians of a few local runs, which are observations, not promises.

Learn more: [Does Receipt help the real test command?](guide/07-find-where-the-time-went.md#does-receipt-help-the-real-test-command)

### Metadata

Metadata is data about the stored results rather than their output bytes: records, quarantine flags, conflict evidence and storage limits. The shared server keeps metadata in PostgreSQL and output in blob files.

Learn more: [D10. Why PostgreSQL for metadata and files for output?](DESIGN-DEFENSE.md#d10-why-postgresql-for-metadata-and-files-for-output)

### Orphan blob

An orphan blob is a blob file that no database record refers to. A failed database save after a successful file write leaves one; garbage collection later removes it.

Learn more: [One server is simple, but can still fail halfway](guide/09-follow-a-request-to-the-deployed-server.md#one-server-is-simple-but-can-still-fail-halfway)

### PostgreSQL

PostgreSQL is the database the shared server uses for result records, quarantine state, limits and credentials. Credential mode requires it.

Learn more: [Why keep descriptions and output separately?](guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately)

### Principal

A principal is the checked identity behind a request: the credential's ID, its subject label and its scopes. The subject is an operator-chosen label, not a verified person.

Learn more: [5. Give callers different permissions](guide/05-give-callers-different-permissions.md)

### Probe

A probe is a small command Receipt runs to record a supported runner's environment. The Python probe records the interpreter and installed package versions; the Go probe records a normalised `go env -json`.

Learn more: [D1. What exactly goes into the cache key?](DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key)

### Process

A process is a running program with its own execution and memory. Receipt starts the test command as a child process and passes its arguments directly, without a shell.

Learn more: [What happens when pytest runs?](guide/01-remember-a-successful-run.md#what-happens-when-pytest-runs)

### Process group

A process group is a set of processes that can receive one signal together. Receipt starts the test command in its own group so that cancellation kills the command and every helper it started.

Learn more: [F8. Processes, process groups, signals and standard streams](DESIGN-DEFENSE.md#f8-processes-process-groups-signals-and-standard-streams)

### Pytest

Pytest is a Python test runner. The guide's running example is `uv run pytest`.

Learn more: [Follow one test run](WORKING-DOC.md)

### Quarantine

Quarantine marks an input key as untrustworthy so it is never replayed again. Locally it is a marker file; on the server it is a flag on the record. Nothing in Receipt clears it.

Learn more: [What if the clients disagree?](guide/02-let-another-client-reuse-the-result.md#what-if-the-clients-disagree)

### Quota

A quota is a storage limit the shared server enforces, such as the number of entries or the bytes of blobs and metadata. An upload that would exceed one is refused with HTTP 507.

Learn more: [The simple choice has a cost](guide/03-delete-old-data-without-breaking-a-reader.md#the-simple-choice-has-a-cost)

### R2

R2 is Cloudflare's object storage. It holds one encrypted backup archive of the droplet's database and files, not Receipt's live output.

Learn more: [One server is simple, but can still fail halfway](guide/09-follow-a-request-to-the-deployed-server.md#one-server-is-simple-but-can-still-fail-halfway)

### READ COMMITTED

READ COMMITTED is PostgreSQL's default isolation level: each statement sees data committed before it began. Receipt relies on its advisory lock, not a stricter isolation level, to serialise storage changes.

Learn more: [D14. Why is READ COMMITTED enough?](DESIGN-DEFENSE.md#d14-why-is-read-committed-enough)

### Refresh mode

`receipt run --refresh` skips reuse and runs the command. A success replaces the saved entry; a failure after a saved success with stable inputs quarantines the key.

Learn more: [D6b. What do `--verify`, `--refresh` and `--bypass` do?](DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do)

### Replay

Replay is what a hit does: write the saved stdout, then the saved stderr, and return the saved exit code. It reproduces output, not execution, so files the tests would write are not created.

Learn more: [A hit: replay without execution](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution)

### Retention

Retention decides how long shared records are kept. `receipt shared prune` removes non-quarantined records older than a duration (default 720 hours) by creation time, not by last use.

Learn more: [G14. Retention ignores use](DESIGN-DEFENSE.md#g14-retention-ignores-use)

### Reverse proxy

A reverse proxy accepts client connections and forwards each request to another server. Caddy is Receipt's reverse proxy: it ends TLS and forwards plain HTTP to Receipt on the same machine.

Learn more: [9. Follow a request to the deployed server](guide/09-follow-a-request-to-the-deployed-server.md)

### Revocation

Revocation marks a credential as no longer valid by setting its revocation time in the database. The next request is refused; a request that already passed its check may still finish.

Learn more: [Revocation does not rewind a request](guide/05-give-callers-different-permissions.md#revocation-does-not-rewind-a-request)

### Scope

A scope is one permission a credential holds: `read`, `publish`, `report` or `metrics`. No scope implies another, so a credential that may upload does not automatically read.

Learn more: [Two questions before storage](guide/05-give-callers-different-permissions.md#two-questions-before-storage)

### SHA-256

SHA-256 is the cryptographic hash function Receipt uses for keys, file contents, blob names, environment values and token hashes. Its output is 32 bytes, written as 64 hexadecimal characters.

Learn more: [D3. Why SHA-256?](DESIGN-DEFENSE.md#d3-why-sha-256)

### Slice

A slice is a Go sequence whose length can vary, written `[]T`. The manifest's command arguments and inputs are slices because their counts differ between runs.

Learn more: [Why use a struct and slices?](guide/01-remember-a-successful-run.md#why-use-a-struct-and-slices)

### Stability check

The stability check is the second input scan after the command finishes. If the key changed during the run, Receipt does not save the result under the old key.

Learn more: [D5. Why scan the inputs twice, and what can the two scans miss?](DESIGN-DEFENSE.md#d5-why-scan-the-inputs-twice-and-what-can-the-two-scans-miss)

### Stdout and stderr

Stdout is a command's normal output stream and stderr is its separate diagnostic stream. Receipt captures each, up to 8 MiB per stream, and replays stdout before stderr.

Learn more: [A miss: run the tests](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-miss-run-the-tests)

### Store-id binding

Store-id binding ties a blob directory to one database. The database records the directory's absolute path and a random ID, and the directory holds the same ID in a `.store-id` file; every lifecycle operation checks both, so one store's cleanup cannot delete another store's blobs.

Learn more: [Why does the filesystem need a lock?](guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock)

### Strict JSON

Strict JSON is Receipt's decoding rule set: reject unknown fields, repeated fields, missing required fields and oversized input before checking values.

Learn more: [4. Check data before accepting it](guide/04-check-data-before-accepting-it.md)

### Struct

A struct is a Go type that groups named fields of different types. `Manifest` and `Result` are structs.

Learn more: [Why use a struct and slices?](guide/01-remember-a-successful-run.md#why-use-a-struct-and-slices)

### TLS

TLS, Transport Layer Security, encrypts a network connection and lets the client verify the server's certificate. Receipt does not speak TLS itself; Caddy terminates it on the droplet.

Learn more: [F9. Bearer tokens, TLS termination and reverse proxies](DESIGN-DEFENSE.md#f9-bearer-tokens-tls-termination-and-reverse-proxies)

### Toolchain

A toolchain is the set of programs that build or run the tests, such as the Python interpreter, the Go tool or rustc. Receipt records supported toolchains through probes and hashes of the tool binaries.

Learn more: [D1. What exactly goes into the cache key?](DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key)

### Transaction

A transaction is a group of database changes that commit together or not at all. A transaction cannot undo a file write, which is why Receipt writes blobs before the record that refers to them.

Learn more: [F6. PostgreSQL transactions, row locks and advisory locks](DESIGN-DEFENSE.md#f6-postgresql-transactions-row-locks-and-advisory-locks)

### Uv

Uv is a Python package and project manager. `uv run pytest` starts pytest in the project's environment, and Receipt probes that environment with `uv run --no-sync` so the probe does not change it.

Learn more: [Follow one test run](WORKING-DOC.md)

### Verify mode

`receipt run --verify` runs the command again on a hit and compares exit code, stdout and stderr byte for byte. A match exits 0, changed inputs exit 1 as inconclusive, and a mismatch quarantines the key.

Learn more: [D6b. What do `--verify`, `--refresh` and `--bypass` do?](DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do)

### Workflow

A workflow is a GitHub Actions configuration file under `.github/workflows/` that defines CI jobs. Receipt's `receipt-ci.yml` runs the cached and uncached test jobs.

Learn more: [Watch the test, not the old PASS message](guide/09-follow-a-request-to-the-deployed-server.md#watch-the-test-not-the-old-pass-message)
