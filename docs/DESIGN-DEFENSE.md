# Defending Receipt's design

Written 15 October 2026 against `main` at commit `5f5794f`. Every code link
points at exact lines on GitHub, and `scripts/docs/test_docs.py` fails if a
linked line range stops containing the symbol its link names.

## How to use this document

This document is organised as the questions an expert reviewer would ask about
Receipt, in the order they would ask them. Part 1 explains the operating-system,
database and cryptography rules the design rests on, so that each later answer
can say *why* and not only *what*. Part 2 walks the decisions from the cache key
outwards to the deployment. Each decision states the choice, the alternatives
and why they lost, the property it protects, what it costs, what happens when
it fails, and where the code is. Part 3 lists what an attacker, a crash or a
slow disk can do. Part 4 lists the known gaps with proposed fixes, and part 5
lists what this document cannot answer yet.

The alternatives in part 2 are retrospective comparisons written for this
document, unless a section links an earlier phase review that recorded them at
the time. Numbers are either measured, with the date and setup stated, or cited
from a linked record. Anything else is labelled as an estimate.

If you have not read the [working guide](WORKING-DOC.md), read chapters 1 to 3
first. They introduce the running example used here: Alice runs `uv run pytest`,
a CI worker runs `go test ./...`, and both share results through one server.

## Contents

