# Design reference

Suppose two clients save results for the same test command. Their recorded inputs
match, but their outputs differ. Receipt needs to preserve that disagreement,
not let whichever upload arrived last silently replace the other.

This reference explains the rules behind that behavior. Start with the
[walkthrough](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md) if you
want to follow one run before examining individual choices.

## Inputs and keys

Receipt records a description of the run, called a manifest. It includes command
arguments, absolute checkout and working-directory paths, platform, discovered
files, environment values and supported toolchain information. A hash is a
fixed-length value calculated from bytes. Receipt uses hashes to compare input
contents and calculates a key from the complete manifest.

[`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L66-L77)
sorts a copy of the input records by kind and name, then encodes the description
as JSON and hashes it. Sorting removes the effect of map iteration order;
command arguments keep their order because changing it can change the command.
The format depends on Go's JSON encoding. Changing that encoding needs a
compatibility plan, not just a new hash function.

A Git commit alone would omit uncommitted changes and environment differences.
Modification times are cheaper to inspect than file bytes, but restoring a file
with its old timestamp can hide changed contents. Even pairing time with size
misses a same-length replacement whose timestamp was preserved.
Receipt therefore reads content, at the cost of work on every ordinary invocation.
Absolute paths prevent reuse between different checkout locations. A test that
prints its working directory or reads a parent directory can behave differently
after a move. Portable keys would enable more sharing, but need a rule for those
path-dependent tests, such as controlling their filesystem view. Receipt does
not provide that control.

`RECEIPT_TOKEN` is excluded from inputs and removed from the child command's
environment. Rotating a cache credential should not invalidate a test result.
Other environment values are hashed by default. Hashes do not conceal easily
guessed secrets; only share manifests with trusted users.

Receipt scans before and after execution. Different descriptions prevent saving
the result, but a file changed and restored between the scans can escape detection.
Neither scan discovers a changing website, database, clock or random value.
A stable recorded key is not proof of stable test behavior. See
[the input contract](USAGE.md#the-input-contract).

## Execution and replay

Only successful commands can become reusable results. Receipt starts the command
as a child process, passes arguments directly and streams output while retaining
a bounded copy. A capture that exceeds its limit is not saved. Standard input is
closed to interactive input, so commands expecting a prompt see end of file.

On Unix, related processes are placed in a process group so cancellation can
signal the group. That does not sandbox the command or control a descendant
that deliberately escapes. See
[`configureProcess`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process_unix.go#L19-L23).

A hit prints saved stdout followed by saved stderr. It cannot reproduce their
original interleaving, terminal behavior or filesystem side effects. Receipt
keeps exact bytes instead of stripping timestamps because it cannot know which
output differences matter to a test.

`--bypass` runs without cache lookup or saving. `--refresh` executes and updates
the local observation. `--verify` reruns a hit and compares exit status and both
output streams; a mismatch stops local reuse of that key. Exact exit behavior is
in [run modes](USAGE.md#useful-commands). A failed upload does not turn a successful
test command into a failure.

## Local storage

A local entry is first written to a temporary file and synced. A hard link then
publishes it under the key only if no entry is already there. A hard link adds a
name for the same underlying file. This avoids exposing a partly written entry
under its final name and makes the first complete observation win.

Renaming over an existing entry would be simpler, but could silently replace a
result another process has already trusted. Publishing one file this way does
not make the complete run indivisible: history, quarantine and result files are
separate operations. Filesystem failures can leave work for a retry.

Hits update entry modification times. Pruning removes least recently touched
entries to approximate a least-recently-used cache. Concurrent commands can exceed
the configured byte target temporarily; history and quarantine records are outside
that entry budget. A quarantine marker prevents replay even if an entry remains.

## Shared storage and conflicts

PostgreSQL stores result descriptions and references to output files. The files
are named by their content hashes, so identical output can be shared between
results. Keeping large output in the database would simplify backup consistency,
but would also make those bytes part of database storage and write traffic.
Receipt uses both stores and accepts the resulting coordination work.

An upload writes or checks its output files before committing the database record.
If the database step fails, an output file may remain without a reference. Cleanup
can later remove it. A database rollback cannot undo a filesystem write.
See the [interrupted-write illustration](FIGURES.md#figure-2-interrupted-write).

Equal output and exit status are treated as the same observation even if timing
metadata differs. Contradictory output quarantines the key: the shared store
refuses further reuse and retains conflict evidence. A trusted client's explicit
conflict report can do the same. This is evidence of disagreement, not proof that
any client actually ran the tests honestly.

When a remote is configured, a local hit checks shared status before replaying.
Otherwise it could keep using a result the server has already quarantined.
Unavailable shared status causes execution rather than offline replay. A status
check cannot recall output already read, and turning off the remote restores
standalone local behavior.

## Locks, limits and cleanup

A reader must keep its output file available between finding the database row
and opening the file. Writers and cleanup therefore take exclusive access;
readers share access. PostgreSQL coordinates storage operations, and a filesystem
lock protects file work even if the database session is lost. All participants
must follow this rule. Direct file edits or incompatible servers can break it.

One writer at a time also makes storage-budget checks straightforward. Uploads
check running totals instead of recounting every file. They still read and write
the uploaded output and commit database changes. A constant-size accounting query
does not make the entire upload constant-cost.

Startup and applied collection recount usage. A failed database commit can leave
unreferenced output that is charged at the next recount. Cleanup first validates
records and constructs the set of referenced file hashes, then removes only known,
unreferenced files. It refuses malformed records, unknown paths and symlinks.
An interrupted deletion can be retried; already deleted files are not rolled back.

The database is bound to its output directory by matching store identities.
Restoring unrelated database and file backups, or deleting the identity markers,
is not a supported recovery method. See [operator recovery](DIGITALOCEAN-SETUP.md#backup-and-recovery).

Per-key locks could let independent uploads overlap. They would not by themselves
protect a shared space budget or cleanup. Consider finer coordination only when
measured waiting justifies a replacement protocol. The
[reader/cleanup illustration](FIGURES.md#figure-3-reader-cleanup) shows what must
remain protected.

## Requests and permissions

The server limits admitted work and gives operations deadlines. A busy request
can receive HTTP 503. Storage and authentication have separate admission limits
but share a database connection pool. Increasing the request limit beyond the
pool size does not create more database capacity, and neither limit bounds every
byte of process memory or every incoming connection.

A credential identifies an operator-assigned label and a set of permissions.
The database stores a digest of a randomly generated token, not the token itself.
Each request checks current expiry and revocation state. A request admitted before
revocation may still finish; revocation also cannot remove cached files a user
already possesses. The [credential reference](SERVICE-CREDENTIALS.md) describes
issuance, rotation, permissions and capacity limits.

Caching permission checks would reduce queries but allow revoked access to remain
valid for a period. Receipt chooses fresh lookups. All permitted callers still
share one cache; this is not isolation between untrusted customers.

## Limits that affect users

- Tests can depend on inputs Receipt does not discover. Use explicit files,
  stable environments or bypass for those workloads.
- Paths and environment differences limit sharing across machines.
- Byte-for-byte verification can reject changing timing text or output ordering.
- Quarantine has no automatic clearing policy or administrative undo command.
- Shared retention uses result age rather than frequency of access. Pruning
  server metadata does not delete client copies or establish test freshness.
- Client network waits have a fixed timeout. Very slow service or large responses
  can cause fallback execution.
- Authentication commands open shared storage and can wait for its lock during
  initialization, even though issuance uses its own lock afterward.
- Backup scheduling, credential renewal and monitoring require operator work.

## Evidence and further reading

The application has unit tests, race tests and optional PostgreSQL integration
checks. Those checks exercise selected behaviors; they do not establish production
capacity or every possible crash outcome. Use the current CI result for the
revision under review, not an old count of passing tests.

For exact accepted fields, read the [JSON contract](JSON-INPUT-CONTRACT.md).
For supported Cargo invocations and dependencies, read the [Rust reference](CARGO.md).
For measured costs, read [selected measurements](PERFORMANCE.md).
The [figure collection](FIGURES.md) shows storage states and coordination without
requiring a chronological implementation report.
