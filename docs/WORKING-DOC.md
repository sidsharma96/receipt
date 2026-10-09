# Receipt: follow one test run

Updated 9 October 2026. This guide explains the code that exists now. Phases 1–4 are committed; Phase 5 is implemented locally but not yet committed. No public deployment is implied.

## Start here

You have a Python project and run its tests with `uv run pytest`. Pytest checks your code. Uv starts it in the project's Python environment. Receipt sits around that command: it can remember a successful run and show its saved output when the recorded inputs match next time.

We will follow that run through six changes to Receipt: saving it locally, sharing it, cleaning up storage, checking incoming data, limiting what callers may do, and measuring where time goes. The examples use Alice and a continuous integration worker, called CI. CI is a program that runs tests automatically.

You do not need to understand database locks or Go channels before starting. Each chapter introduces a problem before the code that solves it. Diagrams show sequences, not measured timings. Examples of output and filenames are illustrative.

The main distinction to keep in mind is that showing a saved result does not run the tests again. Receipt cannot recreate a report file or repeat a network request just by printing old output.

### Choose a reading path

Read the chapters in order to understand the program. Each ends with a question you can answer before opening its explanation. To inspect setup commands, use the [README](../README.md). To examine the full nine-question phase reviews, test records, and dated decisions, use the [review reference](PHASE-REVIEWS.html) or its [phone-readable version](PHASE-REVIEWS.md).

The reference preserves earlier claims and their amendments. This guide describes current behavior directly so you do not have to reconstruct it from history.

## 1. Remember a successful run

Alice runs her tests. They pass. Ten minutes later she runs the same command without changing her code. Could Receipt avoid doing the same work twice?

It first needs a description of the run. The command alone is not enough: `pytest` can behave differently after a source file, environment setting, or Python package changes.

Receipt records the command arguments, discovered files, environment-value hashes, and supported runtime information. It also records the working directory and platform. A hash is a short, fixed-length value calculated from some bytes. It helps compare content without keeping another full copy in the description. Hashing is not encryption.

Receipt calls the whole description a manifest. It calculates another hash from that manifest and uses it as the input key, the name under which it looks for a saved run.

```diagram
Figure 1. A local run, in order
Describe the inputs | Receipt reads the files and settings it knows to record.
Calculate the key | Receipt gives this description a content-derived name.
Look for a saved result | If a usable result exists, Receipt prints it and does not start pytest.
Run pytest on a miss | If no usable result exists, Receipt starts the test command and shows its output.
Check the inputs again | Receipt saves a successful run only if its other storage conditions also hold.
```

Finding a usable saved result is a cache hit. Having to run the command is a cache miss. A cache is storage for work you hope to reuse.

### Why use a struct and slices?

The manifest groups different pieces of information in named fields. This is the actual type from [internal/fingerprint/manifest.go](../internal/fingerprint/manifest.go):

```go
type Manifest struct {
	Version   int      `json:"version"`
	Root      string   `json:"root"`
	Directory string   `json:"directory"`
	Platform  string   `json:"platform"`
	Command   []string `json:"command"`
	Inputs    []Input  `json:"inputs"`
}
```

In Go, `struct` groups named fields. `string` holds text and `int` holds a whole number. `[]string` means a slice of strings, a sequence whose length can vary. `Command` needs a sequence because arguments have an order. `Inputs` needs one because projects contain different numbers of files and settings.

The text in backticks tells Go which field names to write in JSON. JSON is a text format that can represent objects, lists, strings, and numbers. `Version` lets Receipt distinguish this format from a later one.

A fixed-size array would require an input count in advance. A map, a lookup table, would make sense for finding one field by name, but these fields already have distinct names and types. Receipt does use a map during file discovery to avoid recording the same path twice.

### What happens when pytest runs?

Receipt starts pytest as another process. A process is a running program with its own execution and memory. Receipt passes arguments directly rather than asking a shell to interpret a command string.

It shows output while capturing a copy. The capture has a size limit: a test can keep printing to the user, but an oversized capture will not be saved. Failed runs are not saved for replay either.

After execution, Receipt describes the inputs again. If that description changed, it does not publish the result. For example, uv might have installed packages during the run. The second scan helps notice that change.

### Matching inputs do not prove matching behavior

