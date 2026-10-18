# Newcomer questions

These questions are for readers meeting Receipt, test caching or shared storage for the first time. They check the basic ideas: what Receipt records, what a saved result can and cannot do, and which part of the system does which job. Each answer stays hidden until you open it, so try answering in your own words first.

### What problem does Receipt solve?
<!-- check: guide/01-remember-a-successful-run.md -->

**Level:** Newcomer

Alice runs `uv run pytest`, the tests pass, and ten minutes later she runs the same command without changing anything. What does Receipt do the second time, and what does it need to know to do it safely?

<details>
<summary>Answer</summary>

Receipt wraps the test command. Before running it, Receipt describes the run: the command arguments, the repository files, hashes of the environment values, the working directory, the platform and runtime information such as the Python packages. If a successful run with exactly the same description was saved earlier, Receipt prints that run's saved output and exit code instead of starting pytest. Finding such a result is a cache hit; having to run the command is a cache miss. To do this safely Receipt must name every input that could change the result, which is why it records so much more than the command line.

**Learn more:** [1. Remember a successful run](../guide/01-remember-a-successful-run.md) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L333)

</details>

### What is a manifest?

**Level:** Newcomer

Receipt calls its description of a run a manifest. Which kinds of information does a manifest hold?

<details>
<summary>Answer</summary>

A manifest is a Go struct with six fields: a format version, the Git repository root, the working directory, the platform (operating system and CPU architecture, such as `darwin/arm64`), the command arguments in order, and a list of inputs. Each input has a kind, a name and a content hash. There are four kinds of input: files from the repository, environment variables, the executable that runs the command, and toolchain information such as the installed Python packages or Go's settings. Environment values are stored only as hashes, never as plain text.