- [Part 1. Foundations from first principles](#part-1-foundations-from-first-principles)
- [Part 2. The decision tree](#part-2-the-decision-tree)
- [Part 3. Threat and failure model](#part-3-threat-and-failure-model)
- [Part 4. Known gaps and pitfalls](#part-4-known-gaps-and-pitfalls)
- [Part 5. Questions this document cannot answer yet](#part-5-questions-this-document-cannot-answer-yet)
- [Appendix. How the upload-cost numbers were measured](#appendix-how-the-upload-cost-numbers-were-measured)

## Part 1. Foundations from first principles

### F1. Cryptographic hashes and content addressing

A hash function maps any sequence of bytes to a fixed-size value. SHA-256,
defined in [FIPS 180-4](https://csrc.nist.gov/pubs/fips/180-4/upd1/final),
produces 256 bits. Receipt writes those bits as 64 lowercase hexadecimal
characters, for example `71d261af…222a`.

A cryptographic hash promises three things that an ordinary checksum such as
CRC32 does not:

- Preimage resistance. Given a digest, you cannot find any input that produces
  it, short of trying about 2^256 inputs.
- Second-preimage resistance. Given one input, you cannot find a different input
  with the same digest.
- Collision resistance. You cannot find any two different inputs with the same
  digest. The birthday bound makes this the weakest of the three. A generic
  attack needs about 2^128 attempts for a 256-bit hash, which is still far out
  of reach. No practical SHA-256 collision is publicly known.

Content addressing means naming stored bytes by their hash. If two results
print the same output, their output files get the same name, so the server
keeps one copy. A reader can also check what it read: hash the bytes and compare
with the name. That check detects accidental damage. It is not a signature:
anyone who can write both the bytes and the name can make them agree.

A non-cryptographic hash such as xxHash is faster, but an attacker can construct
collisions for it on purpose. In a store shared by several writers, a collision
would let one writer's output be served in place of another's. Collision
resistance is therefore a security requirement here, not only a reliability one.

### F2. Canonical encoding

To hash a structured record, you must first turn it into bytes. JSON allows many
byte sequences for the same data. Object keys can appear in any order, spaces are
optional, `"<"` can be written as `"<"`, and `1` can be written as `1.0`.
Two programs that agree on the data can still produce different bytes and
therefore different hashes.

A canonical encoding removes those choices so that equal data always produces
equal bytes. Receipt does not use a published canonical-JSON standard. It relies
on the fact that one Go program, with one struct type, sorted inputs and Go's
`encoding/json` encoder, always produces the same bytes for the same values.
[D2](#d2-how-does-the-manifest-become-a-key-every-time-the-same-way) explains
the exact rules and their limits.

### F3. Files, inodes, directory entries and hard links

On Unix, a file's bytes and metadata live in an inode. A directory is a list of
names, and each name points at an inode. Two system calls create a name:

- `link(old, new)` adds a second name for an existing inode. If `new` already
  exists, it fails with `EEXIST` and changes nothing. That makes it an atomic
  "create this name only if it is absent" operation.
- `rename(old, new)` moves a name. If `new` exists, POSIX requires rename to
  replace it atomically, so a reader sees either the old file or the new one,
  never a mix. Plain rename cannot express "only if absent". Linux offers
  `renameat2` with `RENAME_NOREPLACE` and macOS offers `renamex_np` with
  `RENAME_EXCL`, but Go's `os` package exposes neither.

Both calls work only within one filesystem, which is why Receipt creates its
temporary file in the destination directory. Hard links also require filesystem
support. APFS and ext4 support them. FAT and exFAT do not.

### F4. Durability: the page cache and fsync

A successful `write` only copies bytes into the kernel's page cache in memory.
The kernel writes them to the disk later. If power fails first, they are lost.
`fsync(fd)` asks the kernel to write a file's data and metadata to stable
storage before returning.

A new name lives in the directory, not in the file. After creating a name, you
must also `fsync` the directory for the name itself to survive a crash. If you
create a new directory, its own name lives in its parent, so the parent needs
an `fsync` too.

On macOS, plain `fsync` hands data to the drive but does not force the drive's
own write cache to flush. Go's `File.Sync` on macOS therefore uses
`fcntl(F_FULLFSYNC)`, falling back to `fsync` only when the filesystem does not
support it (see `internal/poll/fd_fsync_darwin.go` in the Go source). On Linux,
`File.Sync` calls `fsync`. A flush waits for the storage device, so it costs
far more than an ordinary write. On the laptop used for
[D15](#d15-what-does-one-upload-cost-and-how-does-that-grow), one upload into an
empty store, which performs four such flushes plus a database commit, took
23 ms.

### F5. File locks: flock versus fcntl

Unix has two families of advisory file locks. Advisory means that a lock only
stops processes that also ask for it. A program that never asks can still read,
write or delete the file.

- `flock(fd, LOCK_SH | LOCK_EX)` locks a whole file. The lock belongs to the
  *open file description*, the kernel object created by one `open` call. Two
  separate `open` calls on the same file, even in the same process, produce two
  descriptions, and their locks conflict with each other. The lock is released
  when the last descriptor referring to that description is closed, which
  includes the process exiting or being killed.
- `fcntl(F_SETLK)` POSIX record locks belong to the (process, file) pair. Two
  threads in one process never conflict with each other. Worse, closing *any*
  descriptor for that file in the process releases all of the process's locks
  on it, even locks taken through a different descriptor.
- Linux also has open-file-description locks (`F_OFD_SETLK`) with flock-like
  ownership. macOS does not support them.

Receipt's server runs many requests as goroutines inside one process. Each
storage operation opens the lock file separately and must exclude the others.
Only `flock` gives that behaviour on both macOS and Linux. On Linux, `flock` on
NFS is emulated with byte-range locks, so its semantics change on network
filesystems. Receipt therefore supports local filesystems only.

`LOCK_NB` makes `flock` return `EWOULDBLOCK` at once instead of waiting.
Neither the blocking form nor the polling form guarantees first-come,
first-served order.

### F6. PostgreSQL transactions, row locks and advisory locks

A transaction groups statements so that they commit together or not at all.
PostgreSQL keeps old row versions (multi-version concurrency control) so that a
reader sees a consistent snapshot without blocking writers.

The isolation level decides which snapshot a statement sees. Under READ
COMMITTED, the default, each statement sees data committed before that
statement started. Two statements in one transaction can therefore see
different data if another transaction commits in between. REPEATABLE READ and
SERIALIZABLE give the whole transaction one snapshot. SERIALIZABLE also aborts
a transaction whose result could not have come from some serial order, and the
application must retry it.

`SELECT … FOR UPDATE` locks the selected rows until the transaction ends.
Another transaction that tries to update or lock the same rows waits.

An advisory lock is a lock on a number that the application chooses. PostgreSQL
attaches no meaning to the number. Receipt uses the transaction-level functions
(see the [PostgreSQL documentation on advisory locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)):

- `pg_advisory_xact_lock(key)` takes an exclusive lock. It waits while any other
  session holds the key in either mode.
- `pg_advisory_xact_lock_shared(key)` takes a shared lock. Many sessions can hold
  it together. It waits while another session holds the key exclusively.
- Both are released automatically when the transaction commits or rolls back,
  and when the session's connection ends. They cannot be released early.
- The key is a 64-bit integer, and keys are scoped to one database. Two schemas
  in the same database share one key space.
- PostgreSQL's lock manager makes a new request wait if it conflicts with a
  request *already waiting*. A waiting exclusive request therefore holds back
  later shared requests, so a steady stream of readers cannot starve a writer.
  `flock` gives no such promise.

### F7. Connection pools and context cancellation in Go

Go's `database/sql` keeps a pool of database connections. Ordinary queries
borrow any free connection per statement. A transaction, or a `*sql.Conn`
obtained with `DB.Conn`, pins one connection until it is closed. Receipt's
storage operations pin one connection each for their whole duration.

A Go `context.Context` carries a deadline and a cancellation signal. When the
context passed to `BeginTx` is cancelled, `database/sql` rolls the transaction
back by itself. `context.WithoutCancel(ctx)` produces a context that keeps the
values of `ctx` but is never cancelled, which stops that automatic rollback.

### F8. Processes, process groups, signals and standard streams

A process group is a set of processes that receive terminal signals together.
When you press Ctrl-C, the terminal sends `SIGINT` to every process in its
*foreground* process group. A signal sent to a negative process ID, `kill(-pgid,
sig)`, goes to every process in group `pgid`.

`SIGKILL` cannot be caught or ignored, so the process gets no chance to clean
up. `SIGTERM` can be caught. When a process dies from a signal, the shell
convention reports exit status 128 plus the signal number, for example 130 for
`SIGINT` (2) and 137 for `SIGKILL` (9).

Go's `os/exec` connects a child's standard input to the null device,
`/dev/null`, when `Cmd.Stdin` is nil. Reads from `/dev/null` return end of file
immediately. That differs from a closed descriptor, where a read fails with
`EBADF`. When `Cmd.Stdout` is not an `*os.File`, `os/exec` connects the child to
a pipe and copies from it, so the child does not see a terminal.

### F9. Bearer tokens, TLS termination and reverse proxies

A bearer token is a secret string: whoever presents it is treated as its owner.
Nothing binds it to one machine, so a copied token works anywhere until it
expires or is revoked. TLS encrypts the connection and lets the client verify
the server's certificate, so a network observer cannot read the token.

A reverse proxy accepts the client's connection and forwards requests to
another server. When the proxy terminates TLS, the second hop can be plain HTTP
over the loopback interface (`127.0.0.1`), which never leaves the machine. The
proxy then sees every decrypted request.

Storing a hash of a token, rather than the token, means that someone who reads
the database cannot use what they read. Password storage uses slow functions
such as bcrypt or Argon2 because human passwords are guessable. A random
256-bit secret is not guessable, so a single SHA-256 is enough
([D18](#d18-how-are-credentials-generated-stored-and-checked)).

### F10. Admission control with a buffered channel

A Go buffered channel of capacity *n* works as a counting semaphore. Sending
into it takes a slot, receiving from it frees one. A `select` with a `default`
case turns a full channel into an immediate refusal instead of a wait. Receipt
answers that refusal with HTTP 503 and `Retry-After: 1`.

## Part 2. The decision tree

The decisions are ordered so that each one depends only on earlier ones.

### D1. What exactly goes into the cache key?

**Decision.** The key is the SHA-256 of a manifest: a JSON record of everything
Receipt knows to record about one test invocation. The
[`Manifest`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L52-L60)
type has these fields, and [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L97-L134)
fills them.

| Field or input kind | Contents |
| --- | --- |
| `version` | Always 1. A future format change would use 2. |
| `root`, `directory` | Absolute paths of the Git root and the working directory. |
| `platform` | Go's `GOOS/GOARCH` of the client binary, for example `darwin/arm64`. |
| `command` | The exact argument list after `--`. |
| `file` inputs | Every file from `git ls-files --cached --others --exclude-standard`, plus `.receipt.json` even if ignored, plus `extra_inputs`. The `.receipt/` cache directory is excluded. |
| `environment` inputs | Every environment variable except `RECEIPT_TOKEN` and those in `ignore_environment`, as name plus SHA-256 of the value. |
| `executable` input | The absolute path of the resolved command and the SHA-256 of its bytes. |
| `toolchain` inputs | A runtime probe: Python version and installed distributions, `go env -json`, or the Cargo tool, metadata and source hashes. |

[`discoverFiles`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L136-L181)
collects the paths. [`fileInput`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L183-L205)
hashes each one as `SHA-256(octal permission bits + NUL byte + file bytes)`, so
`chmod +x` changes the key. A tracked file that was deleted hashes as
`SHA-256("deleted")`. That marker cannot equal a real file's hash except by a
SHA-256 collision, because every real preimage contains a NUL byte after the
permission digits and the string `deleted` contains none. Symlinks, submodules
and other non-regular files make `Build` fail, and Receipt then runs the command
uncached. [`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L207-L220)
hashes the environment, and [`toolchainProbe`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L267-L290)
selects the runtime probe.

**Alternatives.**

- Hash only the Git commit. Rejected because uncommitted edits and untracked
  files would be ignored, so a dirty working tree could replay a stale success.
- Trace the files each test actually opens. Rejected for now because it needs a
  sandbox or syscall tracing on two operating systems, and it is a larger
  project than the cache itself. It would produce narrower keys and fewer
  misses.
- Exclude the environment instead of hashing it. Rejected because tests read
  variables such as `DATABASE_URL` or `TZ`. Hashing everything favours an extra
  miss over a wrong hit. The user can opt out per variable.

**Invariant.** If any recorded input differs, the key differs. The reverse
claim does not hold. A matching key says nothing about inputs Receipt did not
record, such as the network, the clock, random seeds or ignored files.

**Cost.** Building a manifest reads and hashes every discovered file, so it
costs time proportional to the repository's total bytes, plus one `git` call and
one probe process. A miss builds the manifest twice
([D5](#d5-why-scan-the-inputs-twice-and-what-can-the-two-scans-miss)). The
manifest's size grows with the file count. For this repository, 165 inputs
produced a 29,612-byte manifest (measured 15 October 2026 by running Receipt on
its own checkout). Two Phase 6 and Phase 7 measurements show the cost against
the work avoided: a CodeCraft pytest hit had a 127 ms median against 253 ms for
running the tests directly, while a regex-syntax Cargo hit had a 434 ms median
against 139 ms directly ([MEASUREMENTS.md](MEASUREMENTS.md),
[Phase 7 review](PHASE-REVIEWS.md#phase-7-review-bounded-cargo-support)).

**Failure.** A probe that fails, a symlink input or a missing extra input makes
Receipt print a diagnostic and run the command uncached. It never guesses.

**Portability limit.** Absolute paths and the whole environment are in the key.
Two machines share a key only if they use the same checkout path, the same tool
paths and the same environment values. Receipt's own CI achieves this by
constructing a fixed environment in
[`test_environment`](https://github.com/sidsharma96/receipt/blob/main/deploy/ci/self_test.py#L18-L28).
An ordinary shell environment contains values such as `PWD`, `SHLVL` and
terminal session IDs that differ between sessions, so keys from two terminals
can differ even on one laptop.

**Go test coverage.** For `go test`, Receipt hashes the `go` executable and the
output of `go env -json`, after removing the random suffix of Go's temporary
build directory in [`stableGoEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_environment.go#L11-L39).
That covers the Go version, `GOFLAGS`, `GOROOT` and the path of any `go.work`
file. It does not hash the contents of the module cache, a `go.work` file above
the repository root, or `replace` directories outside the repository. Receipt's
CI declares its one dependency's module directory as an extra input by hand,
in the [`config.write_text`](https://github.com/sidsharma96/receipt/blob/main/deploy/ci/self_test.py#L74-L78)
call of its helper, for this reason.

### D2. How does the manifest become a key every time the same way?

**Decision.** [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L65-L76)
copies the inputs, sorts them by kind and then by name, encodes the manifest
with Go's `json.Marshal`, and returns `Hash` of those bytes.

Four rules make the bytes deterministic:

1. Go encodes struct fields in declaration order, so `version`, `root`,
   `directory`, `platform`, `command`, `inputs` always appear in that order.
2. Inputs are sorted by (kind, name) using byte-wise string comparison.
3. No two inputs share a (kind, name) pair. `sort.Slice` is not stable, so equal
   pairs could otherwise come out in either order.
   [`Manifest.Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/json.go#L63-L88)
   rejects a repeated pair, which makes the sort order total. `Build` cannot
   produce duplicates because it collects paths in a map.
4. `json.Marshal` writes no optional whitespace, and its escaping is fixed:
   `<`, `>` and `&` become `<`, `>` and `&`, and invalid UTF-8
   bytes become the replacement character U+FFFD.

The server recomputes the key when it accepts an upload, in
[`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L51-L73),
and rejects a result whose key does not match its manifest.

**Alternatives.** A published canonical form such as RFC 8785 (JSON
Canonicalization Scheme) would let a non-Go client compute identical keys. It
was not needed while every client and server is the same Go program, and Go's
standard library does not implement it.

**Invariant.** The same manifest values always produce the same key, on any
machine running this code.

**Cost.** Sorting is O(n log n) for n inputs, and encoding and hashing are linear
in the manifest's size. The server repeats this for every upload and every
record it validates during garbage collection.

**Failure and limits.**

- The key depends on Go's encoder. A non-Go client must reproduce the HTML
  escaping and the UTF-8 replacement exactly. If a future Go release changed
  `encoding/json` output, keys would change, and the server would reject
  uploads from clients built with the other version. The `version` field does
  not capture this. Go 1's compatibility promise makes such a change unlikely.
- Because invalid UTF-8 becomes U+FFFD, two file names that differ only in
  invalid bytes encode identically. Their contents are still hashed, so this
  only matters if such a rename leaves the contents the same.
- A manifest with `"inputs": null` and one with `"inputs": []` produce different
  keys. Both are accepted for compatibility. `Build` always produces `[]`.

### D3. Why SHA-256?

**Decision.** SHA-256 for keys, output files, envelope checksums and token
digests, through [`Hash`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L62-L63).

**Alternatives.**

- xxHash or another non-cryptographic hash. Rejected because a writer could
  craft collisions ([F1](#f1-cryptographic-hashes-and-content-addressing)). A
  collision in an output file name would let one result's output be served for
  another. A collision in the key would let a crafted manifest share a key with
  a victim's.
- BLAKE3. Faster than SHA-256 in software and also collision resistant. It is
  not in Go's standard library, and the repository's lint rules allow only the
  standard library and the PostgreSQL driver. Go's `crypto/sha256` uses the CPU's
  SHA instructions on arm64 and on amd64 processors that have them.
- SHA-1 or MD5. Rejected because practical collisions are public.

**Cost.** Hashing is linear in the bytes hashed. Receipt has not measured
hashing separately from file reading. The `fingerprint` timing stage includes
both.

**Revisit if** fingerprinting large repositories becomes the dominant cost and
measurements show hashing, not reading, is the bottleneck.

### D4. Why is `RECEIPT_TOKEN` kept out of the key?

**Decision.** Since 15 October 2026, Receipt's own bearer-token variable,
[`TokenEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L40-L43),
is never an environment input, and the test command does not inherit it.
[`environmentInputs`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L207-L220)
always skips it, and [`withoutToken`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L41-L47)
removes it from the child's environment in `Run`. The client still reads it,
through [`os.Getenv(fingerprint.TokenEnvironment)`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L285-L287) in `run`,
to authenticate to the server.

**What happened before.** Every environment variable was hashed into the
manifest, so the manifest contained `SHA-256(token)`. The credential table
stores exactly the same value as `token_hash`, computed by
[`digest`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L158-L161).
Receipt's CI sets the variable in [`env['RECEIPT_TOKEN']`](https://github.com/sidsharma96/receipt/blob/main/deploy/ci/self_test.py#L79-L82).
That had four consequences:

1. Rotating a credential changed every key. Credentials live at most 30 days,
   so each renewal made every earlier result unreachable.
2. Two different credentials never shared a result, even with otherwise
   identical environments. Alice's results could not serve CI.
3. The digest that the server treats as private was copied into every
   manifest. Manifests are uploaded with results, returned to any read-scoped
   caller who asks for that key, and kept in local `.receipt/` entries and
   history files. The token cannot be recovered from the digest, because the
   secret part has 256 random bits
   ([F1](#f1-cryptographic-hashes-and-content-addressing)), but it linked every
   result to one credential.
4. Unsetting the variable to work offline changed the key, so local hits with
   and without a remote never matched.

**Why the child must lose the token too.** The invariant from
[D1](#d1-what-exactly-goes-into-the-cache-key) is that everything the test can
read from its environment is in the key. If the key ignored the token but the
test could still read it, a test that behaved differently with and without a
token would get a wrong hit. Removing it from both keeps the invariant. A test
that needs a credential must receive it under another variable name, which is
then hashed like any other. This is a guard against accidental use, not a
security boundary: on Linux, a test running as the same user can still read its
parent's environment from `/proc/<pid>/environ`.

**Alternatives.** Keeping the token in the key, which is the strict reading of
"hash everything", was the earlier behaviour and cost a cold cache every 30
days. Adding `"ignore_environment": ["RECEIPT_TOKEN"]` to the `.receipt.json`
that CI writes would have fixed CI only, and would still have passed the token
to tests.

**Cost of the change.** Keys of runs made with `RECEIPT_TOKEN` set change once:
their manifests lose one environment input. Keys of runs made without it are
unchanged, because their manifests never had that input. Stored results are
not touched and remain valid; they are simply no longer looked up, and
`shared prune` removes them after the retention period. For Receipt's own CI
this costs nothing extra: CI lists the client binary in `extra_inputs`, so any
new client build already changes every CI key. No database or store migration
is needed.

### D5. Why scan the inputs twice, and what can the two scans miss?

**Decision.** On a miss, [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L314-L325)
builds the manifest again after the command finishes and publishes only if the
key is unchanged. For example, `uv run` may install packages during the first
run, which changes the Python probe.

**Alternatives.** Copy the inputs to a private snapshot before running, or run
inside a read-only filesystem view. Either would make the inputs immutable for
the run, at the cost of copying the repository or building a sandbox.

**Invariant.** A result is never published under a key whose inputs were
visibly different after the run.

**Cost.** A second full scan on every miss. A hit scans once.

**Failure.** Two scans cannot see a file that changed and changed back during
the run (an ABA change), or anything Receipt does not record. A file a test
writes and deletes between the scans is invisible too.

### D6. Why cache only successes, replay exact bytes and never normalise output?

**Decision.** Only exit code 0 is reusable ([`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L51-L73)).
A hit writes the saved stdout and then the saved stderr. `--verify` compares
exit code and both streams byte for byte.

**Alternatives.** Caching failures would replay a failure after a flaky
infrastructure error. Normalising output, for example removing timings, would
need per-tool rules and would make two different observations look equal.

**Cost.** A hit loses the original interleaving of stdout and stderr, terminal
colours and every side effect of the tests. Verification flags harmless timing
text as a conflict.

### D6b. What do `--verify`, `--refresh` and `--bypass` do?

**Decision.** `receipt run` has three modes besides the normal one, and a run
may use at most one of them. Passing two prints "choose only one of --bypass,
--refresh, --verify" and runs nothing ([`modes`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L257-L265)).

- `--bypass` runs the command directly and returns its exit code. It computes
  no key and reads or writes no cache ([`o.bypass`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L270-L273)).
- `--verify` changes only what happens on a hit, whether the hit came from the
  local cache or from the server. Receipt runs the tests again, scans the inputs
  again and then decides in [`verify`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L163-L191).
  On a miss it behaves like a normal run.
- `--refresh` skips reuse. It still reads the local entry for this key, if one
  exists, so that it can compare against it ([`lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L68-L76)).
  It does not ask the server for one.

**Exit codes of `--verify` on a hit.**

| Situation | What Receipt does | Exit code |
| --- | --- | --- |
| The input key changed between the two scans | Prints "verification inconclusive", quarantines nothing | 1 |
| Exit code, stdout or stderr differs byte for byte, or the output exceeded the capture limit | Writes a local quarantine marker, removes the local entry, reports the conflict's hashes to the server, prints "verification mismatch" | The new run's exit code, or 1 if the new run exited 0 |
| Everything matches | Prints "verification matched byte-for-byte" | 0 |

So a script can tell a match (0) from a problem (non-zero), but it cannot tell
an inconclusive run from a mismatch by the exit code alone; it must read the
message on stderr.

**When `--refresh` quarantines.** Only one case quarantines: the fresh run
**fails**, a cached success for the same key existed locally, and the inputs
did not change during the run ([`failed`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L193-L214)).
A failure after a recorded success means the two observations disagree, so
Receipt writes the marker, removes the entry and reports the conflict. The exit
code is the fresh run's. If the fresh run succeeds, [`publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L216-L245)
removes the old local entry and stores the new one, even if the output differs.
The local cache does not compare them. The upload that follows does: a
PostgreSQL server compares digests, and if the server already holds a different
result for the key it answers 409 and the client quarantines the key locally
([D11](#d11-what-happens-when-two-clients-publish-the-same-key)).

**Order after a successful run.** `publish` stores the entry locally, then
uploads it, then prunes the local cache ([D8](#d8-how-does-the-local-cache-stay-bounded-and-why-do-quarantine-markers-win)). A key quarantined locally is
neither stored nor uploaded.

**Alternatives.** A separate exit code for "inconclusive" would let scripts
retry without parsing text. It was not added; this is a candidate improvement,
not a defect. Making `--refresh` overwrite without any comparison would lose
the one signal it can give cheaply: a success followed by a failure on the same
inputs is direct evidence of a flaky or input-incomplete test.

**Cost.** `--verify` and `--refresh` always run the tests, so they cost a full
run each time. Each quarantine they cause is permanent ([G15](#g15-quarantine-cannot-be-undone)).

### D7. How is a local result published so that a crash never leaves a partial entry?

**Decision.** [`Disk.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L149-L193)
uses this exact sequence:

1. [`MakeDirs`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/dirsync.go#L24-L50)
   creates `.receipt/entries` if needed and syncs the parent of each directory
   it creates, so the new directory's name is durable too ([F4](#f4-durability-the-page-cache-and-fsync)).
2. `CreateTemp` creates `entries/.pending-<random>` in the same directory.
3. Write the whole encoded envelope.
4. `File.Sync` makes the bytes durable ([F4](#f4-durability-the-page-cache-and-fsync)).
5. `Close`.
6. `os.Link(tmp, entries/<key>.json)` publishes the name only if it is absent.
7. On `EEXIST`, read and validate the existing entry with `Disk.Get`. A valid
   existing entry means success. A corrupt one, or a directory in the way, is an
   error. Receipt never overwrites it.
8. Open the directory and `Sync` it so that the new name is durable.
9. The deferred `os.Remove(tmp)` deletes the temporary name. The inode lives on
   under the final name.

[`FileBlobs.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob.go#L71-L112)
uses the same sequence for shared output files, and
[`FileBlobs.Bind`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob_binding.go#L11-L71)
uses it for the store identity marker.

**Why `link` and not `rename`.** `rename` replaces an existing destination. Two
clients publishing different observations for one key would each overwrite the
other, and the last writer would win silently. `link` fails with `EEXIST`, so
the first durable observation stays and later ones are compared against it
([F3](#f3-files-inodes-directory-entries-and-hard-links)). Writing the final
name directly with `O_EXCL` would also be create-if-absent, but a reader could
then open a half-written file.

**What a crash leaves.**

| Crash point | State after reboot | Next run |
| --- | --- | --- |
| Before step 4 finishes | A partial or empty `.pending-*` file. No final name. | Miss, then republish. |
| Between steps 6 and 8 | The final name may or may not have survived. If it survived, it points at synced bytes. | Hit, or miss and republish. |
| After step 8 | A complete entry. The `.pending-*` name may reappear, because its removal was not synced. It is a second name for the same bytes. | Hit. |

A partial file can never appear under the final name, because the bytes were
synced before the name existed. Local `.pending-*` leftovers do not count
towards the size limit. Since 15 October 2026,
[`removeStalePending`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L353-L365)
in `prune` deletes any that are more than an hour old. A younger one may belong
to a publication still running in another process, which removes its own file
within seconds.

Before 15 October 2026, `os.MkdirAll` created `entries/` and each shared shard
directory such as `blobs/ab/` without syncing their parents. After a power loss
a new directory, and the files in it, could vanish. `MakeDirs` closed that gap.

**Cost.** Two `fsync` calls per entry, which on macOS are full cache flushes,
plus one more for each directory created, which happens once per cache and
once per shard.

### D8. How does the local cache stay bounded, and why do quarantine markers win?

**Decision.** After each successful publish, [`prune`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L367-L418)
lists `entries/`, sorts by modification time and deletes the oldest until the
total is at most `max_bytes` (1 GiB by default). Each hit touches the entry's
modification time in [`runSession.replay`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L141-L161),
so this is least-recently-used eviction.

A quarantine marker is a file at `.receipt/quarantine/<key>`.
[`quarantine`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L221-L229)
writes it with a temporary file and `rename`, then syncs `quarantine/`, so the
marker survives a power loss. [`Disk.Remove`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L195-L209)
syncs `entries/` after removing the matching entry. Before 15 October 2026
neither directory was synced, and a power loss could undo both changes.
[`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L68-L102)
checks for it before it opens the entry, and
[`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L216-L245)
checks it before saving. A marker therefore overrides an entry even if a racing
process republishes the entry after the marker was written.

**Cost.** Each miss lists the whole entries directory and sorts it: O(N log N)
for N entries. Concurrent processes can each delete entries, so the limit is
approximate.

### D9. Why does a local hit still ask the server?

**Decision.** With a remote configured, a local hit calls
[`Remote.Check`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L112-L126)
(`GET /v1/status/<key>`) before replaying. HTTP 204 allows replay. HTTP 409
writes a local quarantine marker. Any other answer, including a timeout, means
run the tests.

**Alternatives.** Replay the local copy offline and check later. Rejected
because another client may have quarantined the key since the copy was saved,
and a check after replay cannot take the output back.

**Cost.** One network round trip per hit, and a server outage turns every local
hit into an execution. A legacy disk server answers 501 for status, so every
hit against it executes.

**The disk server has no conflict detection at all.** A server started without
`--postgres` stores entries with the same [`Disk.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L150-L193)
that the client uses locally. When an entry for the key already exists, `Put`
keeps it and returns success as long as the stored entry is valid, so
[`putEntry`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L346-L369)
answers 204 to an upload whose output differs. The first writer wins and the
disagreement is never recorded. Conflict reports and status checks get 501
from [`cacheAPI.shared`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L263-L272),
so no client learns about the disagreement either.

### D10. Why PostgreSQL for metadata and files for output?

**Decision.** The shared store keeps one row per key in
`receipt_entries_v2` with the result's manifest and references to its output
files (a `blobRecord`), and stores output bytes as content-addressed files under
`blobs/<first two hex chars>/<hash>`. See
[`Postgres`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L73-L79)
and the schema in [`Postgres.Migrate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L81-L130).

**Alternatives.**

- Everything in PostgreSQL as `bytea`. One transaction would cover both, which
  removes orphan files. But 8 MiB outputs would inflate the database, its
  write-ahead log and its backups, and identical outputs would need explicit
  deduplication.
- Everything in files. No database to run, but quarantine evidence, quotas and
  credentials would need their own crash-safe file formats and locking.
- Object storage such as S3 or R2. It would separate output from the server,
  but it has no file locks, so the cleanup protocol in D12 would need a new
  design such as reference counts or epoch-based collection.

**Invariant.** A reader never sees a database record whose output files were
not yet durable. [`Postgres.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L191-L242)
inserts the record inside its transaction, then writes both output files with
`fsync`, then commits. The record becomes visible only at commit, so the files
are durable first. Since 15 October 2026 the insert comes before the file
writes, so that a quota refusal writes no files; before that, the files were
written first.

**Failure.** If the commit fails after the files were written, they remain as
orphans that nothing references. `shared gc` removes them later. No
single rollback covers both stores.

### D11. What happens when two clients publish the same key?

**Decision.** [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L267-L351)
runs `INSERT … ON CONFLICT (key) DO NOTHING`, reads back the stored digest and
compares. The digest is the SHA-256 of the stdout hash, stderr hash and exit code
([`ConflictReport.digest`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L51-L58)).
Duration and creation time are excluded. Equal digests make the upload a no-op.
Different digests set `quarantined = TRUE`, store the new observation as
evidence, commit, and return 409. Every later read of that key returns 409.

All of this needs PostgreSQL. A disk server ([D9](#d9-why-does-a-local-hit-still-ask-the-server)) accepts a differing upload
with HTTP 204 and keeps the first result, so two clients that disagree are
never told.

**Alternatives.** Last writer wins hides disagreement. Keeping both results and
choosing one at read time needs a rule for which run was correct, and Receipt
has no evidence to decide that.

**Cost.** Quarantine is permanent. There is no command to trust a key again,
and a holder of the `report` scope can quarantine any key whose manifest it
knows ([D18](#d18-how-are-credentials-generated-stored-and-checked)).

The `SELECT … FOR UPDATE` in `publishTx` can never wait, because every caller of
`publishTx` already holds the exclusive lifecycle lock ([D12](#d12-why-does-one-lock-serialize-every-shared-writer)).
It is defence in depth, kept so that the row logic stays correct if the global
lock is ever narrowed.

### D12. Why does one lock serialize every shared writer?

**Decision.** Every shared storage operation enters through
[`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L72-L119):

1. Pin one database connection from the pool.
2. Begin a READ COMMITTED transaction with `context.WithoutCancel`.
3. Take the PostgreSQL advisory lock `lifecycleLock`, shared for reads and
   exclusive for uploads, reports and maintenance.
4. Take a `flock` on `blobs/.lifecycle.lock` in the same mode, through
   [`lockBlobDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/lifecycle_unix.go#L15-L57).
   [`openLockFile`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/lifecycle_unix.go#L59-L79)
   opens the lock file with `O_NOFOLLOW`, so a symlink at that name makes the
   open fail, and then checks with `fstat` that it opened a regular file. Before
   15 October 2026 it checked the name with `Lstat` and then opened it, and a
   symlink swapped in between would have been followed.

[`storageTx.Rollback`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L61-L70)
and `Commit` end the transaction, which releases the advisory lock, then return
the connection and close the lock file, which releases the `flock`.

| Operation | Mode | Code |
| --- | --- | --- |
| Read a result | shared | [`Postgres.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L146-L189) |
| `shared stats` | shared | [`Postgres.Stats`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L273-L286) |
| Upload a result | exclusive | [`Postgres.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L191-L242) |
| Report a conflict | exclusive | [`Postgres.Report`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L244-L265) |
| `shared gc` | exclusive | [`Postgres.Collect`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L382-L451) |
| `shared prune` | exclusive | [`Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L470-L509) |
| `shared limits` | exclusive | [`Postgres.SetLimits`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L288-L316) |
| Binding check and usage recount at every startup and every CLI command | exclusive | [`initializePolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L129-L169) |

The status check, [`Postgres.Quarantined`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L132-L144),
is one single-row query and takes no lifecycle lock.

**Why global and not per key.** Two keys can reference one output file, and all
keys draw on one storage budget. Suppose uploads for keys A and B both reference
output file X, and garbage collection is deciding whether X is unused. A lock on
A or B does not tell the collector that X is about to gain a reference. And two
uploads that each see 10 MiB of free budget could both add 8 MiB. One lock over
the whole store answers both questions with the simplest possible argument.

**Alternatives.**

- Per-key locks plus a lock per output file plus a locked budget counter. More
  concurrency, but every operation must take several locks in a fixed order,
  and the counter must be correct after every crash.
- SERIALIZABLE transactions with retry. PostgreSQL would detect conflicting
  reads and writes in the database, but it cannot see the filesystem, so it
  cannot protect output files from the collector.
- Reference counts stored per output file. Collection would become cheap, but
  every crash between a file write and a count update leaves a wrong count, and
  a wrong count deletes a needed file.

**Why two locks.** The advisory lock coordinates every process that uses the
database. If the server's database connection dies, PostgreSQL releases the
advisory lock at once, but the server may still be writing or deleting files.
The `flock` stays held until that process returns, so a collector in another
process that now gets the advisory lock still waits for the files. The tests
`TestDatabaseConnectionLossKeepsFilesystemExclusion` and
`TestLifecycleCancellationKeepsFilesystemExclusion` cover this. A Go mutex would
not work, because the `shared gc` command is a separate process.

**Why `context.WithoutCancel` for `BeginTx`.** If the request's context were
cancelled, `database/sql` would roll back the transaction and release the
advisory lock while file work was still running. With `WithoutCancel`, the
transaction ends only when the deferred `Rollback` runs, after the file work has
returned. Individual queries still use the request context, so cancellation
still stops the next query.

**Deadlock argument.** Every caller takes the advisory lock before the `flock`.
No code waits for the advisory lock while holding the `flock`. No code under the
lock asks the pool for a second connection. So no cycle of waits can form. The
one process that can hold the `flock` without the advisory lock (after losing
its connection) never waits for the advisory lock again.

**Why `flock` and not `fcntl`.** The server holds many shared locks at once from
different goroutines, each with its own open of the lock file, and an exclusive
request from another goroutine must wait for all of them. `flock` locks belong
to each open, so this works. POSIX `fcntl` locks belong to the process, so a
goroutine asking for an exclusive lock would not conflict with shared locks held
by its own process. Closing any one descriptor would also drop them all
([F5](#f5-file-locks-flock-versus-fcntl)).

**Why polling, and its lack of fairness.** `lockBlobDirectory` tries
`flock(LOCK_NB)` and, if the lock is busy, waits for a 10 ms ticker or context
cancellation before trying again. A blocking `flock` call cannot be interrupted
by a Go context, so a cancelled request would stay stuck in the kernel. The
polling loop has no queue. If shared holders overlapped continuously, an
exclusive waiter could fail every attempt and starve. In practice the advisory
lock in front prevents this: PostgreSQL makes new shared requests wait behind a
waiting exclusive request ([F6](#f6-postgresql-transactions-row-locks-and-advisory-locks)),
so the set of shared `flock` holders drains. The remaining costs are up to
10 ms of extra delay per contended acquisition, because the advisory lock is
released a moment before the `flock`, and the loss of that protection for any
future code path that takes the `flock` without the advisory lock.

**Cost.** All uploads, reports and maintenance run one at a time. Reads run
together but wait behind any exclusive operation. In the Phase 6 measurement,
four clients re-uploading existing results into a 128-entry store spent a mean
of 54.56 ms in the advisory-lock call against a 74.67 ms median upload
([MEASUREMENTS.md](MEASUREMENTS.md)).

**Failure.** If the `flock` cannot be acquired before the 25-second request
deadline, the request fails with 500 and the client runs its tests instead.

### D13. Which advisory lock keys exist, and what does each guard?

The keys are the ASCII bytes of `receipt` (`72 65 63 65 69 70 74` in
hexadecimal) written as decimal digits, plus a small offset: 0, 1 or 2.

| Key | Taken by | Mode | Guards |
| --- | --- | --- | --- |
| `72656365697074` | [`Postgres.Migrate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L81-L130) | exclusive | Concurrent `CREATE TABLE IF NOT EXISTS` at startup |
| `72656365697075` | [`lifecycleLock`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L20-L22) through `Postgres.lifecycle` | shared or exclusive | Shared metadata, output files, quotas and collection (D12) |
| `72656365697076` | [`credentialLock`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L15-L18), taken by [`Postgres.Migrate` in internal/auth](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L24-L34) | exclusive | Creating the credential table |
| `72656365697076` | [`credentialLock`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L15-L18), taken by [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74) | exclusive | Counting rows and inserting a credential |

Until 15 October 2026 the credential code reused the storage lock's key,
`72656365697075`. Earlier documents described it as a separate, dedicated lock,
which was wrong at the time; the dated corrections are in
[PHASE-REVIEWS.md](PHASE-REVIEWS.md#corrections-15-october-2026). The fix gave
credentials their own key.

**Consequence.** Issuing a credential, and the credential-table migration that
runs at server startup and before every `receipt auth` command, now wait only
for each other. They no longer wait for in-flight reads, uploads or collection,
and reads and uploads no longer queue behind them. The test
`TestCredentialIssuanceDoesNotWaitForStorageLock` holds a shared storage lock
and checks that migration and issuance both finish within two seconds.

One pause remains. Every `receipt auth` command first calls `openShared`, which
runs the storage migration, and that migration takes the exclusive lifecycle
lock, `flock` included, once, through
[`openShared` and then `credentials.Migrate`](https://github.com/sidsharma96/receipt/blob/main/internal/app/auth_command.go#L74-L86)
in `authCommand`. So running `receipt auth issue` on the server still waits for
in-flight storage work once, before issuance starts
([G1](#g1-receipt-auth-still-takes-the-storage-lock-once)).

**Is anything lost by separating them?** No code relied on issuance excluding
storage work. The credential table and the storage tables share no rows, and
`Postgres.Authenticate` takes no lock at all. The server never issues credentials; only
`receipt auth issue` does. If an old build and a new build of that command ran
at the same moment, they would take different keys, and two of them racing for
the last free slot could leave 1,025 rows. That needs two operators issuing at
the 1,024-row cap with mixed builds.

**What the lock prevents.** [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L74)
counts rows and inserts only if the count is below 1,024. Under READ COMMITTED,
two issuers without a lock could both count 1,023 and both insert, leaving 1,025
rows. The test `TestCredentialCapacityUnderConcurrentIssuance` runs eight
concurrent issuers against one free slot.

### D14. Why is READ COMMITTED enough?

Every writer and every maintenance operation holds the exclusive lifecycle lock
for its whole transaction. While one holds it, no other transaction changes
`receipt_entries_v2`, `receipt_evidence_v2` or the policy row. A statement under
READ COMMITTED then sees the same data as a snapshot taken at the start of the
transaction would, so write skew and lost updates cannot occur. Readers hold the
shared lock, so no writer commits while they read a record and its files.
Correctness therefore comes from the lock, not from the isolation level.

The credential table has the same structure: issuance is serialized by the lock
in [D13](#d13-which-advisory-lock-keys-exist-and-what-does-each-guard), and
revocation is a single `UPDATE`.

Alternatives were SERIALIZABLE with a retry loop, or one counter row locked with
`FOR UPDATE`. Both would work for the database tables alone. Neither protects
the output files.

### D15. What does one upload cost, and how does that grow?

Under the exclusive lock, one `Postgres.Put` does the following work. Let B be
the number of output files, M the number of result rows and E the number of
evidence rows.

| Step | Work | Code |
| --- | --- | --- |
| Read the policy row and the `.store-id` marker | constant | [`Postgres.policy`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L171-L193) |
| Check whether the key is quarantined | one index lookup | [`Postgres.checkBlobQuota`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L337-L380) |
| Read the running totals | one row | [`readUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L208-L215) |
| Check whether each output file is already stored | at most two `lstat` calls | [`FileBlobs.Contains`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob_inventory.go#L146-L165) |
| Insert the record and evidence, update the totals | policy again, a few index lookups and inserts | [`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L267-L351) |
| Write two output files | two hashes, writes, `fsync`s and directory `fsync`s | [`FileBlobs.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob.go#L71-L112) |
| Add the new files' bytes to the totals and commit | one update and a commit | [`Postgres.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L191-L242) |

None of these steps depends on B, M or E, apart from the logarithmic cost of
an index lookup. Because uploads are serialized, throughput is at most one
divided by this latency.

**Running totals.** The table `receipt_storage_usage_v6` holds one row with four
numbers: output bytes, metadata bytes, result rows and evidence rows. Every
change to what they count updates them in the same transaction:
[`publishTx`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L267-L351)
adds the record and evidence it inserted, `Put` adds the bytes of output files
that were not stored before, [`Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L470-L509)
subtracts the rows it deletes, and [`Postgres.Collect`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L382-L451)
replaces the output total with the exact sum from its own walk. A rollback
undoes the update together with the change, so the database totals stay exact.

The output total can still drift, because files live outside the transaction.
If an upload writes its files and then its commit fails, the files stay as
orphans and the total does not include them. To keep that rare, `Put` now runs
`publishTx` before writing any file, so a quota refusal writes nothing; only a
database failure after the file writes, or a crash, leaves uncounted orphans.
Readers still never see a record without its files, because the record becomes
visible only at commit, after the files are synced
([D10](#d10-why-postgresql-for-metadata-and-files-for-output)). Totals can also
be wrong after a restore from backup, or after an older build that does not
know about them changed the store. Two mechanisms correct them:
[`reconcileUsage`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L226-L237)
recounts everything at every server start and every CLI command, and
`shared gc --apply` resets the output total. Older servers must not run beside
a newer one (the README's upgrade rule), so an older build can only be an older
maintenance command, and those only delete rows or files that the newer build
counted. Between corrections the totals then err high, which makes quotas
stricter, not looser. `shared stats` and
`shared limits` still walk the store, so what they print is always exact.

**Cost of the change.** Startup reconciliation does the O(B + M + E) work that
every upload used to do, once per process start and once per `receipt shared`
or `receipt auth` command, under the exclusive lock. Before 15 October 2026 the
same work ran on every upload. Existing stores need no migration step: the
first start of the new build creates the table and fills it by recounting.

**Measured.** On 15 October 2026, the same test ran before and after the change
on the same laptop (Apple M1, PostgreSQL 14.17 over loopback TCP): sequential
uploads of new keys with 1 KiB outputs, 15 samples per size.

| Results already stored (about) | Median before | Median after | Fastest after | Slowest after |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 33.4 ms | 19.1 ms | 15.7 ms | 43.3 ms |
| 265 | 37.4 ms | 18.8 ms | 16.6 ms | 44.6 ms |
| 1,030 | 54.8 ms | 24.2 ms | 18.2 ms | 30.0 ms |
| 4,045 | 131.8 ms | 19.9 ms | 17.5 ms | 40.2 ms |

The median no longer grows with the store. At an empty store the time goes to
fixed work: four file flushes and the SQL statements, counting the
transaction's start, lock and commit. This run did not split those apart. The
[appendix](#appendix-how-the-upload-cost-numbers-were-measured) has the code.
An earlier run of the old code on the same laptop, also on 15 October 2026,
gave medians of 23.0, 35.6, 51.4 and 111.5 ms for the same sizes. The two runs
of the old code differ by up to 45 percent at the empty store, so read the
table as the shape of the growth, not as precise figures.

These were small manifests. Real manifests are larger: this repository's is
29,612 bytes. With the default 64 MiB metadata budget, that size allows roughly
2,200 results before uploads return HTTP 507, well before the 10,000-row limit.
That estimate divides the budget by one measured manifest and ignores the
record's other fields.

A read is cheaper. [`Postgres.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L146-L189)
does one index lookup, decodes one record and reads and hashes two output files.
It does not scan the store. Single reads in the same run took 1 to 2 ms.

### D16. How does cleanup avoid deleting a file that is still needed?

**Decision.** Two separate commands.

- [`Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L470-L509)
  (`shared prune`) deletes non-quarantined rows older than a cutoff, 720 hours by
  default, and their evidence. It never touches files and always keeps
  quarantined keys.
- [`Postgres.Collect`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L382-L451)
  (`shared gc`) builds a map of every output hash still referenced, walks the
  output directory and deletes any file not in the map, plus leftover
  `.pending-*` files. It validates every record first. One malformed record
  stops the whole collection before anything is deleted.

Both run under the exclusive lock, so no upload can add a reference during the
walk, and no reader can be between finding a record and opening its file.

**Binding.** A random store ID is saved in the database and in `blobs/.store-id`.
[`FileBlobs.Bind`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob_binding.go#L11-L44)
refuses to work if they differ. Without it, pointing a second database at the
same directory would make every file look unreferenced to that database's
collector.

**Retention is first-in, first-out.** `created_at` is set once, by the first
upload, and is never refreshed by a hit. A result used every day still expires
30 days after it was first stored. The local cache, by contrast, evicts the
least recently used entry ([D8](#d8-how-does-the-local-cache-stay-bounded-and-why-do-quarantine-markers-win)).

**Cost.** Collection reads and validates every record, which recomputes every
manifest's key, and walks every file: O(M + B) time and O(live hashes) memory,
all under the exclusive lock. With `--apply` it also replaces the running output
total ([D15](#d15-what-does-one-upload-cost-and-how-does-that-grow)) with the
exact sum of the files that remain, and commits. Deletions are permanent. An
interrupted run can be repeated safely. It commits nothing, so the total keeps
counting files it already deleted until the next run or restart.

### D17. How does the server bound concurrent work?

**Decision.** Several independent limits:

| Limit | Value | Code |
| --- | --- | --- |
| Storage slots | `--max-inflight`, default 16, range 1 to 128. Production uses 2. | [`cacheAPI.serveAuthenticated`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L191-L218), [`--max-inflight 2`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service#L14) |
| Authentication slots | equal to `--max-inflight` | [`HandlerAuthorized`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L16-L22) |
| Storage deadline | 25 s per admitted request | [`context.WithTimeout`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L216-L219) |
| Authentication deadline | 2 s | [`context.WithTimeout`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L49-L59) |
| Database pool | 16 open, 4 idle, 5-minute lifetime, shared by `serve` and every CLI command | [`SetMaxOpenConns`](https://github.com/sidsharma96/receipt/blob/main/internal/app/shared_command.go#L26-L33) |
| HTTP server | header 5 s, read 30 s, write 30 s, idle 60 s, headers 16 KiB | [`http.Server`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L540-L540) |
| Request body | 32 MiB | [`cacheAPI.putEntry`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L346-L353) |
| Client timeout | 10 s per request, redirects refused | [`http.Client`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L54-L58) |
| Go memory target | `GOMEMLIMIT=320MiB` in production | [`GOMEMLIMIT=320MiB`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service#L13) |

A full slot pool returns 503 at once. Nothing queues inside Receipt.

**Interactions to know.**

- Each storage operation pins one pooled connection for its whole duration, so
  at most 16 storage operations run at once whatever `--max-inflight` says.
  Requests above 16 wait for a connection until their deadline.
- Authentication also needs a pooled connection. If storage operations hold all
  16, a new request's authentication can time out after 2 s and return 503.
- An upload body is read after the storage slot is taken. A client with a
  valid publish token that sends its body slowly holds a slot for up to the
  30-second read timeout.
- Bodies are held in memory: up to 32 MiB per admitted upload, plus decoding
  copies. `--max-inflight` bounds this, but it is not a memory limit.
- The 10 s client timeout covers the whole upload. A 32 MiB upload over a slow
  link can fail even though the server would accept it.

**Why reject instead of queue.** A queue hides overload as latency until every
client times out. An immediate 503 lets the client fall back to running its
tests within milliseconds. Receipt's clients treat every server error as "run
the tests", so overload costs speed, never correctness.

### D18. How are credentials generated, stored and checked?

**Generation.** [`generate`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L137-L144)
reads 48 bytes from `crypto/rand`. The first 16 bytes become a public ID, the
remaining 32 bytes become the secret. The token is
`rcpt_<32 hex ID>_<64 hex secret>`, exactly 102 characters.

**Storage.** The database row keeps the ID, `SHA-256(whole token)` in hex, the
subject label, scopes, creation and expiry times, and an optional revocation
time. [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L88)
computes the expiry with the database clock.

**Checking.** [`cacheAPI.authenticate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L42-L77)
rejects any header that is not one `Bearer` value of the right length before
taking an authentication slot. [`Postgres.Authenticate`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L90-L121)
then parses the ID, fetches the row by primary key, asks the database whether
it is unrevoked and unexpired using `clock_timestamp()`, and compares digests
with `subtle.ConstantTimeCompare`. There is no permissions cache, so a
revocation applies to the next request. A request that has already passed the
check finishes.

**Why SHA-256 and not bcrypt or Argon2.** Slow password hashes exist to make
guessing a human password expensive. A 256-bit random secret cannot be guessed,
so slowness adds no protection. It would add CPU cost to every request and give
an unauthenticated client a way to burn server CPU.

**Why a constant-time comparison.** An ordinary byte comparison stops at the
first difference, so its timing can reveal how many leading bytes matched. The
row lookup by ID and the network are not constant time, but the secret part is
only ever compared in constant time.

**Scopes.** [`requiredScope`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L85-L105)
maps each route to one scope:

| Scope | Allows |
| --- | --- |
| `read` | `GET /v1/entries/<key>`, `GET /v1/status/<key>` |
| `publish` | `PUT /v1/entries/<key>` |
| `report` | `POST /v1/conflicts/<key>` |
| `metrics` | `GET /v1/metrics` |

`GET /v1/identity` needs any valid credential. Unmapped routes are refused
before storage. Scopes do not imply each other. They limit operations, not
keys: every credential sees the whole store.

**The 30-day replay window.** [`ValidateIssue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L114-L127)
caps a lifetime at 720 hours. A copied token works from any machine for the rest
of its lifetime unless someone notices and revokes it. Nothing binds a token to
a client, such as a client certificate or a signed request. CI's token was
issued for the full 720 hours and expires on 12 November 2026 at 03:28 UTC.

**Capacity.** At most 1,024 rows, counting expired and revoked ones. There is no
purge command, so about 1,000 renewals fill the table.

### D19. How is a running test stopped?

**Decision.** [`Run`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L49-L86)
starts the command directly with no shell. [`configureProcess`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process_unix.go#L16-L46)
puts it in a new process group. On cancellation it sends `SIGTERM` to the whole
group, checks every 10 ms whether any member is left (signal 0 tests for
existence without delivering anything), and after `terminationGrace`, 5
seconds, sends `SIGKILL` to whatever remains. `WaitDelay` then gives the output
pipes 2 seconds to close. [`Main`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L97-L107)
cancels the context on `SIGINT` or `SIGTERM`. Before 15 October 2026 the group
got `SIGKILL` at once.

Because the child has its own process group, it is not in the terminal's
foreground group. Ctrl-C reaches only Receipt, which then kills the child's
group. Grandchildren that stayed in that group die too. A grandchild that
started its own group escapes.

Standard input is `/dev/null`, so a test that reads input gets end of file at
once ([F8](#f8-processes-process-groups-signals-and-standard-streams)). The code
comment and some older documents say "closed stdin", which is inaccurate.
Output goes through pipes, so tests do not see a terminal, and each captured
stream stops growing at 8 MiB.

**Exit codes.** A child killed by a signal it did not expect reports
`ExitCode() == -1`. `Run` then reads the wait status and reports 128 plus the
signal number, as a shell does: 139 for a `SIGSEGV` crash, 137 for `SIGKILL`
from the kernel's out-of-memory killer. If Receipt itself cancelled the command,
after Ctrl-C or `SIGTERM`, `Run` reports 130 however the command ended, even if
it caught `SIGTERM` and exited 0, and `run` returns 130 too
([`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L314-L320)).
Interrupted and failed runs are never stored. Before 15 October 2026 every
signal death was reported as 130.

**Cost of the grace period.** A test that ignores `SIGTERM` keeps running for up
to 5 seconds after Ctrl-C, so stopping Receipt can take up to 7 seconds,
counting `WaitDelay`. In exchange, a test that handles `SIGTERM` can remove
temporary files or stop servers it started.

### D20. How does the Cargo adapter decide what it can trust?

The adapter accepts one narrow grammar of `cargo test --frozen`
([`parseCargo`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo.go#L22-L55)),
refuses Cargo configuration files and compiler-override variables
([`cargoPolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_policy.go#L13-L52)),
asks Cargo for the dependency graph, and walks it in
[`cargoClosure`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L78-L126).
The walk is a depth-first search with a stack. It marks a package as seen when
it is popped, not when it is pushed, so the stack can hold one entry per edge:
O(E) memory, O(V + E) time, then an O(V log V) sort. Normal and build dependency
edges are always followed, and dev-dependency edges only from the root
([`cargoDependencies`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L128-L150)).
[`cargoPackagePolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L152-L177)
refuses build scripts, procedural macros, unknown target kinds, targets outside
their package, path dependencies outside the repository and any source that is
not a registry, which includes Git dependencies. The full contract, with its
limits, is in [CARGO.md](CARGO.md).

### D21. How is the server deployed, and how would we recover it?

**Topology.** One DigitalOcean droplet runs Caddy, Receipt and PostgreSQL 16.
Cloudflare DNS points `receipt.sidsharma.dev` at the droplet with proxying off,
so Cloudflare does not see requests.

- Caddy listens on TCP 443 and 80. It obtains and renews a Let's Encrypt
  certificate, terminates TLS and forwards to `127.0.0.1:7777`
  ([`reverse_proxy`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/Caddyfile#L1-L3)).
  Port 80 answers with a 308 redirect to HTTPS. The Caddyfile sets no request
  size limit, rate limit or timeout of its own, so Receipt's limits are the only
  ones.
- Receipt listens only on loopback port 7777 and runs as the `receipt` user
  with systemd hardening such as [`ProtectSystem=strict`, `NoNewPrivileges` and
  `UMask=0077`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service#L14-L27). The unit starts
  it with [`ExecStart`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service#L14).
- PostgreSQL listens locally. Receipt connects through a Unix socket with peer
  authentication, so there is no database password.
- The firewall allows SSH, 80 and 443. Caddy's admin port 2019 is loopback only.

**Why Caddy in front.** Certificate issuance and renewal stay out of Receipt.
Receipt's client still refuses plain HTTP to any host other than loopback ([`Remote.requestPath`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L37-L47)).
The cost is that Caddy and the host administrator can read every request,
including tokens.

**CI.** The cached job runs Receipt against the HTTPS endpoint after checking
`/v1/identity`, and an independent job always runs the full suite with
PostgreSQL and the race detector. See [RECEIPT-CI.md](RECEIPT-CI.md).

**Backups and the restore drill.** There is no scheduled backup. On 13 October
2026 a one-off drill stopped Receipt, dumped PostgreSQL and copied the cache
directory, restarted Receipt (a 1.306-second interval), encrypted the archive
with OpenSSL CMS (AES-256-GCM to an RSA recovery certificate), uploaded it to
R2 with a conditional write, downloaded that object, decrypted it on the Mac,
verified checksums, restored into a separate database and directory inside a
private mount namespace, and fetched all 10 results. The database and files are
copied together because a database restored without its output files points at
missing files. Details are in [DIGITALOCEAN-SETUP.md](DIGITALOCEAN-SETUP.md#13-october-one-off-r2-backup-and-restore).

**Why the drill needs a private mount namespace.** The restored files sit in a
scratch directory such as `/var/backups/receipt-restore.yCuUL5/extracted/var/lib/receipt`,
but the restored database says its blobs live at `/var/lib/receipt/cache/blobs`.
The storage policy row records that absolute path and a random store identity,
and every storage operation checks both: [`Postgres.policy`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L171-L193)
compares the server's blob directory with the recorded path, and `Bind` compares
the `.store-id` file with the recorded identity. A server started against the
scratch directory would refuse every request with "blob directory does not match
database storage policy". The binding exists so that one database's `shared gc`
can never treat another store's files as unreferenced and delete them.

The drill therefore needs the restored files to appear at the original path
without touching the live server's files at that path. A Linux mount namespace
gives a process its own table of mounts. The drill is run under
`unshare --mount --propagation private`, the command the R2 drill script names.
The script refuses to continue unless its mount namespace differs from PID 1's ([`readlink`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-check-20261012.sh#L5)),
and then bind-mounts the restored directory over `/var/lib/receipt`
([`mount --bind`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-check-20261012.sh#L7-L8)).
Only processes inside that namespace see the restored files. The live server
keeps its own view. The test server also uses a separate restored database
(`receipt_restore_20261012`, and `receipt_restore_r2_20261013` for the R2 drill)
and listens on another loopback port (17778, and 17779 for the R2 drill), so
production rows and the production port are never touched. The R2 drill script
makes the same namespace check ([`readlink`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-r2-drill.py#L43-L44)).
The dated account is in [DIGITALOCEAN-SETUP.md](DIGITALOCEAN-SETUP.md#12-october-amendment-encrypted-backup-and-isolated-restore).

The cost is that recovery onto a new machine must restore the files to the same
absolute path, or the database row must be changed by hand.

**What the drill does not prove.** Recovery onto a new machine, recovery without
the single copy of the private key on the Mac, or a backup taken while Receipt
is running. A restored backup also restores old credential state: a token
revoked after the backup becomes valid again until it is revoked a second time.

**Fly.io.** [FLY-DEPLOYMENT.md](FLY-DEPLOYMENT.md) is an undeployed proposal.

## Part 3. Threat and failure model

Receipt is a trusted-team cache. It authenticates callers and limits what each
credential may do. It does not verify that an uploaded result came from a real
test run.

| Actor or event | What it can do | What the design does | What remains |
| --- | --- | --- | --- |
| Network observer | Read or change traffic | Clients require HTTPS off loopback and refuse redirects. Caddy terminates TLS. | Trust in Caddy, Cloudflare DNS and the certificate authority. |
| Unauthenticated internet client | Send requests | Wrong-length headers are refused before any slot. Every protected route needs a valid token. | With `--max-inflight 2`, a stream of well-formed fake tokens can fill both authentication slots and make real callers get 503. Clients then run tests locally. No per-IP or Caddy rate limit exists. |
| Holder of a `publish` token | Upload any result whose key matches its own manifest | Uploads are validated for shape, size, checksum and key. A different observation for an existing key quarantines it. | Cache poisoning: a fake success for a key nobody has stored yet is accepted and replayed to everyone who computes that key. The independent uncached CI job is the only defence. |
| Holder of a `report` token | Quarantine any key whose manifest it can supply, even one never stored | Reports are validated and stored as evidence. | Denial of reuse. Quarantine is permanent and has no undo command. |
| Holder of a `read` token | Fetch any result whose key it knows | No listing endpoint exists. | Each fetched manifest exposes file paths and environment value hashes. Manifests made before 15 October 2026 also carry the owner's token hash (D4). |
| Thief of a token | Use it from anywhere | Expiry at most 30 days. Revocation applies to the next request. | Replay for the rest of the lifetime if nobody revokes it. |
| Malicious test code run with a token | Read `RECEIPT_TOKEN` and use it | The test's environment no longer contains it (D4). Pull requests never receive the CI token. | On Linux, a same-user process can still read the parent's environment from `/proc`. Code on a trusted branch is trusted. |
| Repository writer | Change the workflow to print the token | None in code | Repository writers are trusted. |
| Host administrator or Caddy | Read requests and tokens, change data | None | Trusted. Root SSH login with a key is still allowed, and `sid` has passwordless sudo. |
| Database writer | Change credentials, quarantine flags or records | `Postgres.Get` validates records and output hashes before replaying | Trusted. |
| Hostile JSON | Malformed, oversized or ambiguous bodies | Size limits before reading, `strictjson` rejects unknown, duplicate and wrongly null fields and non-UTF-8, and semantic validation runs before storage | Bodies are fully buffered in memory. |
| Hostile local program on the server | Ignore locks and change files | `Get` re-hashes output files before serving them | Locks are advisory. |
| Crash or power loss | Interrupt any step | Files are synced before they are named, new directories' parents and quarantine markers' directories are synced, and output files are durable before their record commits | Orphan files until `shared gc`. |
| Database connection loss | Release the advisory lock early | The `flock` keeps file work exclusive | The request fails and the client runs its tests. |
| Clock jump | Change expiry decisions | Credential expiry and `created_at` use the database clock. Durations use Go's monotonic clock. | `shared prune` computes its cutoff with the CLI host's clock, which matters if the CLI runs on another host. Local LRU uses file times. |
| Slow disk | Slow every `fsync` | Uploads are serialized, so one slow flush delays all writers. Requests fail at 25 s, and clients fall back. | No alerting exists. |
| Full disk | Fail writes | The blob budget refuses uploads with 507 before the disk fills, if the budget is below the free space. | The budget does not count PostgreSQL's own files or the write-ahead log. |

## Part 4. Known gaps and pitfalls

Each item is a fact about the current code, with a proposed fix. None of the
fixes were made in the documentation pass that wrote this list. Items fixed
later are removed or reworded to what remains, and their numbers are not
reused. [PHASE-REVIEWS.md](PHASE-REVIEWS.md#known-gap-fixes-15-october-2026)
lists each fix with its date and test.

### G1. `receipt auth` still takes the storage lock once

Issuance itself has its own advisory lock key since 15 October 2026
([D13](#d13-which-advisory-lock-keys-exist-and-what-does-each-guard)). But
[`openShared` and then `credentials.Migrate`](https://github.com/sidsharma96/receipt/blob/main/internal/app/auth_command.go#L74-L86)
in `authCommand` runs the storage migration first, and that takes the exclusive
lifecycle lock once. A `receipt auth` command therefore still waits for the
slowest in-flight storage operation before it starts. Fix: open only the
database connection in `authCommand`, without initialising storage.

### G4. The connection pool silently caps storage concurrency at 16

[`SetMaxOpenConns`](https://github.com/sidsharma96/receipt/blob/main/internal/app/shared_command.go#L26-L33)
allows 16 connections, but `--max-inflight` accepts up to 128. Fix: size the
pool from `--max-inflight` plus authentication, or reject larger values.

### G5. Production admission is two requests

[`--max-inflight 2`](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service#L14)
runs with `--max-inflight 2`, and authentication slots equal that number. A
third concurrent request gets 503. Fix: separate the authentication slot count
and add a per-IP rate limit in Caddy.

### G11. The stdin comment is wrong

The comment [`Stdin is closed`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L56-L61) in `Run`
says stdin is closed. It is `/dev/null`. Fix: correct the comment.

### G13. Go's temporary-path pattern can miss an adjacent flag

The pattern in [`regexp.Compile` in `stableGoEnvironment`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/go_environment.go#L14-L37)
consumes the delimiter after a match, so a second prefix-map flag separated by a
single space is not normalised. That causes extra misses, never wrong hits. Fix:
match each whitespace-separated flag on its own.

### G14. Retention ignores use

[`created_at` in `Postgres.Expire`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L470-L509)
deletes by `created_at`, which hits never refresh. Fix: record a last-used time,
updated outside the read lock or in batches.

### G15. Quarantine cannot be undone

There is no command to clear `quarantined` for a key, and
[`quarantined=TRUE`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L346-L346) in `publishTx`
lets a report create a quarantined row for a key that was never stored. Fix: an
administrator command that clears quarantine with a recorded reason.

### G16. Keys depend on Go's JSON encoder

See [D2](#d2-how-does-the-manifest-become-a-key-every-time-the-same-way). Fix, if
a non-Go client is ever needed: specify the canonical form, for example RFC 8785,
and bump the manifest version.

### G17. Manifests grow with the repository

A manifest costs about 180 bytes per input file. Large repositories approach the
32 MiB request limit together with output, and the 64 MiB metadata budget fills
after about 2,200 results for this repository
([D15](#d15-what-does-one-upload-cost-and-how-does-that-grow)). Fix: store
manifests compressed, or as a hash plus a separately stored body.

### G18. The client timeout is fixed at 10 seconds

[`http.Client`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L54-L57) in `Remote.requestPath`
uses a 10-second total timeout. Fix: make it configurable or scale it with the
body size.

### G19. Toolchain versions drift

`go.mod` says Go 1.22, CI uses 1.22.12, the Fly Dockerfile uses 1.27.2, and the
development Mac now defaults to 1.27.2. Fix: pin one version, or add a
`toolchain` line to `go.mod`.

### G20. Small hygiene items

- [`outDiscard`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L335-L346)
  duplicates `io.Discard`.
- [`commandPrune`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L28-L36)
  is an untyped constant inside the typed `commandName` block.
- The comment above the `extra_inputs` loop in
  [`range extra` loop in `discoverFiles`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L136-L153)
  describes `.gitignore` and matches nothing there.

### G21. Operations

No scheduled backup, one copy of the recovery key, no monitoring or alerting,
manual token renewal, root key login still allowed, passwordless sudo for `sid`,
the retired `receipt-ci` account still present, and no observed Caddy
certificate renewal. See [DIGITALOCEAN-SETUP.md](DIGITALOCEAN-SETUP.md).

## Part 5. Questions this document cannot answer yet

- How fast is fingerprinting per gigabyte of repository, and how much of it is
  hashing? Not measured.
- How long does credential issuance actually pause traffic on the production
  server under load? Reasoned in D13, not measured.
- Does Caddy renew the certificate and survive a reboot unattended? The first
  renewal has not happened yet.
- What does a full new-host recovery take? Not tested.

## Appendix. How the upload-cost numbers were measured

The numbers in D15 came from a temporary test file, never committed, run
against a disposable PostgreSQL 14.17 cluster on `127.0.0.1`. The first run
used a copy of the repository at `5f5794f`. The before-and-after pair used the
same file in this repository at `45090db` and then with the running totals
added:

```sh
RECEIPT_PUTCOST=1 RECEIPT_TEST_DATABASE_URL='postgres://receipt_test@127.0.0.1:54339/receipt_test?sslmode=disable' \
  go test -count=1 -run TestPutCostByStoreSize -v ./internal/cache
```

<details>
<summary>The temporary test (internal/cache/zz_putcost_test.go, not committed)</summary>

```go
package cache

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"receipt/internal/fingerprint"
)

func putCostResult(i int) Result {
	m := fingerprint.Manifest{Version: 1, Root: "/r", Directory: "/r", Platform: "x/y", Command: []string{"go", "test", fmt.Sprint(i)}, Inputs: []fingerprint.Input{}}
	out := make([]byte, 1024)
	copy(out, fmt.Sprintf("result %d", i))
	return Result{Version: 1, Key: m.Key(), Manifest: m, Stdout: out, Stderr: nil, ExitCode: 0, DurationNS: 1, CreatedAt: time.Now().UTC()}
}

func TestPutCostByStoreSize(t *testing.T) {
	if os.Getenv("RECEIPT_PUTCOST") == "" {
		t.Skip()
	}
	s := postgresFixture(t)
	ctx := context.Background()
	n := 0
	for _, target := range []int{0, 250, 1000, 4000} {
		for ; n < target; n++ {
			if err := s.Put(ctx, putCostResult(n)); err != nil {
				t.Fatal(err)
			}
		}
		var samples []time.Duration
		for j := 0; j < 15; j++ {
			r := putCostResult(1_000_000 + target*100 + j)
			start := time.Now()
			if err := s.Put(ctx, r); err != nil {
				t.Fatal(err)
			}
			samples = append(samples, time.Since(start))
		}
		n += 0
		sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
		get := putCostResult(0)
		gs := time.Now()
		if target > 0 {
			if _, err := s.Get(ctx, get.Key); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("entries~%d blobs~%d: Put median %v min %v max %v; one Get %v", target, target, samples[7], samples[0], samples[14], time.Since(gs))
	}
}
```

</details>

Each run takes about two to six minutes, most of it filling the store. The
store sizes in the D15 table include the 15 samples that each earlier
round left behind, so they are 0, 265, 1,030 and 4,045 rather than the round
targets 0, 250, 1,000 and 4,000. The single `Get` timing per round is the
source of the 1 to 2 ms read figure. One client uploaded sequentially, so the
numbers contain no lock waiting. Each column is one run on one laptop, not a
confidence interval.