Suppose a test asks a public website for today's price. The recorded source files can stay unchanged while the website's answer changes. Receipt does not automatically discover that dependency.

The same issue applies to time, randomness, and some files outside the discovered set. Declare extra files where appropriate, or bypass the cache for a command whose dependencies you cannot safely describe. The two scans also cannot detect a file that changes and changes back during the run.

With `--verify`, Receipt runs a saved command again and compares the exit code and both output streams byte for byte. A difference makes it stop trusting that key. Even timing text can cause a difference; a mismatch is not automatically a failed test.

### Try explaining it

Why might editing an unrelated file cause a cache miss when you run just one test?

### Answer: Receipt records the repository

Receipt currently includes the discovered repository files, not only the files that one test actually reads. The broader description can cause extra misses. Narrowing it would require a reliable way to discover that test's dependencies.

For the exact local storage rules and alternatives, see [Phase 1's review](PHASE-REVIEWS.html#phase-1-review-remembering-a-local-run).

## 2. Let another client reuse the result

Alice has saved a successful run. Now CI wants the same result. A file on Alice's laptop is not enough: the two clients need somewhere they can both ask.

Receipt can run a server. A client sends an HTTP request to it, a message that names an operation and may carry data. A lookup asks for a result; an upload supplies one. The server does not run pytest on the client's behalf. It stores and returns reported results.

### Why keep descriptions and output separately?

The server needs to find a result by its input key and remember whether it is still trusted. PostgreSQL, a database that stores and queries records, handles that information. The potentially larger output is stored in files on the server.

Those output files are named by hashes of their content. Receipt calls them blobs. Here, blob means a stored sequence of bytes. Two results with identical output can refer to the same file instead of keeping two copies.

```diagram
Figure 2. Where a shared result goes
Client | The client sends a successful result to the Receipt server.
Output files | The server writes complete output files, named by their content hashes.
Database record | The server saves the result description and references to those files.
Another client | The server loads the record, checks the output files, and returns the result.
```

The files come before the database record. Otherwise another client could find a record whose output has not been written yet. But the file write can succeed and the database save can fail. That leaves an unreferenced file, which later cleanup can remove. There is no single undo operation covering both storage systems.

### A local copy still needs a shared trust check

When sharing is enabled, Receipt does not blindly replay its local copy. It asks the server whether the key has been marked untrustworthy. If that check is unavailable, it executes the command instead.

Here is the relevant condition in [internal/app/run_session.go](../internal/app/run_session.go):

```go
if s.remote.URL == "" {
	return prior, true, nil
}
```

`s` contains the settings and resources for this invocation. `prior` is the saved local result. The empty URL means no shared server is configured, so the local result can be used. The three returned values are the result, whether it is a hit, and an error. Below these lines, the shared-server case checks the key's status before allowing replay.

### What if the clients disagree?

Imagine Alice and CI report different output for the same key. Receipt must not quietly choose whichever answer arrived last. It marks that key as quarantined, meaning it must not be reused, and can retain information about the conflicting observations.

This is not proof that either caller is honest. A permitted caller can report false results. Nor does a hash prove the tests ran: it checks content identity, not execution history.

Sharing also has a practical limit today. Absolute checkout paths and environment settings are part of the key. Alice and CI do not automatically get matching keys merely because they checked out the same commit. Portable keys need further design.

### Try explaining it

A client has a saved local result, but cannot contact its configured shared server. Why run pytest instead of printing the local result?

### Answer: the local copy may be out of date about trust

The server might have learned about a conflicting observation since that copy was saved. Without the status check, the client cannot tell. Executing loses the reuse benefit but avoids treating an unchecked shared result as trusted.

For publication, conflict handling, and networking details, see [Phase 2's review](PHASE-REVIEWS.html#phase-2-review-sharing-results-between-clients).

## 3. Delete old data without breaking a reader

Eventually the server fills with old results. Removing old database records and removing unused output files are separate jobs. Deleting a record does not immediately mean every file it referred to is unused: another result might refer to the same file.

Receipt's collection command first works out which files surviving records still need. It keeps their hashes in a map. When inspecting a file, it can ask the map whether that hash is needed. Searching a list could also work, but would repeatedly scan that list for each file.

### Why does the filesystem need a lock?

Consider a client reading a result. The server first finds its database record, then opens the output file. If cleanup deletes that file between those steps, the read fails.

A lock coordinates these operations. In Receipt's shared storage protocol, cleanup waits while a reader is using the protected storage. Multiple readers can overlap; cleanup and writes need exclusive access, which makes other participating operations wait.

```timeline
Figure 3. Cleanup waits for a reader
1 | Reader finds the result record. | Cleanup asks for exclusive access and waits.
2 | Reader opens and checks the output file. | Cleanup still waits.
3 | Reader finishes and releases its shared lock. | Cleanup can now inspect and remove unused files.
```

The numbered rows show a possible execution, not elapsed time. The important relationship is that deletion starts after the protected read finishes.

The database lock coordinates participants using the same database. A filesystem lock also coordinates access to the output directory. Receipt binds that directory to its metadata store so a different store cannot treat another store's files as its own unused data.

These rules depend on every participant using the protocol. They do not defend against a hostile local program that ignores locks and changes files. They also do not establish that arbitrary network filesystems behave correctly.

### The simple choice has a cost

Before saving new shared data, Receipt inspects existing storage to check the selected limits. A tiny upload can therefore do more work as the store grows. Collection also scans records and files, and it can make reads and writes wait while it runs.

We chose this understandable approach for a small store. Keeping continuously updated usage counts could reduce scans, but every interrupted or retried update would need to keep those counts correct. That is a possible later design, not a free optimization.

The limits do not cap every byte PostgreSQL or the operating system might use. Database overhead and temporary files need operational attention too. Maintenance defaults to a dry run so an operator can inspect planned changes before applying them.

### Try explaining it

Why is locking each input key separately insufficient for all cleanup and storage-limit checks?

### Answer: different keys can affect the same storage

Two keys can refer to one output file. Both can also consume the same remaining storage budget. A per-key lock alone does not coordinate those shared decisions.

For the exact limits and coordination rules, see [Phase 3's review](PHASE-REVIEWS.html#phase-3-review-limits-and-cleanup-without-deleting-needed-files).

## 4. Check data before accepting it

A client sends a result as JSON. What if it includes an unknown field, repeats a field, or claims that a failed run is reusable? Receipt needs to reject that data before treating it as a valid result.

There are two different questions. Does the document have the expected structure? And do its values make sense together?

For example, a result must name its key. That is a structural requirement. The key must also match its manifest, and reusable results must describe success. Those are rules about meaning.

```diagram
Figure 4. From incoming bytes to a stored result
Bound the input | Receipt limits how much data it reads.
Check the JSON structure | It rejects unexpected, repeated, or missing required fields.
Check the values | It checks rules such as success and agreement between the key and manifest.
Accept the result | Only a valid result can proceed to storage. Stored envelopes also carry an integrity checksum.
```

### Why check inside the type?

The same result type is read from HTTP requests and local files. Putting its decoding checks in `UnmarshalJSON` makes them run when Go's JSON decoder fills that type, rather than relying on each caller to remember them.

This is the last part of that method in [internal/cache/json.go](../internal/cache/json.go):

```go
if err := Validate(Result(next)); err != nil {
	return err
}
*r = Result(next)
return nil
```

Earlier lines decode into a temporary value called `next`. `Validate` checks its meaning. If validation returns an error, the method returns without replacing the destination. `*r` refers to the result the caller asked the decoder to fill. Assignment happens only after the checks succeed. `nil` means there is no error.

Code that builds a result directly does not use JSON decoding, so storage methods also validate those values. A decoding hook is useful, but it is not permission checking and does not prove the sender ran the tests.

### What does a checksum tell us?

A stored envelope includes a hash of its payload. Reading it checks whether the payload still matches that hash. This can expose damage or accidental changes. Someone able to replace both the payload and checksum can still construct a matching pair.

### Try explaining it

If a JSON document has every required field, is that enough to accept it?

### Answer: the values can still contradict the rules

A complete result can claim a nonzero exit code, contain an invalid hash, or carry a key that does not match its manifest. Checking the shape does not replace checking these values.

The exact fields, size limits, and compatibility rules belong in the [JSON input contract](JSON-INPUT-CONTRACT.md). Interrupted-write evidence is in [Phase 4's review](PHASE-REVIEWS.html#phase-4-review-checking-incoming-data-and-interrupted-writes).

## 5. Give callers different permissions

Alice should be able to download results. CI should also be able to upload them. Giving both the same secret gives both the same broad access. Receipt's optional credential mode lets an operator issue separate secrets with explicit permissions and expiry times.

A credential is the secret a client presents. Anyone who obtains that secret can use it, so it must be protected. The operator can give a credential a label such as `alice`, but that label is not a verified human account.

### Two questions before storage

Authentication asks whether the credential is valid. Authorization asks whether it allows the requested operation. A valid credential can still lack permission to upload.

```diagram
Figure 5. Alice tries to upload with a read-only credential
Check the credential | Receipt checks whether the secret is valid, unexpired, and not revoked. Failure stops the request.
Check upload permission | Alice's credential allows reading, not publishing. Receipt refuses this upload here.
Storage stays untouched | The denied request does not proceed to decoding and storing a result. A permitted request would continue.
```

The refusal happens in [internal/cache/authorization.go](../internal/cache/authorization.go):

```go
if known && !principal.Allows(scope) {
	a.metrics.forbidden.Add(1)
	http.Error(w, "forbidden", http.StatusForbidden)
	return
}
```

`principal` holds the checked credential's public details and permissions. `scope` is the permission this operation requires. `Allows` checks for that permission. When the check fails, Receipt counts the refusal, sends an HTTP response, and returns before the storage handler runs. `known` means the operation has a permission mapping; other code rejects unmapped operations.

### Why does Receipt ask the database each time?

Suppose Alice's secret is stolen. The operator revokes it, meaning later credential checks should refuse it. Receipt consults the current database record for each request instead of trusting a permission copy remembered earlier.

The database stores a hash of the token, not the usable secret. The token is generated from randomness intended for security, not chosen as a human password. Exact byte counts and comparison mechanics are implementation details in the review reference.

Checking each request costs a database query. A remembered copy could reduce queries, but might keep accepting a revoked credential until it refreshes. We currently favor the direct check. This is a choice about revocation behavior, not a claim about production throughput.

### Revocation does not rewind a request

If an upload passes its credential check and then pauses, revoking the credential does not cancel that upload. It may still finish. A later request is checked again and will be refused once it sees the committed revocation.

When the database cannot perform a check, the server refuses access rather than substituting the old shared secret. The client may then run tests directly; server permissions do not control what the user's local programs can execute.

### What this phase does not provide

Callers still share one store. Read permission does not mean access to only Alice's results. Browser login, private organizations, and a durable record of who uploaded each result are not implemented.

Remote credentials need encrypted transport, normally HTTPS, so someone observing the connection cannot read the secret in transit. Receipt does not provision that deployment. The secret is also currently passed to the child test process through the environment. Run trusted code, and do not automatically exclude that setting from input recording if it can affect behavior.

### Try explaining it

Alice can download a result but cannot upload one. Did authentication fail?

### Answer: her permission check failed

Her credential is valid. It does not allow this operation. An invalid credential receives HTTP 401; a valid credential without the needed permission receives HTTP 403. HTTP is the request-and-response protocol, and these numbers identify response categories.

Use the [credential setup guide](SERVICE-CREDENTIALS.md) for commands. Use [Phase 5's review](PHASE-REVIEWS.html#phase-5-review-separate-caller-credentials-and-operation-permissions) for exact lifetimes, row limits, concurrency mechanisms, and verification records.

## 6. Find the code and defend a decision

The chapters explain behavior. To follow a change in the source, start with the part responsible for it. A Go package groups related source files. These packages run in one client or server process; a package is not a separate service.

### The main responsibilities

- [app](../internal/app/run_session.go) reads the command and combines lookup, execution, verification, and publication.
- [fingerprint](../internal/fingerprint/manifest.go) describes inputs and calculates their key.
- [runner](../internal/runner/process.go) starts the command and captures output.
- [cache](../internal/cache/store.go) stores results. Its other files implement HTTP, shared storage, and maintenance.
- [strictjson](../internal/strictjson/object.go) checks the declared JSON structure. Each decoded type owns its value rules.
- [auth](../internal/auth/auth.go) defines service credentials and permissions. Its PostgreSQL implementation manages their lifetime.

These boundaries keep responsibilities identifiable. They do not make future work automatic. A cloud output store must still solve publication, deletion, and failure handling. Another project may need different permissions and account rules. Receipt's current internal packages are not a ready-made shared identity platform.

### Frequently performed work versus occasional work

People call frequently performed work the hot path. In Receipt, input scanning and result lookup happen on normal commands. In credential mode, each server request also checks its credential. A cache hit avoids pytest, not all computation or disk access.

Occasional work is called the cold path. Issuing credentials and collecting unused files fit here. Occasional does not mean cheap: cleanup can inspect the whole store and make other operations wait.

We have tested selected behaviors and failures, but have not established production latency or capacity. The [verification records](PHASE-REVIEWS.html#verification-implementation-evidence-versus-documentation-checks) and each phase's evidence section say what actually ran. Their test names and execution counts are available for inspection rather than part of the introductory lesson.

### How to explain a choice to someone else

Start with the problem. Then describe the chosen behavior and one reasonable alternative. Finish with its cost, what we checked, and what would make us change our mind.

For cleanup, that becomes: a reader needs its output file to remain available; locks make cleanup wait; delaying deletion alone cannot establish that a paused reader has finished; the chosen lock can make other work wait; selected concurrency tests check this sequence. If measured waiting becomes unacceptable, we need a different protocol, not just a smaller timeout.

For permissions, it becomes: a stolen secret must stop working on later checks; fresh database lookups support that behavior; cached permissions can remain stale; queries cost work and depend on the database; selected tests cover denial and revocation. If queries become a measured bottleneck, any cache must state how long revoked access can remain accepted.

The full [nine-question reviews](PHASE-REVIEWS.md) cover structures, architecture, coordination, pitfalls, costs, guarantees, alternatives, evidence, and reasons to revisit choices. Read them when you want to inspect a particular decision in depth.

## 7. Find where the time went

Alice uploads a small test result. It takes longer than she expects. Reading a
small file should be quick, but perhaps the request has not reached the file yet:
another writer or cleanup may hold the lock it needs.

Run the server with --request-log to give each request a server-generated ID and
one timing summary. The ID appears in its response header too, so Alice can find
the matching summary without printing her token or test output into the log.

```diagram
Figure 6. Reading the request in parts, not guessing from its payload
Credential check | Receipt checks whether this request may proceed.
Connection and locks | The request can wait here before opening any output file.
Metadata and files | Receipt reads and checks the stored result.
Response, then summary | The response reports its request ID; the handler's summary records measured stages.
```

### Read one measurement carefully

In our local experiment, four clients repeatedly uploaded already-stored results
into a store with 128 descriptions. The mean database-lock call took about
55 milliseconds. That call includes a database round trip and time waiting for
other work to release the lock. It is inside the full request duration, not extra
time to add on top. [The measured bars](PHASE-REVIEWS.html#figure-6-measured-wait)
show both on the same scale.

Some timers contain others: counting storage includes file enumeration, for
example. Adding every number would count some work twice. These summaries are
clues about where to investigate, not a complete accounting of every instruction.

### Does Receipt help the real test command?

We ran a CodeCraft selection with 15 passing tests. Five direct runs had a median
of 253 milliseconds. The first Receipt run took 592 milliseconds; it ran the
tests and scanned inputs before and afterward. Five saved-result hits had a
median of 127 milliseconds. They skipped pytest but still inspected inputs.

The median is the middle duration after sorting the samples. These few ordered
samples, on one machine with warm filesystem caches, are not a promise for every
project. [The comparison](PHASE-REVIEWS.html#figure-6-local-measurements) and
[repeatable commands](MEASUREMENTS.md) let you inspect what was measured.

### What do diagnostics cost?

Each enabled timer reads the clock and updates a small record. The server writes
one summary after the request handler releases its storage locks. We deliberately
did not add a background queue: a slow destination can therefore delay handler
completion. The measured handler duration excludes the final log write.

These new summaries omit secrets and captured output. That does not make all
stderr safe to publish: the test process can still print whatever it normally
prints. Diagnostics are also not a durable history of who submitted each result.

### Try explaining it

If four simultaneous writes spend much of their time waiting for a lock, why
would adding more writer goroutines not necessarily make them finish faster?

### Answer: They still need the same exclusive access

More goroutines can wait together, but only one can hold this exclusive lock.
First measure what happens while it is held. A different locking design would
still need to protect files from cleanup and enforce the shared storage budget.

## Updates and documentation records

### 9 October 2026: Phase 6 diagnostics and local measurements

Implemented opt-in CLI timings and redacted request summaries, a fixed-size
per-operation recorder, and repeatable local/shared experiments. See the
[nine-question Phase 6 review](PHASE-REVIEWS.html#phase-6-review-find-where-the-time-went)
and [raw samples](measurements/phase-6-2026-10-09.json). Runner adapters, durable
caller history and production deployment remain separate future work.

### 9 October 2026: separate teaching from the review record

This revamp introduces a current-behavior reading path, five sequence figures, explained source excerpts, and questions with optional answers in the HTML reader. The existing phase reviews and evidence were moved to the linked reference without changing their recorded conclusions. No Go implementation changed for this revamp.

The writing approach follows [Receipt's prose guide](PROSE-STYLE-GUIDE.md), the [technical-writing guide](https://github.com/cursor/plugins/blob/main/pstack/skills/technical-writing/SKILL.md), and [unslop](https://github.com/cursor/plugins/blob/main/pstack/skills/unslop/SKILL.md). Netscope's local field notes provided the chapter-and-example model. These sources guide presentation, not claims about Receipt's implementation.

### 9 October 2026: illustrations in the phase reviews

The [phase-review reference](PHASE-REVIEWS.html) now includes ten illustrations:
manifest fields, changed input keys, shared storage, an interrupted save,
reader and cleanup coordination, upload validation, permission checks, and
revocation, upload inspection costs, and the collector's map of referenced
files. Each appears beside the explanation it supports and includes a
text version in the [Markdown reference](PHASE-REVIEWS.md). The numbered events
show possible execution orders rather than measured timings.

### 9 October 2026: rebuild the review illustrations

All five reviews now have purpose-drawn illustrations: twelve in total, including
the local hit/miss branch and request-admission slots. Four offer manual
step-through controls for interrupted publication, reader/cleanup coordination,
validation, and revocation. Connected arrows show shared file references; aligned
lanes distinguish waiting from work. The Markdown reference includes the same
SVG pictures and text explanations. These are examples of program behavior, not
timing measurements.

The SVG images were rendered and inspected. Documentation checks passed; the
local HTML reader still needs browser interaction and phone-layout verification
because of the previously recorded local-file browser restriction. No Go code
changed for this illustration update.

### 9 October 2026: show what remains after failure

The review adds five illustrations beside previously unpictured examples:
temporary changes missed by two input scans, committing a conflict warning,
files left after a failed save, partial cleanup and retry, and failure to print
an already-created credential. Phase 4's guarantees section now shows the
filesystem and database outcomes explicitly and explains why a well-formed
result is not proof that tests ran. The review has seventeen figures; this
count is not a claim that every explanation needs or now has an illustration.

### 9 October 2026: try real upstream test suites

The [real-repository lab](REAL-REPO-LAB.md) records initial tests in pytest,
Hugo, regex, Ghostty and Vue core. Each selected baseline passes. Only the
Python experiment currently demonstrates Receipt reuse: Go's probe includes
a changing temporary path, and the other three runners execute uncached.
A deliberately failing Python test also shows why a changed file must trigger
execution instead of replaying an earlier success. This is experimental evidence
and a list of next fixes, not a completed new runner-support phase.

### 9 October 2026: correct Go's changing temporary-path input

Hugo's selected tests can now be saved and replayed. Go invents a new temporary
directory for each probe; Receipt used to mistake that generated name for a
meaningful change. The correction replaces only the random suffix in the known
compiler mapping, keeping real compiler settings in the key. A deliberately
failing source change forced execution; restoring the source restored the hit.
The [dated correction review](REAL-REPO-LAB.md#9-october-2026-go-scratch-path-correction)
explains the comparison, alternatives, evidence and remaining limits. This is
a small maintenance fix, not completed Cargo/Vitest/Zig support.

### Maintaining these documents

Edit this Markdown source and regenerate the offline HTML with `python3 docs/render_working_doc.py`. Check for stale output with `python3 docs/render_working_doc.py --check`. Update the relevant teaching chapter when behavior changes, then append the dated nine-question review and evidence to both review-reference formats. Preserve historical amendments.

The HTML is a generated view of this source, not another prose copy to maintain. It works without a network connection. If your phone's file viewer shows literal HTML, use [this Markdown version](WORKING-DOC.md); it contains the same explanations and figure sequences.