**Learn more:** [Why use a struct and slices?](../guide/01-remember-a-successful-run.md#why-use-a-struct-and-slices) · [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [`Manifest`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L53-L60)

</details>

### What is a hash, and what is the input key?

**Level:** Newcomer

The guide says Receipt hashes the manifest to get an input key. What is a hash, and why use one as the key?

<details>
<summary>Answer</summary>

A hash is a short, fixed-length value calculated from some bytes. Receipt uses SHA-256, which always produces 32 bytes, written as 64 hexadecimal characters. The same bytes always give the same hash, and in practice different bytes give different hashes. Receipt encodes the manifest as JSON, with the inputs sorted by kind and then name, and hashes that JSON. The result is the input key: the name under which Receipt looks for a saved result. Any change to a recorded input changes the key, so an edited file leads to a lookup under a different name. A hash is not encryption: it hides nothing and cannot be reversed into a key you could decrypt.

**Learn more:** [F1. Cryptographic hashes and content addressing](../DESIGN-DEFENSE.md#f1-cryptographic-hashes-and-content-addressing) · [D2. How does the manifest become a key every time the same way?](../DESIGN-DEFENSE.md#d2-how-does-the-manifest-become-a-key-every-time-the-same-way) · [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L65-L76)

</details>

### Why can an unrelated edit cause a miss?

**Level:** Newcomer

Alice runs only one test file. She edits a README in the same repository. Why does her next run miss?

<details>
<summary>Answer</summary>

Receipt records every file Git knows about, plus untracked files that are not ignored, not only the files one test actually reads. The edited README changes its file hash, which changes the manifest and therefore the key. The broader description causes extra misses, but it avoids replaying a result when a file the test does read has changed. Narrowing the description would need a reliable way to discover which files a test reads, which Receipt does not have.

**Learn more:** [1. Remember a successful run](../guide/01-remember-a-successful-run.md#matching-inputs-do-not-prove-matching-behavior) · [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key)

</details>

### Why are failed runs never saved?

**Level:** Newcomer

A test run fails with exit code 1. Will Receipt replay that failure next time?

<details>
<summary>Answer</summary>

No. Receipt saves only runs that exit with code 0, which means the command reported success. A failed run prints "failed results are not cached", and the next run executes the tests again. Storage refuses a nonzero exit code as well: the result check rejects it with "reusable results require success". Replaying a failure would hide whether a fix worked, and a failure is the case where you most want the tests to run again.

**Learn more:** [D6. Why cache only successes, replay exact bytes and never normalise output?](../DESIGN-DEFENSE.md#d6-why-cache-only-successes-replay-exact-bytes-and-never-normalise-output) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L72)

</details>

### What does a replay not reproduce?
<!-- check: guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md -->

**Level:** Newcomer

The original test run wrote a coverage report and printed warnings mixed in with normal output. What will a replay give you?

<details>
<summary>Answer</summary>

A replay gives back only the captured output and the exit code. It does not recreate files the tests wrote, such as a coverage report or a JUnit XML file, and it does not repeat network requests. The mixing of standard output and standard error is lost too: Receipt writes all of the saved standard output first, then all of the saved standard error. Timings printed in the output are the original run's timings. If you need a test's side effects, run it with `--bypass`.

**Learn more:** [A hit: replay without execution](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution) · [D6. Why cache only successes, replay exact bytes and never normalise output?](../DESIGN-DEFENSE.md#d6-why-cache-only-successes-replay-exact-bytes-and-never-normalise-output)

</details>

### Where does Receipt keep its local cache?

**Level:** Newcomer

Alice has not configured anything. Where on her disk are saved results kept, and why must that place be inside a particular folder?

<details>
<summary>Answer</summary>

By default the cache is the `.receipt` directory at the root of the Git repository. Saved results live in `.receipt/entries/<key>.json`, quarantine markers in `.receipt/quarantine/`, and the last manifest for each working directory in `.receipt/history/`. A cache inside the repository must sit under `.receipt` because file discovery skips that directory. A cache stored anywhere else in the repository would be one of its own inputs, so saving a result would change the key of the next run.

**Learn more:** [D7. How is a local result published so that a crash never leaves a partial entry?](../DESIGN-DEFENSE.md#d7-how-is-a-local-result-published-so-that-a-crash-never-leaves-a-partial-entry) · [`cacheDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L169-L187)

</details>

### What does quarantine mean?

**Level:** Newcomer

Receipt sometimes says a key is quarantined. What does that mean for the next run?

<details>
<summary>Answer</summary>

A quarantined key is one Receipt has seen produce two different observations: for example, a rerun printed different output from the saved run. Receipt then refuses to reuse that key. Locally it writes a marker file under `.receipt/quarantine/` and removes the saved entry; on the shared server it sets a flag on the key's record. Every later run with that key executes the tests. Quarantine does not decide which observation was right, and no command lifts it; a new key, produced by changing an input, is the normal way out.

**Learn more:** [What if the clients disagree?](../guide/02-let-another-client-reuse-the-result.md#what-if-the-clients-disagree) · [A conflict: the same key, different observations](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-conflict-the-same-key-different-observations) · [`quarantine`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L223-L229)

</details>

### Why does --verify exist?

**Level:** Newcomer

An ordinary hit never runs the tests. How would you find out that a saved result no longer matches what the tests print today?

<details>
<summary>Answer</summary>

Run `receipt run --verify -- <command>`. On a hit, Receipt runs the command again and compares the exit code, standard output and standard error byte for byte with the saved result. If they match, it prints "verification matched byte-for-byte" and exits 0. If they differ, it quarantines the key and exits with a nonzero code, even when the fresh tests passed, because the disagreement itself is the finding. A difference can be harmless timing text such as "0.42s" versus "0.51s", so a mismatch is not automatically a failed test.

**Learn more:** [Matching inputs do not prove matching behavior](../guide/01-remember-a-successful-run.md#matching-inputs-do-not-prove-matching-behavior) · [D6b. What do `--verify`, `--refresh` and `--bypass` do?](../DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do) · [`runSession.verify`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L163-L191)

</details>

### What do the client and the server each do?

**Level:** Newcomer

Alice and CI share results through a Receipt server. Which machine runs pytest?

<details>
<summary>Answer</summary>

The client runs pytest, always on the machine where `receipt run` was typed. The server never runs tests. It stores results that clients upload over HTTP and returns them to clients that ask for a key. On a miss, the client executes the tests and, if the run is eligible, saves the result locally and uploads it. Another client with the same key can then fetch that result and replay it without executing anything.

**Learn more:** [2. Let another client reuse the result](../guide/02-let-another-client-reuse-the-result.md) · [A miss: run the tests](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-miss-run-the-tests)

</details>

### What are PostgreSQL and blobs for?
<!-- check: guide/02-let-another-client-reuse-the-result.md -->

**Level:** Newcomer

The shared server uses PostgreSQL and a directory of blob files. What goes where?

<details>
<summary>Answer</summary>

PostgreSQL, a database, holds the small records: each key's result description, references to its output, whether the key is quarantined, conflict evidence and storage limits. The output bytes themselves go into files called blobs, named by the SHA-256 hash of their content and stored as `blobs/<first two hex characters>/<hash>`. Because the name comes from the content, two results with identical output share one file. The server writes the blob files first and the database record second, so a committed record never points at a file that does not exist yet.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [D10. Why PostgreSQL for metadata and files for output?](../DESIGN-DEFENSE.md#d10-why-postgresql-for-metadata-and-files-for-output) · [`FileBlobs.path`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob.go#L33-L38)

</details>

### Why not print the local copy when the server is down?

**Level:** Newcomer

A client has a saved local result, but it cannot reach its configured shared server. Why does Receipt run pytest instead of printing the local result?

<details>
<summary>Answer</summary>

When a shared server is configured, a local hit is replayed only after the server confirms the key is not quarantined. Another client may have reported a conflict for that key since the local copy was saved, and the client cannot know that while the server is unreachable. So Receipt prints "shared status unavailable or quarantined; executing instead of local replay" and runs the tests. This loses the reuse benefit during an outage but never treats an unchecked result as trusted.

**Common wrong answer.** "The local copy is valid, so it replays and the outage does not matter." That is true only when no remote is configured.

**Learn more:** [A local copy still needs a shared trust check](../guide/02-let-another-client-reuse-the-result.md#a-local-copy-still-needs-a-shared-trust-check) · [D9. Why does a local hit still ask the server?](../DESIGN-DEFENSE.md#d9-why-does-a-local-hit-still-ask-the-server) · [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L68-L102)

</details>

### What is the shared-storage lock for?
<!-- check: guide/03-delete-old-data-without-breaking-a-reader.md -->

**Level:** Newcomer

A client is reading a result while cleanup is deleting unused files. What could go wrong without a lock, and what does the lock make each side do?

<details>
<summary>Answer</summary>

The server reads a result in two steps: it finds the database record, then opens the output file. If cleanup deleted that file between the two steps, the read would fail even though the record was valid. Receipt's lock prevents this. Readers take it in shared mode, so many reads can overlap. Uploads and cleanup take it in exclusive mode, so cleanup waits until every reader has finished and no reader starts while cleanup runs. The lock has two parts, a PostgreSQL advisory lock and a file lock on the blob directory, and it protects only programs that use it.

**Learn more:** [Why does the filesystem need a lock?](../guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock) · [F5. File locks: flock versus fcntl](../DESIGN-DEFENSE.md#f5-file-locks-flock-versus-fcntl) · [D16. How does cleanup avoid deleting a file that is still needed?](../DESIGN-DEFENSE.md#d16-how-does-cleanup-avoid-deleting-a-file-that-is-still-needed)

</details>

### Is a complete JSON document enough?
<!-- check: guide/04-check-data-before-accepting-it.md -->

**Level:** Newcomer

A client uploads a result as JSON. Every required field is present and has the right type. Must the server accept it?

<details>
<summary>Answer</summary>

No. Receipt checks two separate things. The structure check rejects unknown fields, repeated fields and missing required fields. The value check then applies the rules about meaning: the version must be 1, the exit code must be 0, the key must equal the hash of the included manifest, and each output stream must fit the size limit. A well-formed document can still claim a failed run or carry a key that does not match its manifest, so the shape check does not replace the value check.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L72)

</details>

### What does a checksum prove?

**Level:** Newcomer

Each saved entry carries a SHA-256 checksum of its contents. What can that checksum detect, and what can it not?

<details>
<summary>Answer</summary>

When Receipt reads an entry, it recomputes the hash of the payload and compares it with the stored checksum. A mismatch exposes damage or an accidental change, such as a truncated file, and Receipt treats the entry as corrupt. The checksum cannot detect a deliberate change: anyone who can rewrite the payload can also rewrite the checksum to match. It also says nothing about whether the tests really ran; it proves only that the bytes are the ones that were saved.

**Learn more:** [What does a checksum tell us?](../guide/04-check-data-before-accepting-it.md#what-does-a-checksum-tell-us) · [F1. Cryptographic hashes and content addressing](../DESIGN-DEFENSE.md#f1-cryptographic-hashes-and-content-addressing)

</details>

### 401 or 403?
<!-- check: guide/05-give-callers-different-permissions.md -->

**Level:** Newcomer

Alice's credential lets her download results. She tries to upload one and the server refuses. Did authentication fail?

<details>
<summary>Answer</summary>

No. Authentication asks whether the credential is valid: known, unexpired and not revoked. Authorization asks whether that credential allows the requested operation. Alice's credential is valid but carries only the `read` permission, and uploading needs `publish`. The server answers HTTP 403 Forbidden for a valid credential without the permission, and HTTP 401 Unauthorized for a missing, invalid, expired or revoked one. The four permissions are `read`, `publish`, `report` and `metrics`, and none of them implies another.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L24-L40)

</details>

### Which package does what?
<!-- check: guide/06-find-the-code-and-defend-a-decision.md -->

**Level:** Newcomer

You want to change how Receipt describes inputs. Which Go package do you open, and which packages hold the run flow, process execution and storage?

<details>
<summary>Answer</summary>

`internal/fingerprint` describes the inputs and calculates the key, starting at `Build` and `Manifest.Key`. `internal/app` reads the command line and combines lookup, execution, verification and publication, starting at `run`. `internal/runner` starts the test command and captures its output. `internal/cache` stores results locally, over HTTP and in shared storage. `internal/strictjson` checks JSON structure, and `internal/auth` defines credentials and permissions. All of these packages run inside one client or server process; a package is not a separate service.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L100-L143)

</details>

### How do you find one slow request?
<!-- check: guide/07-find-where-the-time-went.md -->

**Level:** Newcomer

Alice's small upload took much longer than she expected. How can the server operator find the timing for exactly her request?

<details>
<summary>Answer</summary>

Start the server with `--request-log`. Each request then gets a server-generated ID, which the server returns in the `X-Receipt-Request-ID` response header, and the server writes one redacted timing summary per request to standard error. Alice reports the ID, and the operator finds the matching summary without anyone printing her token or test output. The summary splits the request into stages, such as the credential check and waiting for locks, so a long wait for the lock shows up separately from reading files.

**Learn more:** [7. Find where the time went](../guide/07-find-where-the-time-went.md) · [Read one measurement carefully](../guide/07-find-where-the-time-went.md#read-one-measurement-carefully)

</details>

### What does the Cargo adapter refuse?
<!-- check: guide/08-check-what-a-rust-test-depends-on.md -->

**Level:** Newcomer

Alice's own crate has no build script, but a helper library it depends on has one. Will Receipt replay a previous success for `cargo test --frozen`?

<details>
<summary>Answer</summary>

No. Receipt follows the whole selected dependency graph before looking up a result. A build script is a program Cargo runs while compiling, and it could read files or settings Receipt does not record. Procedural macros also run code during compilation. When any selected package has either, Receipt refuses to reuse a result and runs Cargo normally. Receipt also refuses commands outside a short accepted grammar, any command without `--frozen`, Cargo configuration files, compiler overrides in the environment and dependencies that do not come from a registry or the repository.

**Learn more:** [8. Check what a Rust test depends on](../guide/08-check-what-a-rust-test-depends-on.md) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoBuildScript`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L30-L34)

</details>

### What do DNS, HTTPS and Caddy each do?
<!-- check: guide/09-follow-a-request-to-the-deployed-server.md -->

**Level:** Newcomer

A CI job contacts `https://receipt.sidsharma.dev`. Which part finds the server, which part protects the connection, and which part checks the CI job's token?

<details>
<summary>Answer</summary>

DNS turns the name `receipt.sidsharma.dev` into the droplet's IP address; Cloudflare answers that lookup but does not carry the cache request. HTTPS encrypts the connection and lets the client check the certificate for that hostname. Caddy, a web server on the droplet, ends the HTTPS connection and forwards the request to Receipt at `127.0.0.1:7777` on the same machine; this forwarding role is called a reverse proxy. Receipt itself checks the token and its permissions. A rejected token means the connection worked and access was refused, so the fix is a valid credential, not a new certificate.

**Learn more:** [9. Follow a request to the deployed server](../guide/09-follow-a-request-to-the-deployed-server.md) · [Encryption is not permission](../guide/09-follow-a-request-to-the-deployed-server.md#encryption-is-not-permission) · [F9. Bearer tokens, TLS termination and reverse proxies](../DESIGN-DEFENSE.md#f9-bearer-tokens-tls-termination-and-reverse-proxies)

</details>

### What does restoring a backup give you?

**Level:** Newcomer

The droplet's data is lost and you restore the encrypted R2 backup. What do you get back, and what do you not get?

<details>
<summary>Answer</summary>

The backup contains both stores a shared result needs: a PostgreSQL dump and an archive of Receipt's data directory with the blob files, plus the binary and selected configuration. Restoring gives back the state at the moment of the backup. Anything that changed afterwards is missing, so a credential revoked after the backup would come back unrevoked. A restore also does not settle a disagreement between two observations; it recovers earlier state, not proof that any result is correct. Without the private recovery key kept on the Mac, the encrypted backup cannot be decrypted at all.

**Learn more:** [Recovery: restore stored state, not trust](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#recovery-restore-stored-state-not-trust) · [D21. How is the server deployed, and how would we recover it?](../DESIGN-DEFENSE.md#d21-how-is-the-server-deployed-and-how-would-we-recover-it)

</details>

### Does an unchanged key prove the tests still pass?
<!-- check: guide/11-defending-the-design.md -->

**Level:** Newcomer

The key is the same as yesterday's. Does that prove the tests would produce the same output if you ran them today?

<details>
<summary>Answer</summary>

No. The key describes the inputs Receipt records, not every influence on a test. A test that asks a website for today's price, reads the clock, uses randomness or reads a file outside the recorded set can behave differently under the same key. The manifest type's own comment says it "does not prove output determinism". Receipt's answers to this gap are `--verify`, which reruns and compares, and quarantine, which stops reuse once two observations disagree. They expose some problems; they do not remove the uncertainty.

**Common wrong answer.** "Yes, the key covers all the inputs." It covers the declared inputs only.

**Learn more:** [Matching inputs do not prove matching behavior](../guide/01-remember-a-successful-run.md#matching-inputs-do-not-prove-matching-behavior) · [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [`Manifest`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L52-L60)

</details>

### Why can a cache hit still be expensive?
<!-- check: guide/12-profile-before-optimizing.md -->

**Level:** Newcomer

The tests did not execute, but Alice still waited 359 ms. Which work can remain, and which measurement should she inspect first?

<details>
<summary>Answer</summary>

Receipt must discover and hash inputs, probe the toolchain, construct the key, read and validate the saved result, replay output and update bookkeeping. Start with wall time and the Receipt timing stages. In the recorded workload fingerprinting had a 267 ms median, so a database index would not address the main cost of this standalone local hit.

**Learn more:** [Make a cache hit cheaper](../guide/12-profile-before-optimizing.md)

</details>

### Is allocated memory the same as peak memory?
<!-- check: guide/12-profile-before-optimizing.md -->

**Level:** Newcomer

A benchmark allocates about 15 MB each time it decodes a result. Does one hundred repetitions prove that the process needs 1.5 GB of RAM at once?

<details>
<summary>Answer</summary>

No. Allocation bytes count memory created over time; the garbage collector can reclaim earlier objects. Peak resident size measures a different quantity: memory resident in the process at its high point. Inspect both allocation profiles and process memory, and remember that separate PostgreSQL and child processes are not included in a Go heap profile.

**Learn more:** [Avoid repeatedly parsing large output](../guide/12-profile-before-optimizing.md#the-second-opportunity-avoid-repeatedly-parsing-large-output)

</details>

### Does a sequential scan prove that an index is missing?
<!-- check: guide/12-profile-before-optimizing.md -->

**Level:** Newcomer

A plan says sequential scan. Should we always add an index? What did the retention experiment actually establish?

<details>
<summary>Answer</summary>

No. Reading all rows can be sensible for an exact recount, a tiny table or a broad selection. The selective retention count benefited from a partial time index: it visited three cached blocks instead of 910. That does not prove faster full deletion, which also changes evidence, reads JSON sizes and commits. Compare complete workloads and index maintenance costs before adopting it.

**Learn more:** [Are our indexes appropriate?](../guide/12-profile-before-optimizing.md#are-our-indexes-appropriate)

</details>

### Why does streaming preserve the file hash?
<!-- check: guide/12-profile-before-optimizing.md -->

**Level:** Newcomer

A file contains `abc`. Receipt used to hash the permission prefix and file bytes in one slice. It now writes the prefix, then `ab`, then `c` into one hash state. Why is the digest unchanged, and why must the old file size not limit the read?

<details>
<summary>Answer</summary>

The same SHA-256 state receives the same bytes in the same order; chunk boundaries are not additional input. Computing separate chunk digests and combining them would be different. The size hint may be stale if the file grows, so it sizes the temporary buffer but does not cap the total bytes read. Receipt reads until EOF. This preserves the hash calculation for stable files without making concurrent edits an atomic snapshot.

**Learn more:** [Streaming file hashing](../guide/12-profile-before-optimizing.md#streaming-file-hashing-implemented-18-october)

</details>

### Why does an executable use no permissions prefix?
<!-- check: guide/13-hash-executables-and-measure-probes.md -->

**Level:** Newcomer

Receipt now streams executable bytes through the same helper used for project files. Why must it pass an empty prefix instead of copying the project-file permissions prefix?

<details>
<summary>Answer</summary>

The previous executable identity hashed only raw executable bytes. Adding permissions would change that byte sequence and break compatibility with old keys. Reusing a helper does not make the two input formats identical; each caller must preserve its own existing format.

**Learn more:** [Executable streaming](../guide/13-hash-executables-and-measure-probes.md#reuse-the-streaming-mechanism-with-a-different-prefix)

</details>

### Why not cache the slow probe by executable hash?
<!-- check: guide/13-hash-executables-and-measure-probes.md -->

**Level:** Newcomer

The Python probe costs much more time than reading the interpreter executable. Why not remember its output until the executable changes?

<details>
<summary>Answer</summary>

Packages can be installed or removed while the interpreter bytes stay unchanged. Go settings can change without changing its executable too. A probe cache would need a reliable invalidation contract for those inputs. This optimization retains fresh probes and measures their cost rather than silently reusing stale environment descriptions.

**Learn more:** [Fresh probes](../guide/13-hash-executables-and-measure-probes.md#why-probes-still-run-on-every-scan)

</details>

### Why is direct pytest an exception to streaming?
<!-- check: guide/13-hash-executables-and-measure-probes.md -->

**Level:** Newcomer

The command is an executable script named `pytest`. Why does Receipt still read all of it, and what would a future streaming implementation need to preserve?

<details>
<summary>Answer</summary>

Receipt reads its shebang to choose the Python interpreter. Keeping the existing full read preserves hashing and parsing from the same bytes, including long first lines and missing newlines. A future single-pass collector would need to preserve that accepted input behavior; simply adding a short line cap or reading the header separately could change the contract.

**Learn more:** [Direct pytest](../guide/13-hash-executables-and-measure-probes.md#preserve-the-direct-pytest-exception)

</details>

### Why can the first fingerprint reuse the Git root?
<!-- check: guide/14-reuse-the-git-root.md -->

**Level:** Newcomer

Receipt just found `/work/project` to load configuration. Which work can its first
fingerprint reuse, and which work must still happen?

<details>
<summary>Answer</summary>

It can reuse that root string within this invocation, assuming the repository
topology stays stable during initial collection. It must still ask Git for the
current file list and hash the current inputs. This removes one root query, not
the discovery of changed files. It is not a filesystem snapshot.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>

### Why discover the root again after tests run?
<!-- check: guide/14-reuse-the-git-root.md -->

**Level:** Newcomer

A command creates a new Git repository in its nested working directory. What
should the final fingerprint do?

<details>
<summary>Answer</summary>

It should ask Git for the root again. The new nested root changes the manifest,
so the before/after keys differ and Receipt refuses to store the result. Reusing
the initial root after execution would weaken that check.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>

### Why did removing a process barely change peak RAM?
<!-- check: guide/14-reuse-the-git-root.md -->

**Level:** Newcomer

Receipt starts one fewer Git process, and the measured hit is faster. Why might
maximum resident memory remain almost unchanged?

<details>
<summary>Answer</summary>

Peak memory measures the largest reported footprint, not the sum of memory used
over time. The removed Git process ran sequentially; another part of the run can
still set the same peak. Saving a process can reduce CPU and waiting without
reducing that maximum.

**Learn more:** [14. Reuse the Git root](../guide/14-reuse-the-git-root.md)

</details>
