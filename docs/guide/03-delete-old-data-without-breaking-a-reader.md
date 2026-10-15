# 3. Delete old data without breaking a reader

Eventually the server fills with old results. Removing old database records and removing unused output files are separate jobs. Deleting a record does not immediately mean every file it referred to is unused: another result might refer to the same file.

Receipt's collection command first works out which files surviving records still need. It keeps their hashes in a map. When inspecting a file, it can ask the map whether that hash is needed. Searching a list could also work, but would repeatedly scan that list for each file.

## Why does the filesystem need a lock?

Consider a client reading a result. The server first finds its database record, then opens the output file. If cleanup deletes that file between those steps, the read fails.

A [lock](../GLOSSARY.md#lock) coordinates these operations. In Receipt's shared storage protocol, cleanup waits while a reader is using the protected storage. Multiple readers can overlap; cleanup and writes need exclusive access, which makes other participating operations wait.

**Figure 3. Cleanup waits for a reader**

| Step | Reader | Cleanup |
|---|---|---|
| 1 | Reader finds the result record. | Cleanup asks for exclusive access and waits. |
| 2 | Reader opens and checks the output file. | Cleanup still waits. |
| 3 | Reader finishes and releases its shared lock. | Cleanup can now inspect and remove unused files. |

The numbered rows show a possible execution, not elapsed time. The important relationship is that deletion starts after the protected read finishes.

The database lock coordinates participants using the same database. A filesystem lock also coordinates access to the output directory. Receipt binds that directory to its metadata store so a different store cannot treat another store's files as its own unused data.

These rules depend on every participant using the protocol. They do not defend against a hostile local program that ignores locks and changes files. They also do not establish that arbitrary network filesystems behave correctly.

## The simple choice has a cost

Before saving new shared data, Receipt inspects existing storage to check the selected limits. A tiny upload can therefore do more work as the store grows. Collection also scans records and files, and it can make reads and writes wait while it runs.

We chose this understandable approach for a small store. Keeping continuously updated usage counts could reduce scans, but every interrupted or retried update would need to keep those counts correct. That is a possible later design, not a free optimization.

The limits do not cap every byte PostgreSQL or the operating system might use. Database overhead and temporary files need operational attention too. Maintenance defaults to a [dry run](../GLOSSARY.md#dry-run) so an operator can inspect planned changes before applying them.

## Read further

For the exact limits and coordination rules, see [Phase 3's review](../PHASE-REVIEWS.md#phase-3-review-limits-and-cleanup-without-deleting-needed-files).

## Check your understanding

<!-- questions:start -->

### What is the shared-storage lock for?

**Level:** Newcomer

A client is reading a result while cleanup is deleting unused files. What could go wrong without a lock, and what does the lock make each side do?

<details>
<summary>Answer</summary>

The server reads a result in two steps: it finds the database record, then opens the output file. If cleanup deleted that file between the two steps, the read would fail even though the record was valid. Receipt's lock prevents this. Readers take it in shared mode, so many reads can overlap. Uploads and cleanup take it in exclusive mode, so cleanup waits until every reader has finished and no reader starts while cleanup runs. The lock has two parts, a PostgreSQL advisory lock and a file lock on the blob directory, and it protects only programs that use it.

**Learn more:** [Why does the filesystem need a lock?](../guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock) · [F5. File locks: flock versus fcntl](../DESIGN-DEFENSE.md#f5-file-locks-flock-versus-fcntl) · [D16. How does cleanup avoid deleting a file that is still needed?](../DESIGN-DEFENSE.md#d16-how-does-cleanup-avoid-deleting-a-file-that-is-still-needed)

</details>

### Two locks for one storage protocol

**Level:** Practitioner

Every shared storage operation enters through `Postgres.lifecycle`. Which locks does it take, which operations take them in shared or exclusive mode, and why use both a PostgreSQL lock and a file lock?

<details>
<summary>Answer</summary>

`lifecycle` takes a dedicated database connection and begins a READ COMMITTED transaction. In that transaction it calls `pg_advisory_xact_lock_shared` or `pg_advisory_xact_lock` on the key 72656365697075. It then takes `flock` with `LOCK_SH` or `LOCK_EX` on `.lifecycle.lock` in the blob directory. Reads (Get and Stats) take shared mode. Uploads, conflict reports, limit changes, `shared gc`, `shared prune` and policy initialisation take exclusive mode. Both locks are released when the transaction commits or rolls back.

The advisory lock coordinates every process that shares the database: server processes and maintenance commands. The file lock protects the files themselves. If the database connection dies, PostgreSQL releases the advisory lock, but the file lock stays held until the Go code closes it, so `shared gc` cannot delete files that an interrupted upload is still writing. The transaction also begins with `context.WithoutCancel`, so a cancelled request does not make `database/sql` roll back and release the lock while file work is still running.

**Learn more:** [Why does the filesystem need a lock?](../guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock) · [D12. Why does one lock serialize every shared writer?](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [F5. File locks: flock versus fcntl](../DESIGN-DEFENSE.md#f5-file-locks-flock-versus-fcntl) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L72-L119)

</details>

### Database connection lost mid-upload

**Level:** Expert

An upload is writing output files when its request is cancelled, or its database connection drops. What stops `shared gc` in another process from deleting those files underneath it?

<details>
<summary>Answer</summary>

Two separate mechanisms. The transaction is begun with `context.WithoutCancel`, so a cancelled request context does not make `database/sql` roll back and release the advisory lock while file work is still running. The transaction ends only when the deferred `Rollback` or `Commit` runs, after the file work returns. Individual queries still use the request context.

If the connection itself dies, PostgreSQL releases the advisory lock at once, and a collector could acquire it. But every lifecycle operation also holds a `flock` on `blobs/.lifecycle.lock`, and that lock belongs to the open file, not the database session. The uploading process holds it until it closes the file, so the collector waits for the files. The tests `TestLifecycleCancellationKeepsFilesystemExclusion` and `TestDatabaseConnectionLossKeepsFilesystemExclusion` cover both cases. The limit is that `flock` coordinates one host only.

**Learn more:** [D12. Why does one lock serialize every shared writer?](../DESIGN-DEFENSE.md#d12-why-does-one-lock-serialize-every-shared-writer) · [`Postgres.lifecycle`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/maintenance.go#L72-L119) · [`lockBlobDirectory`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/lifecycle_unix.go#L15-L63)

</details>

<!-- questions:end -->
