# 3. Delete old data without breaking a reader

Eventually the server fills with old results. Removing old database records and removing unused output files are separate jobs. Deleting a record does not immediately mean every file it referred to is unused: another result might refer to the same file.

For example, results A and B both refer to output file X. Removing A must keep X
while B still needs it. Only after both references are gone can collection remove X.

Receipt's collection command first works out which files surviving records still need. It keeps their hashes in a map, a lookup table that answers whether a hash is present. When inspecting a file, it can ask the map whether that hash is needed. Searching a list could also work, but would repeatedly scan that list for each file.

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

The database lock coordinates participants using the same database. A filesystem lock also coordinates access to the output directory. If an upload loses its database connection, the database can release its lock
while the process still has file work to finish. The filesystem lock remains
held until that work releases it. Cleanup must obtain both locks, so it still waits.
Receipt binds that directory to its metadata store so a different store cannot treat another store's files as its own unused data.

These rules depend on every participant using the protocol. They do not defend against a hostile local program that ignores locks and changes files. They also do not establish that arbitrary network filesystems behave correctly.

## The simple choice has a cost

Collection scans every record and file, and it can make reads and writes wait while it runs.

Uploads check the limits against running totals: one database row updated together with each storage change. A transaction groups database changes so they are committed together or undone together. The cost is keeping those totals correct when a write is interrupted. A rollback undoes the total together with the change, but output files live outside the database, so a failed commit can leave files the total does not count. Receipt recounts shared usage when a server or administration command opens that shared store, and collection resets the file total, so a successful recount includes those leftover files again. See [locks and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup) for the rules and [selected measurements](../PERFORMANCE.md#shared-upload-accounting) for the comparison.

The limits do not cap every byte PostgreSQL or the operating system might use. Database overhead and temporary files need operational attention too. Maintenance defaults to a [dry run](../GLOSSARY.md#dry-run) so an operator can inspect planned changes before applying them.

## Read further

For the exact limits and coordination rules, see [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### What is the shared-storage lock for?

**Level:** Newcomer

A client is reading a result while cleanup is deleting unused files. What could go wrong without a lock, and what does the lock make each side do?

<details>
<summary>Answer</summary>

The server reads a result in two steps: it finds the database record, then opens the output file. If cleanup deleted that file between the two steps, the read would fail even though the record was valid. Receipt's lock prevents this. Readers take it in shared mode, so many reads can overlap. Uploads and cleanup take it in exclusive mode, so cleanup waits until every reader has finished and no reader starts while cleanup runs. The lock has two parts, a PostgreSQL advisory lock and a file lock on the blob directory, and it protects only programs that use it.

**Learn more:** [Why does the filesystem need a lock?](../guide/03-delete-old-data-without-breaking-a-reader.md#why-does-the-filesystem-need-a-lock) · [Locks limits and cleanup](../DESIGN-DEFENSE.md#locks-limits-and-cleanup)

</details>

### Two results share an output file

**Level:** Practitioner

Results A and B both refer to file X. Cleanup removes A but keeps B. Should it remove X? Explain the lookup it needs.

<details>
<summary>Answer</summary>

No. Cleanup first records files needed by surviving results. B still needs X, so X’s hash remains in that lookup table and collection keeps the file. Deleting one result is not the same as deleting everything it referred to.

**Learn more:** [Chapter 3](../guide/03-delete-old-data-without-breaking-a-reader.md)

</details>

### A lock released before file work ends

**Level:** Expert

An upload loses its database connection while still writing an output file. The database releases its lock. Why must cleanup still wait?

<details>
<summary>Answer</summary>

The upload still holds the filesystem lock. Cleanup needs that lock too, so obtaining the database lock alone does not let it delete files. Once the upload finishes its protected file work and releases the filesystem lock, cleanup can proceed. This protects cooperating operations, not programs that ignore the locks.

**Learn more:** [Chapter 3](../guide/03-delete-old-data-without-breaking-a-reader.md)

</details>

<!-- questions:end -->
