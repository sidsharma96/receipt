# 6. Find the code

Suppose you want to change what Receipt records before running `go test`.
Start in `internal/fingerprint`, which discovers inputs and builds their
description. You do not need to begin in the HTTP server or database code.

A path such as `internal/fingerprint` is relative to the Receipt source checkout,
not your test project's directory. It names a directory containing Go source.
A Go package groups related source files. Receipt's packages run inside the same
client or server process; a package is not a separately deployed service.

## The main responsibilities

| Package | Responsibility | Starting point |
| --- | --- | --- |
| `internal/app` | Interpret commands and coordinate lookup, execution and saving | [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L232-L335) |
| `internal/fingerprint` | Discover and describe inputs | [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L111-L154) |
| `internal/runner` | Start the command, stream output and handle cancellation | [`Run`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L50-L88) |
| `internal/cache` | Store, retrieve and check results | [`Store`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L41-L45) |
| `internal/strictjson` | Reject malformed JSON object shapes | [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61) |
| `internal/auth` | Issue and check credentials | [`Principal`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L51-L60) |

Follow a normal command through those boundaries:

```text
app reads arguments
  -> fingerprint describes inputs
  -> cache looks for a result

Hit: app replays output
Miss:
  runner starts the test
  -> fingerprint checks inputs again
  -> attempt local save
  -> attempt shared upload
  -> prune local cache
```

For the miss above, assume a successful test, unchanged recorded inputs, output
within the capture limit and no quarantine. Receipt first attempts to save the
result locally, then uploads it if a remote is configured, then prunes the local
cache. Pruning removes older entries to stay near the configured size limit.
An ordinary upload failure is reported, but a successful local save remains and
the tests keep their successful exit code. A reported conflict instead triggers
quarantine, which Chapter 10 follows in detail.

The order matters: an upload failure need not lose the local result, and pruning
may remove that result later even if the upload succeeded. These are separate
operations, not one change that either happens everywhere or nowhere. Follow
[`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L217-L246)
to see their coordination in `internal/app/run_session.go`.

This division lets a test substitute a storage implementation without starting a
server. It does not make a new backend automatic: an object store would still
need rules for failed uploads, concurrent reads and deletion.

## What needs to be installed?

The built executable needs Git and the tools for the workload. A pytest command
needs its Python environment; a Go command needs Go. Go commands under
`tools/` check documentation, build releases and support measurements. They are
development tools; a downloaded Receipt executable does not invoke them.

A shared server receives results rather than running users' tests. The
credential-enabled server needs PostgreSQL and a writable output directory.
A TLS proxy provides HTTPS for remote clients. It does not need pytest or a Go
compiler to serve the compiled application.

To change and rebuild Receipt, use the [contributor guide](../CONTRIBUTING.md).
To install a release, use the [README](https://github.com/sidsharma96/receipt#install).

## Follow a change through its consequences

If a new input is added, ask whether it changes the saved key, whether it can be
read reliably, and what happens when reading fails. If storage changes, trace a
successful save and then interrupt it between the file and database steps.

Explain the choice through that concrete sequence. Compare an alternative that
solves the same problem, including what it costs. The
[design reference](../DESIGN-DEFENSE.md) develops these comparisons; it is a lookup
resource rather than a required checklist for every small change.


## Check your understanding

<!-- questions:start -->

### Which package does what?

**Level:** Newcomer

You want to change how Receipt describes inputs. Which Go package do you open, and which packages hold the run flow, process execution and storage?

<details>
<summary>Answer</summary>

`internal/fingerprint` describes the inputs and calculates the key, starting at `Build` and `Manifest.Key`. `internal/app` reads the command line and combines lookup, execution, verification and publication, starting at `run`. `internal/runner` starts the test command and captures its output. `internal/cache` stores results locally, over HTTP and in shared storage. `internal/strictjson` checks JSON structure, and `internal/auth` defines credentials and permissions. All of these packages run inside one client or server process; a package is not a separate service.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L111-L154)

</details>

### The order of a run, from lookup to prune

**Level:** Practitioner

Walk through `receipt run -- uv run pytest` on a miss that succeeds, with a remote cache configured. In what order do lookup, execution, local storage, upload and pruning happen?

<details>
<summary>Answer</summary>

`run` parses flags and refuses more than one of `--bypass`, `--refresh` and `--verify`. It loads `.receipt.json`, resolves the cache directory and builds the manifest; if that fails, it runs the command uncached. `runSession.lookup` treats a locally quarantined key as a miss, then reads the local entry. With no local entry it asks the server with a GET, and a remote hit is stored locally and replayed.

On a miss, Receipt runs the command, streaming output while capturing a bounded
copy. It checks the inputs again and calls the run stable only if that succeeds
with the same key. For a stable success within the capture limit and without
quarantine, `runSession.publish` attempts local storage, then shared upload, then
local pruning. An ordinary network failure during upload is reported on stderr
without changing the tests' successful exit code. A conflict is different: it
triggers quarantine, as the walkthrough explains.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L232-L335) · [`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L217-L246)

</details>

### A new store behind the same interface

**Level:** Expert

A new storage implementation provides the same methods as the existing store. Does this alone establish that it handles interrupted uploads and cleanup correctly?

<details>
<summary>Answer</summary>

No. Matching method signatures lets the caller invoke the implementation. It does not establish what happens if saving fails halfway, or if cleanup overlaps a read. Trace a save, a failure and a retry through the new store before assuming it can replace the old one.

**Learn more:** [Chapter 6](../guide/06-find-the-code-and-defend-a-decision.md)

</details>

<!-- questions:end -->
