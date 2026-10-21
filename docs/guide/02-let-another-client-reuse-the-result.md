# 2. Let another client reuse the result

Alice has saved a successful run. Now CI, the automated service that runs tests after code changes, wants the same result. A file on Alice's laptop is not enough: the two clients need somewhere they can both ask.

Receipt can run a server. A client sends an HTTP request to it, a message that names an operation and may carry data. A lookup asks for a result; an upload supplies one. The server does not run pytest on the client's behalf. It stores and returns reported results.

## Why keep descriptions and output separately?

The server needs to find a result by its input key and remember whether it is still trusted. [PostgreSQL](../GLOSSARY.md#postgresql), a database that stores and queries records, handles that information. The potentially larger output is stored in files on the server.

Those output files are named by hashes of their content. Receipt calls them [blobs](../GLOSSARY.md#blob). Here, blob means a stored sequence of bytes. Two results with identical output can refer to the same file instead of keeping two copies.

**Figure 2. Where a shared result goes**

| Step | What happens |
|---|---|
| Client | The client sends a successful result to the Receipt server. |
| Output files | The server writes complete output files, named by their content hashes. |
| Database record | The server commits the result description and references to those files. |
| Another client | The server loads the record, checks the output files, and returns the result. |

The files are saved before the database record is committed. Otherwise another client could find a record whose output has not been written yet. But the file write can succeed and the database commit can fail. That leaves an unreferenced file, which later cleanup can remove. There is no single undo operation covering both storage systems.

## A local copy still needs a shared trust check

When sharing is enabled, Receipt does not blindly replay its local copy. It asks the server whether the key has been marked untrustworthy. If that check is unavailable, it executes the command instead.

Here is the relevant condition in internal/app/run_session.go, inside [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L69-L103):

```go
if s.remote.URL == "" {
	return prior, true, nil
}
```

`s` contains the settings and resources for this invocation. `prior` is the saved local result. The empty URL means no shared server is configured, so the local result can be used. The three returned values are the result, whether it is a hit, and an error. Below these lines, the shared-server case checks the key's status before allowing replay.

## What if the clients disagree?

Imagine Alice and CI report different output for the same key. Receipt must not quietly choose whichever answer arrived last. It marks that key as [quarantined](../GLOSSARY.md#quarantine), meaning it must not be reused, and can retain information about the conflicting observations.

This is not proof that either caller is honest. A permitted caller can report false results. Nor does a hash prove the tests ran: it checks content identity, not execution history.

Sharing also has a practical limit today. Absolute checkout paths and environment settings are part of the key. Alice and CI do not automatically get matching keys merely because they checked out the same commit. Portable keys need further design.

## Read further

For publication, conflict handling, and networking details, see [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### What are PostgreSQL and blobs for?

**Level:** Newcomer

The shared server uses PostgreSQL and a directory of blob files. What goes where?

<details>
<summary>Answer</summary>

PostgreSQL, a database, holds the small records: each key's result description, references to its output, whether the key is quarantined, conflict evidence and storage limits. The output bytes themselves go into files called blobs, named by the SHA-256 hash of their content and stored as `blobs/<first two hex characters>/<hash>`. Because the name comes from the content, two results with identical output share one file. The server writes the blob files first and the database record second, so another client does not see the new record before its output is written. Later file damage or loss can still prevent reading it.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`FileBlobs.path`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob.go#L34-L39)

</details>

### Blobs before the commit

**Level:** Practitioner

In what order does a shared upload write output files and the database record, and why that order? What does a shared read check before it returns a result?

<details>
<summary>Answer</summary>

`Postgres.Put` validates the result, takes the exclusive lifecycle lock, checks the blob quota and refuses a key that is already quarantined. `publishTx` then inserts the record that points at the blobs, inside the still-open transaction, and checks the metadata quotas. Next `Put` writes the stdout blob and the stderr blob, each with a temporary file, `fsync`, a hard link that refuses to overwrite and a directory `fsync`. Last, it commits.

The blobs are durable before the commit, and the record becomes visible only at the commit, so readers do not see the new record before its output is written. Later damage or file loss can still break a read. Committing the record before writing the blobs would let another client find a record whose output does not exist yet. Inserting the record before the blob writes, but committing after them, means a quota refusal writes no files. The cost is that a crash or a failed commit can still leave orphan blobs, which `shared gc` later removes. No single operation undoes both a file write and a database transaction.

`Postgres.Get` takes the shared lock, reads the record, digest and quarantine flag, and returns a conflict if the key is quarantined. It checks the record's version and key, fetches each blob while verifying its length and hash, and recomputes the output digest. A digest mismatch is reported as corruption; a missing blob is treated as a miss.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`Postgres.Put`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L192-L243) · [`Postgres.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/shared.go#L147-L190)

</details>

### The shared server is down

**Level:** Expert

A developer has a valid local entry for a key and a `remote` configured. The shared server is unreachable. Does Receipt replay the local entry?

<details>
<summary>Answer</summary>

No. With a remote configured, a local hit is replayed only after `GET /v1/status/<key>` returns 204. Any other outcome, including a timeout, a connection error or a 5xx, makes `lookup` print "shared status unavailable or quarantined; executing instead of local replay" and run the tests. A 409 also writes a local quarantine marker. The rule is fail-closed: another client may have quarantined the key since this copy was saved, and the client cannot know that while the server is down. A successful run then tries to upload; that failure is reported and the local entry stays. To work offline with local hits, run without `--remote` and without a configured `remote`.

**Common wrong answer.** "Yes, the local cache is the fast path and the server is only a fallback." Local reuse is subordinate to the shared quarantine state whenever sharing is configured.

**Learn more:** [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L69-L103)

</details>

<!-- questions:end -->
