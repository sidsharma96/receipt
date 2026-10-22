# 2. Let another client reuse the result

A successful result is saved on your laptop. Can a CI job reuse it? CI is the automated service that runs tests after code changes. Its test job runs on another machine, which cannot read your laptop's local cache. Both machines need access to a shared store; reuse also requires matching input keys.

Receipt can run a server. A client sends an HTTP request to it, a message that names an operation and may carry data. A lookup asks for a result; an upload supplies one. The server does not run pytest on the client's behalf. It stores and returns reported results.

## Why keep descriptions and output separately?

The server needs to find a result by its input key and remember whether it is still trusted. [PostgreSQL](../GLOSSARY.md#postgresql), a database that stores and queries records, handles that information. The potentially larger output is stored in files on the server.

Those output files are named by hashes of their content. Receipt calls them [blobs](../GLOSSARY.md#blob). Here, blob means a stored sequence of bytes. Two results with identical output can refer to the same file instead of keeping two copies.

**Figure 2. Where a shared result goes**

| Step | What happens |
|---|---|
| Client | The client sends a successful result to the Receipt server. |
| Pending database record | The server prepares a record in a transaction; other clients cannot see it yet. |
| Output files | The server writes complete output files, named by their content hashes. |
| Database record | The server commits the result description and references to those files. |
| Another client | The server loads the record, checks the output files, and returns the result. |

The database groups its changes in a transaction. Committing makes those changes
visible together; until then, the new record is pending. Preparing the record
first lets the server reject it for exceeding a storage limit before writing
output files. It saves those files before committing, so another client cannot
see the new record before its output is ready.

Suppose the server writes both output files, then the transaction rolls back
without committing. The result is absent from the database, but the files remain.
They are unreferenced files that later cleanup can remove. A retry can reuse the
complete files and try to publish the record again. There is no single undo
operation covering both storage systems.

## A local copy still needs a shared trust check

When sharing is enabled, Receipt does not blindly replay its local copy. It asks the server whether the key has been marked untrustworthy. If that check is unavailable, it executes the command instead.

Here is the relevant condition in internal/app/run_session.go, inside [`runSession.lookup`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L69-L103):

```go
if s.remote.URL == "" {
	return prior, true, nil
}
```

`s` contains the settings and resources for this invocation. `prior` is the saved local result. The empty URL means no shared server is configured, so the local result can be used. The three returned values are the result, whether it is a hit, and an error. Below these lines, the shared-server case checks the key's status before allowing replay.

`==` compares the URL with the empty string `""`. `return` ends this function;
`true` says a hit was found, and `nil` in the error position means no error.

## What if the clients disagree?

Suppose your laptop and the CI job report different output for the same key. Receipt must not quietly choose whichever answer arrived last. It marks that key as [quarantined](../GLOSSARY.md#quarantine), meaning it must not be reused, and can retain information about the conflicting observations.

This is not proof that either caller is honest. A permitted caller can report false results. Nor does a hash prove the tests ran: it checks content identity, not execution history.

Sharing also has a practical limit today. Absolute checkout paths and environment settings are part of the key. Your laptop and the CI machine do not automatically get matching keys merely because they checked out the same commit. Portable keys need further design.

## Read further

For publication, conflict handling, and networking details, see [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### What are PostgreSQL and blobs for?

**Level:** Newcomer

The shared server uses PostgreSQL and a directory of blob files. What goes where?

<details>
<summary>Answer</summary>

PostgreSQL holds each result's description, references to its output and quarantine state. The output bytes go into files called blobs, named by hashes of their contents. Two results with identical output can share one file. The server prepares the record inside a database transaction, writes the files, then commits the record. Another client cannot see the new record before commit. Later file damage or loss can still prevent reading it.

**Learn more:** [Why keep descriptions and output separately?](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately) · [Shared storage and conflicts](../DESIGN-DEFENSE.md#shared-storage-and-conflicts) · [`FileBlobs.path`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/blob.go#L34-L39)

</details>

### A transaction stops before commit

**Level:** Practitioner

The shared server prepares a new result record, writes both output files, then
rolls back the database transaction without committing. Can another client see
that result? What remains, and what can a retry do?

<details>
<summary>Answer</summary>

The new record was never committed, so another client cannot see it. The complete
files remain because rolling back database changes does not undo file writes.
A retry can reuse those files and try to commit a new record. If they remain
unreferenced, cleanup can remove them. Preparing a record is different from
publishing it: commit is the point at which readers can see it.

**Learn more:** [Where a shared result goes](../guide/02-let-another-client-reuse-the-result.md#why-keep-descriptions-and-output-separately)

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
