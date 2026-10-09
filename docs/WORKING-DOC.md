# Receipt: how it works, and why

Updated 2026-10-08. Phases 1–3 describe the implementation at ff45e74. The appended Phase 4 review records the new input-validation rules and failure tests; its amendments take precedence where earlier reviews describe decoding or local publication.

Suppose you run the same Python tests twice without editing anything. Running them again may repeat work you have already done. Receipt tries to save that work: it remembers a successful run and can print its saved output the next time the recorded inputs match.

That sounds simple. The interesting questions begin when a test reads something we did not record, two users get different outputs, or cleanup deletes a file another command still needs. This guide follows those questions from a personal command-line tool to a small shared service.

## How to read this guide

Start with Phase 1. It explains how Receipt describes a command and remembers its result. Phase 2 adds a server so several clients can share results. Phase 3 adds limits and cleanup. Phase 4 checks the incoming data and deliberately interrupts storage operations to inspect what remains.

Each phase answers the same nine review questions. The answers explain the current code, including later changes where they affect an earlier phase. This is not a claim that earlier implementations already had every current safeguard. Earlier committed document versions are available through Git history.

A code excerpt is copied from the attributed source file. Examples that invent file names, counts or output are labelled illustrative. They show the reasoning, not a measurement.

For phone reading, use [the Markdown companion](WORKING-DOC.md) if your file viewer displays HTML as text. In a browser, wide code blocks and comparison tables scroll inside their own boxes. The page itself should fit the screen.

The writing rules are in [PROSE-STYLE-GUIDE.md](PROSE-STYLE-GUIDE.md). In short: explain a situation and its sequence before naming the concept. A reader should be able to describe what happens, not just recognize the terms.

## What exists now?

Receipt has four implemented parts:

- Phase 1 saves successful command results on the user's computer, explains input changes, reruns results for verification and prunes old local entries.
- Phase 2 shares results through HTTP, stores result descriptions in PostgreSQL, stores output in content-named files and remembers conflicting observations.
- Phase 3 limits selected storage quantities and provides maintenance commands that coordinate with readers and writers.
- Phase 4 defines accepted JSON fields and values, rejects malformed entries and reports before storage, and tests selected interrupted storage operations.

The code also has typed command names, automated lint checks and selected error-handling helpers. These are implementation conventions, not another product phase.

There are still important unfinished areas. Receipt does not discover every dependency a test might read. Its shared server trusts the holders of one access token; it does not give different users separate roles or private tenants. Multi-machine output storage and production deployment measurements remain future work. Phase 4 implements the documented JSON contract, not a universal guarantee about every input source or reported observation.

## One command, from the user's perspective

Imagine running pytest in a Git repository. Git is used to discover files in the working tree; Receipt reads their current contents, not just the latest commit.

Receipt records those files together with the command arguments, environment-value hashes and supported runtime information. It gives that record a content-derived name, called an input key. If it finds a usable saved result under that key, it prints the result. Otherwise it runs pytest.

A usable saved result is a cache hit. Having to execute the command is a cache miss. Those names describe whether earlier work can be reused.

The distinction matters: printing a saved result is not running the tests again. If the tests normally write a report file or send a network request, replay does not repeat that side effect.

To try the personal tool, build it in the Receipt repository with “make build”, then invoke the built executable from the Git repository containing your tests. These are illustrative invocations, not a new smoke-test record:

~~~sh
/Users/siddharthsharma/Desktop/projects/receipt/receipt run -- uv run pytest
/Users/siddharthsharma/Desktop/projects/receipt/receipt explain -- uv run pytest
/Users/siddharthsharma/Desktop/projects/receipt/receipt run --verify -- uv run pytest
~~~

The double dash separates Receipt's options from the test command. “explain” compares recorded inputs with the last run. “--verify” reruns a saved result and compares its output. See the [README](../README.md) for configuration, supported commands and setup.

## Phase 1 review: remembering a local run

Reviewed 2026-10-08; explanations revised using the style guide on the same date.

### 1. What data structures are used, and why?

Imagine writing down everything Receipt needs to identify a run: its command, its working directory and the files it depends on. We need to keep those pieces together without confusing one with another.

Go's struct does that. It groups named fields with known types. Receipt calls this record a Manifest. From [internal/fingerprint/manifest.go](../internal/fingerprint/manifest.go):

~~~go
type Manifest struct {
	Version   int      `json:"version"`
	Root      string   `json:"root"`
	Directory string   `json:"directory"`
	Platform  string   `json:"platform"`
	Command   []string `json:"command"`
	Inputs    []Input  `json:"inputs"`
}
~~~

Command holds the arguments in order. Inputs holds individual records describing an input's kind, name and hash. Root and Directory are paths; Platform records the operating system and CPU architecture. Version identifies the format of this record so future changes can be distinguished from the current format. JSON is a text format for structured data; the labels beside the fields say which names to use when encoding this struct as JSON.

The square brackets in “[]string” and “[]Input” mean slices: sequences whose length can vary. One command may have three arguments; another may have ten. One repository may contain fifty inputs; another may contain thousands. A fixed-size array would force us to choose a length in advance.

Order has different meanings in these two slices. Rearranging command arguments can change the command. Rearranging the discovered input records should not change which run they describe. Receipt therefore keeps argument order but sorts the inputs before calculating the key.

From Manifest.Key in the same file:

~~~go
m.Inputs = append([]Input(nil), m.Inputs...)
sort.Slice(m.Inputs, func(i, j int) bool {
	if m.Inputs[i].Kind != m.Inputs[j].Kind {
		return m.Inputs[i].Kind < m.Inputs[j].Kind
	}
	return m.Inputs[i].Name < m.Inputs[j].Name
})
~~~

Here m is the manifest being named. The first line makes a new slice containing the input records. Why copy before sorting? A slice refers to an underlying array. Sorting that array could otherwise rearrange the caller's inputs too. The remaining lines order records first by kind and then by name.

During file discovery, Receipt also uses a map to remember paths it has already seen. A map is a lookup table. If Git and an explicit extra-input setting both identify the same file, the map helps avoid including it twice.

A list could also remember those paths. But each new path would have to search the existing list for a duplicate. The map is a better fit for repeated membership questions; the slice is a better fit for the final ordered record. Neither is universally better.

A linked list could hold the inputs, but Receipt does not need frequent insertion in the middle of a sequence. Extra nodes and pointers would add work to an operation that mostly appends, sorts and scans. A generic JSON map could hold the manifest fields, but then code would have to check what type each value contains. Named struct fields make the intended shape easier to inspect.

### 2. What is the overall flow, and where can it be extended?

A Go package groups related source files and functions. Follow one run through those packages:

~~~text
app: read the user's command and options
  → fingerprint: describe inputs and calculate their key
  → cache: look for a saved result
       found → app validates and prints it
       missing → runner executes the command
                   → fingerprint describes inputs again
                   → app saves an eligible successful result
~~~

The arrows show who calls whom; they are not separate services. All of this can happen inside one Receipt process on the user's computer.

The fingerprint package answers “What inputs are we recording?” The runner answers “How do we launch this command and capture its output?” The cache package answers “How do we store and retrieve a result?” The app package combines those operations according to the user's options.

Keeping these jobs separate makes a change easier to place. A new pytest-like tool might need a new input probe in fingerprint. A new result store belongs behind the cache methods. A new CLI option belongs in app.

That does not mean extensions are automatic. A store with basic Get and Put methods still needs correct publication and failure behaviour. A new tool adapter still needs to understand what the tool reads. Later phases add requirements that are not captured by the original two storage methods.

The [directory overview](#where-to-find-the-implementation) maps these responsibilities to files.

### 3. Which basic execution and coordination tools are used?

On a miss, Receipt must start another program. That program is a subprocess: a separate process launched by Receipt. Starting it directly avoids an extra shell interpreting the arguments.

From Run in [internal/runner/process.go](../internal/runner/process.go):

~~~go
stdout, stderr := &limitedBuffer{limit: 8 << 20}, &limitedBuffer{limit: 8 << 20}
c := exec.CommandContext(ctx, args[0], args[1:]...)
c.Dir = dir
configureProcess(c)
// Stdin is closed: a cached run cannot replay interactive input.
c.Stdout = io.MultiWriter(out, stdout)
c.Stderr = io.MultiWriter(errOut, stderr)
~~~

args[0] is the executable; the remaining arguments are passed to it. Dir selects the working directory. stdout and stderr are buffers for the command's normal-output and error-output streams.

MultiWriter copies each stream to two destinations: the user's output writer and the capture buffer. You see a miss's output as it arrives rather than waiting until the command finishes.

Each capture buffer stops growing at 8 MiB. The expression “8 << 20” calculates that number of bytes. The user can still receive later output, but a result whose capture overflowed is not saved for reuse. This bounds Receipt's capture memory; it does not bound the test program's own memory. The command's standard input, stdin, is closed: interactive answers cannot be replayed from a saved result.

The context passed to CommandContext carries a cancellation signal. On supported Unix systems, Receipt also targets the launched process group during cancellation so children in that group are not left running unnecessarily. That is process management, not isolation from the user's files or network.

Saving the result has its own sequence: write a temporary file, sync and close it, then create the final filename only if absent. Sync asks the operating system to persist writes. Creating the final name after writing prevents readers from seeing a partly written final record.

Two Receipt processes may still run the same tests at once. Only one can first publish at a given final name, but safe publication of one file does not eliminate duplicate execution or coordinate every other operation.

### 4. What can go wrong, and how would we investigate it?

Suppose a test reads a CSV file that Git ignores. If we do not declare that file as an extra input, its contents may change without the key changing. Receipt can then reuse a result whose tests would behave differently now.

This is why dependency discovery matters more than simply choosing a good hash function. Receipt includes tracked and nonignored untracked working-tree files, explicit extras, environment-value hashes, executable bytes and supported runtime/package information. It does not automatically record external databases, network responses, time, randomness or every installed-package byte.

A hash is a fixed-size value calculated from bytes. Receipt uses the SHA-256 algorithm for these hashes. The same manifest bytes produce the same input key. But that tells us only that the recorded inputs match. It does not prove that they are every input the program uses.

Receipt scans inputs before and after execution. A changed file normally prevents saving the result. But imagine the file contains A before the run, changes to B while the test reads it, then returns to A before the second scan. Receipt sees A twice. Two scans do not freeze the filesystem.

Symbolic links—paths pointing to another path—and Git submodules are not treated as fully understood inputs. Unsupported cases run without caching rather than creating a result from an incomplete description.

When debugging an unexpected hit or miss, start with “explain”, then inspect argument, environment, path and tool information. Absolute checkout paths are part of the key, so moving a repository can cause a miss even if its source files are unchanged.

Finally, replay prints saved stdout followed by saved stderr. If the original run alternated between them, replay does not reconstruct that alternation. That can explain apparent changes in message order without implying that the stored stream bytes changed.

### 5. Which work happens often, and what does it cost?

Looking for a usable result happens on ordinary runs. This is the hot path: frequently performed work. Pruning old cache entries is occasional work, or a cold path. “Cold” does not mean free.

A hit skips pytest, but still describes the inputs, reads file contents, probes supported tool information, reads the saved result and prints the output. If describing the inputs costs almost as much as the tests themselves, the cache may offer little benefit.

As an illustrative example, a one-file test in a thousand-file repository still uses the broader recorded input set. Receipt may inspect many files even though the test command names only one. This favours a conservative description over finely targeted reuse.

If T is the total file content read and F is the number of input records, hashing includes work proportional to T. Sorting the records adds roughly O(F log F) work. Big O describes how work grows with input size; it is not a measured duration. Disk speed and runtime probes also matter.

A miss pays that preparation cost, the test execution and another input scan before saving. Large captured output adds encoding, hashing and disk-writing work.

Local pruning reads the entry inventory, sorts by file modification time and removes old entries until the approximate entry-byte budget is met. Hits touch those times to record recent use. With E entries, sorting adds O(E log E) work. Separate processes can overlap, so this is not a perfectly ordered global history or a strict instantaneous byte limit.

### 6. What does the local design ensure, and what does it assume?

A saved result is checked before reuse: its payload digest must match the stored bytes, its manifest must produce the claimed key and its exit status must indicate success. This catches a changed or malformed record rather than blindly trusting its filename.

That is useful protection, but consider a dishonest person who changes the result and recalculates all its hashes. Hash checks alone do not identify the producer. They are integrity checks, not signatures.

The main assumption is that the recorded inputs adequately describe the command. If a test reads the current time, the same recorded key can produce different outputs. Deterministic execution means the same inputs always give the same output; Receipt does not establish that property.

Verification reruns and compares exit status plus output bytes. If pytest prints a different elapsed time, verification can find a conflict even if all assertions passed. The comparison means “these observations matched exactly,” not “these test outcomes were semantically equivalent.”

The local entry budget excludes history and quarantine records. Quarantine means a recorded instruction not to reuse a key after conflicting observations. It persists rather than automatically trusting the key again after some time.

Hashing environment values avoids storing their plaintext values, but hashing is not encryption. Someone may guess a simple value and hash the guess. Captured output can also contain secrets. Share results only within the trust assumptions of the tool.

### 7. What other approaches could solve these problems?

These are comparisons added to explain the design, not claims that every alternative was debated before implementation.

For input preparation, an incremental tree of hashes could help avoid reprocessing unchanged portions of a large repository. Such a structure is called a Merkle tree: parent hashes summarize child hashes. It would require rules for detecting changes reliably. It would not discover the ignored CSV file from the earlier example or make a time-dependent test deterministic.

For eviction, consider three saved entries A, B and C. If A was saved first but read most recently, deleting the oldest saved entry is not the same as deleting the least recently used entry. FIFO—first in, first out—would remove A by age. Receipt instead uses touched file times as an approximation to recent use.

A linked list plus a map can track exact recent use within one process. A heap can keep the oldest candidate readily available: it is a structure organized around finding a highest-priority item. Both approaches need maintained state if we want the benefit across many separate CLI processes. Rebuilding and sorting an on-disk inventory is simpler for a small personal cache.

For targeted reuse, a full build system could track which tests depend on which files. That could avoid a miss after an unrelated edit. But we would have to obtain reliable dependency information first. Hashing the broader declared input set accepts extra misses instead of claiming that narrow relationship.

### 8. What evidence supports the design?

[The fingerprint tests](../internal/fingerprint/manifest_test.go) check canonical key behaviour, working-tree/environment changes and unsupported-input cases. [The runner tests](../internal/runner/process_test.go) check output streaming, exit status, cancellation and capture limits. Local-store tests also exercise malformed and corrupted records.

The historical review records one CodeCraft pytest comparison: about 1.69 seconds for a miss and 0.22 seconds for a hit. That shows the particular invocation benefited. It does not establish typical speedups across repository sizes or commands.

We have not shown that all dependencies are discoverable. Systematic power-loss and disk-full testing also remain unfinished. The verification ledger later in this guide distinguishes recorded implementation checks from checks performed for this documentation update.

### 9. What would make us change these decisions?

If input scanning takes most of an ordinary run, measure where it spends time before adding incremental hashing. If users need reuse across different checkout paths, define when those paths are equivalent before removing them from identity.

If byte comparisons quarantine many useful results because of timing text, investigate explicit output normalization. That means specifying which differences can be ignored, with tests showing that meaningful differences are still caught.

If local pruning becomes slow, revisit the maintained eviction structures above. If inputs frequently change during execution, revisit the two-scan model; a stronger execution environment may be needed rather than another hash function.

## Phase 2 review: sharing results between clients

Reviewed 2026-10-08; explanations revised using the style guide on the same date. Phase 3 later adds the broader coordination used by current shared storage.

### 1. What data structures are used, and why?

Suppose Alice and Bob run different commands that both print the same output. Saving a complete copy of that output inside every result would waste space.

The shared server separates the description of a result from its output bytes. PostgreSQL stores rows describing which input key produced which result. Files hold the stdout and stderr bytes. We call a stored chunk of bytes a blob; here it means one output stream.

A row refers to its output through BlobRef. From [internal/cache/blob.go](../internal/cache/blob.go):

~~~go
type BlobRef struct {
	Hash string `json:"hash"`
	Size int    `json:"size"`
}
~~~

Hash identifies the exact expected bytes. Size records their expected length. A reader checks both when opening the file. The filename is based on the content hash, so identical output can be stored once and referenced by several results.

For illustration, 100 different input keys with the same 1 MiB stdout can reference one stdout file. There are still 100 result descriptions, and clients still upload the output. Deduplication saves repeated server-side storage; it does not automatically save upload bandwidth.

The database also stores conflict evidence. Its action table identifies an input key; its evidence table identifies distinct observations for that key. A primary key is the table's uniqueness rule. Repeating the same observation does not create another identical evidence row.

There are three different identities to keep separate:

| Identity | Example question it answers |
| --- | --- |
| Input key, calculated from the manifest | Are Alice and Bob describing the same recorded inputs? |
| Blob hash, calculated from output bytes | Is this exactly the output file the record expects? |
| Observation digest, calculated from exit status and output hashes | Did the two runs have the same recorded outcome? |

Elapsed execution duration and creation time are excluded from the observation digest. But a timing value printed inside stdout is still output, so it can make the observations differ.

### 2. How do clients and the server work together?

A client is the Receipt program making requests. The server is the Receipt process accepting those requests. PostgreSQL is the database service the server uses.

A client first describes its inputs. It can check the server's status for the key and fetch a shared result, or execute the command when no usable result is available. After a successful run, it can upload the result.

The server checks the access token and request bounds, then asks shared storage to publish or retrieve the result. Storage reads/writes the database description and the output files.

~~~text
client → HTTP request → Receipt server
                           → PostgreSQL: result description and conflict state
                           → files: stdout and stderr bytes
client ← HTTP response ← reconstructed result
~~~

The server's description uses format version 2 with file references. The HTTP result and local-cache result remain version 1 with the full output. The server reconstructs that result for the client, so a client does not need to understand the server's file layout.

Now imagine Alice publishes one output for key K and Bob later publishes a different output for K. The server records the conflict and quarantines K. Quarantine says “do not reuse this key”; it does not say which client was right.

A conflict report can also create that prohibition before any successful result has been saved. In that case the database remembers the warning without a reusable result. This small record is sometimes called a tombstone.

### 3. How does it handle overlapping requests and network work?

The server may receive many requests at once. Go runs request handlers using goroutines: independently scheduled activities inside one process.

Unrestricted handlers could start more work than the service can reasonably handle. Receipt uses a buffered channel as a limited collection of admission slots. A request puts an item into the channel when admitted and removes it when finished.

The following excerpt is from serveHTTP in [internal/cache/http.go](../internal/cache/http.go):

~~~go
select {
case slots <- struct{}{}:
default:
	metrics.rejected.Add(1)
	w.Header().Set("Retry-After", "1")
	http.Error(w, "busy", http.StatusServiceUnavailable)
	return
}
~~~

The empty struct is a token representing one occupied slot; its contents are irrelevant. If all slots are occupied, default rejects this request with HTTP 503 instead of starting more API work. The default capacity is 16. Each admitted operation also receives a 25-second context deadline.

The database connection pool has a separate purpose: reuse connections while limiting how many are open. The shared maintenance CLI allows up to 16 open connections, four idle connections and a five-minute lifetime. A request slot and a database connection are not the same resource.

Database updates use transactions. A transaction groups changes so they commit together or are rolled back together. Row locks make conflicting updates wait rather than both modifying a selected row independently.

Current shared writers also take the broader locks added in Phase 3. Different keys therefore do not imply concurrent writes. The row-level rules remain useful, but the complete current sequence serializes shared writers.

HTTP describes requests and responses. HTTPS adds transport encryption and server-certificate checks. An access token is separate: it shows that the request possesses the configured credential. Encryption alone does not decide whether the caller may upload a result.

### 4. Which cases are easy to misunderstand?

A conflicting upload is supposed to fail as a reusable publication, but its warning must still be saved.

If we automatically rolled back every database change whenever returning an error, we could report “conflict” while forgetting that K should no longer be reused. The implementation therefore commits the conflict state before returning the conflict error.

A client with a shared server configured also checks status before replaying its local copy. If the status request fails, it runs the command rather than replaying a result whose shared status it cannot check.

That check has a limit. Alice can check K, then Bob can quarantine K just before Alice finishes replaying. One status response cannot recall output Alice already read. Older clients that do not check status also do not follow this rule.

When debugging conflicting results, look at the exit status and exact output, not only whether both test suites passed. Timing text or random ordering can differ. When debugging rejected uploads, distinguish a conflict from a storage-budget failure or an unavailable server; they mean different things.

### 5. Which paths are frequent, and what do they cost?

A client fetching a shared result sends a network request, waits for the response and receives the saved output. The server reads the result description, reads the two output files, verifies them and encodes the response.

A local hit with a shared server configured still needs the status request. Thus “the bytes are already on my laptop” does not mean “no network work.” Network latency can matter even when the server has little computation to do.

Uploading involves output hashing, file work and database updates. If the output is already stored, the server still checks that existing file's bytes rather than accepting its name blindly. The current file implementation can do temporary-file work for a duplicate too.

Metrics are occasional operational requests. They report aggregate counters such as requests and failures, not a history of individual executions. The counters reset when the server restarts.

Phase 3 adds whole-store accounting to writes. That is a current cost of publication, described in its review below, not an optimization we already solved.

### 6. What can clients rely on, and who must they trust?

The server checks stored output against its expected length and hash. A malformed or changed file is not returned as a valid result simply because its name matches.

It also records conflicting observations and refuses reuse of quarantined keys. This is stronger than silently keeping the first result after learning that another observation disagrees.

But an authorized client can lie about what it ran. Conflict reports are assertions from trusted clients, not verified execution proofs. Someone with the token can prevent useful reuse by submitting false conflicting observations.

The bearer token grants shared read/write access. Receipt does not yet distinguish users, organizations, administrative roles or private tenants. It should not be exposed to unrelated untrusted writers as if those rules existed.

Clients require HTTPS for non-loopback URLs. Loopback is the local machine's network interface. That client rule does not mean a production TLS server and certificate setup are already deployed. Deployment and untrusted-code execution are separate unfinished problems.

### 7. What other storage and conflict designs could work?

The comparisons below explain current choices after implementation. The decision summary later in this guide identifies the recorded decisions; new comparisons are not claims about earlier debates.

Putting output inside every metadata record is simpler: one record contains everything. But repeated large output then consumes repeated storage. Keeping PostgreSQL descriptions and content-named files separates those costs.

Putting output bytes directly in PostgreSQL could keep their publication and the result description in one database transaction. It would also enlarge the database and change write, backup and retrieval costs. That is a plausible alternative, not inherently bad.

SQLite could reduce setup for a local deployment. Before substituting it in a shared server, we would need to test its write behaviour and operating assumptions for that deployment. PostgreSQL was chosen for shared metadata and transaction coordination; it is not established as optimal for every workload.

An object-storage service could hold bytes for servers on different machines. It adds network failures and different rules for writing, reading and deleting objects. Current local filesystem locks would not carry over automatically.

For evidence, storing every run would give more history than storing distinct observations. It would also grow faster. Ignoring conflicts would be cheaper still, but would knowingly allow reuse of contradictory results. The present design chooses distinct evidence plus quarantine within the Phase 3 budgets.

### 8. What tests and measurements support this?

[The shared-store tests](../internal/cache/shared_test.go) exercise publication, concurrent conflicts, reports, missing/corrupted output and HTTP handling. TestHTTPAdmissionAndMetricsAuth checks bounded admission and metrics access.

The historical review describes 32 equal and 20 differing concurrent uploads, six independent blob-writer processes and fetching results after a database restart. These are specific scenarios, not a proof covering every sequence of distributed events.

Production TLS, malicious token holders, multi-machine filesystem behaviour and sustained server load remain unestablished. The current tests are useful evidence for the cases they execute, not a deployment certificate.

### 9. When should the shared design change?

If output bandwidth dominates, measure uploads before changing the wire format. Server-side deduplication does not solve repeated network transfer.

Before allowing unrelated users, specify ownership and access rules. Adding a login page without authorization checks would not make one user's results private from another.

Before running independent server machines, revisit where output lives and how cleanup coordinates with every instance. If strict immediate revocation becomes necessary, the status-then-replay approach needs a different protocol; another status endpoint alone will not close that timing gap.

## Phase 3 review: limits and cleanup without deleting needed files

Reviewed 2026-10-08; explanations revised using the style guide on the same date.

### 1. What data structures are used, and why?

Suppose we want the shared cache to stop growing indefinitely. Limiting only output bytes is not enough: a large number of tiny results can still create many database rows.

Receipt stores four budgets in Limits. From [internal/cache/maintenance.go](../internal/cache/maintenance.go):

~~~go
type Limits struct {
	MaxBlobBytes     int64 `json:"max_blob_bytes"`
	MaxMetadataBytes int64 `json:"max_metadata_bytes"`
	MaxEntries       int64 `json:"max_entries"`
	MaxEvidence      int64 `json:"max_evidence"`
}
~~~

The first field limits inventoried blob bytes. The second limits the measured JSON representation of metadata. The other two limit the number of result rows and distinct evidence rows.

Initial limits are 1 GiB, 64 MiB, 10,000 result rows and 100,000 evidence rows respectively. MiB and GiB are byte units: 1 MiB is 1,048,576 bytes; 1 GiB is 1,073,741,824 bytes. These are defaults, not measured capacity recommendations.

Cleanup needs a different structure: a set of output hashes still referenced by saved results. Consider this illustrative state:

~~~text
result A → stdout X, stderr Y
result B → stdout X, stderr Z
files on disk: X, Y, Z, W
~~~

X is needed twice, but the collector only needs to know that it is needed at least once. Y and Z are needed too. W has no reference.

Collect builds the set with a map. Two separate excerpts from the function in the same source file:

~~~go
live := map[string]bool{}
~~~

~~~go
live[ref.Hash] = true
~~~

The second line records the hash from one stdout/stderr reference. In our example, adding X twice still leaves one X entry with value true. During inspection, the collector can ask whether a file's hash is in this map. Missing hashes identify unreferenced candidates, after the other safety checks described below.

A slice of referenced hashes would work too. But checking W could require scanning the whole slice; repeating that for every file does more work. A sorted slice could use binary search, repeatedly halving the search range. A map directly supports the repeated lookup we need, with memory overhead for its lookup structure.

The collector also keeps an inventory slice of files. Both the map and the inventory grow with the store. They are not fixed-memory structures.

A stored reference count could avoid rebuilding the set. That changes the problem from “what do surviving records reference now?” to “did every earlier update maintain the count correctly?” We return to that trade-off in question 7.

### 2. What happens during retention and collection?

Two commands remove different kinds of things.

Retention, called “prune”, removes old nonquarantined database records and their evidence. Garbage collection, called “gc”, removes output files no surviving record needs.

Return to results A and B above. If retention removes A, file X is still needed by B. Deleting A's output files blindly would break B. Collection therefore looks at the references that remain after retention, rather than assuming each result owns a private copy.

The collector's sequence is:

1. Take the maintenance locks.
2. Read and validate surviving result descriptions.
3. Build the map of referenced output hashes.
4. Inspect and validate the recognized file layout.
5. Report eligible deletions, or delete them if “--apply” was supplied.
6. Release the locks.

Quarantined records survive retention. If they contain saved output references, those references remain in the live map too. A known conflict is not forgotten just because the result becomes old.

Dry-run is the default for requested maintenance changes. However, opening shared storage can initialize database tables, policy and directory binding. Dry-run does not promise that first-time setup changes nothing.

These methods currently require filesystem operations such as directory inventory and removal. ManagedBlobs expresses that requirement. Postgres still accepts the broader BlobStore interface and checks this capability at runtime. A future constructor should make that requirement clearer; a cloud backend is not a plug-in replacement just because it can Get and Put bytes.

### 3. Why do readers, writers and cleanup need locks?

Safe publication of a complete file solves one problem: another process should not see half its bytes. It does not make “publish the file, then update the database” a single operation.

Without the current locks, the following sequence could occur. This is an illustrative failure trace, not a claim that current tests found this exact execution in production:

1. An upload writes and publishes output file X.
2. Before the upload commits a database record referencing X, cleanup reads the database.
3. Cleanup sees no reference to X and deletes it.
4. The upload commits its record. That record now points to a missing file.

Every individual step can succeed while the final result is wrong. We need cleanup to wait until publication has finished.

A reader has a similar problem. It may read a database record naming X before it opens X. If retention removes the record and collection deletes X in between, the later open fails. On Unix, an already-open file descriptor can remain usable after its pathname is removed; our example deliberately concerns the interval before opening.

Receipt coordinates these sequences with shared and exclusive locks. A lock is a rule for who must wait while another operation uses the resource.

Several readers may hold a shared lock together. They keep it while reading the description and output files. A writer or cleanup needs an exclusive lock: it waits for readers to finish, then makes new readers and other writers wait until its own operation ends.

The lock is therefore held across the sequence that needs protection, not merely around a single file write. This also means that a long cleanup can delay ordinary requests.

Current storage uses two lock mechanisms:

- A PostgreSQL advisory transaction lock coordinates operations through the database.
- A native filesystem lock on the directory's lifecycle lock file protects file work across cooperating processes.

Why isn't the database lock enough? Suppose a writer's database connection dies while file work is still running. PostgreSQL releases the database lock. The filesystem lock is still held until that file operation returns and cleanup releases its resources. Other participating processes cannot begin deletion just because the database connection disappeared.

Why not use only a Go mutex? A mutex coordinates activities sharing it inside one process. A separate maintenance CLI process does not share that in-memory mutex. The filesystem lock supplies process-to-process coordination for this directory.

The SQL lock choice in lifecycle, [internal/cache/maintenance.go](../internal/cache/maintenance.go), is:

~~~go
function := "pg_advisory_xact_lock_shared"
if exclusive {
	function = "pg_advisory_xact_lock"
}
~~~

“Shared” selects the reader-compatible mode; exclusive selects the mode used by publication and maintenance. The function then obtains the filesystem lock.

Every operation uses that order: database first, filesystem second. Otherwise one operation could hold the database lock while waiting for the filesystem lock, while another holds the filesystem lock and waits for the database. Neither can finish. That is a deadlock.

These locks are cooperative, also called advisory. An old program or a person deleting files manually can bypass the rules. They are not filesystem permissions and are not claimed to behave identically on every network filesystem.

### 4. What partial failures make maintenance tricky?

A database transaction can undo database changes. It cannot undo an independently published file.

Suppose the writer saves X, then its metadata update fails. There is no successful result row, but X may still exist. It is an orphan: a file no saved result references. Collection can remove it later; until then it uses space.

Cancellation creates another subtlety. A context deadline says the caller wants work to stop. It does not instantly interrupt every operating-system file operation.

If database/sql automatically rolled back on cancellation while file work continued, the SQL lock could disappear too early. lifecycle therefore starts the transaction with context.WithoutCancel and uses the caller's context for individual SQL operations. Explicit Commit/Rollback and resource cleanup control the combined lock lifetime. The filesystem lock also protects the connection-loss case.

Cleanup validates the records and recognized directory layout before starting deletion. A malformed record could otherwise make a needed file look unreferenced. An unexpected symbolic link could point outside the intended directory. Rather than guess, collection stops on those unsafe conditions.

Another mistake would be pairing database A with database B's file directory. Database A would see no references to B's files and might delete them. Bind prevents this with a store-identity marker. From [internal/cache/blob_binding.go](../internal/cache/blob_binding.go):

~~~go
if string(b) != id+"\n" {
	return fmt.Errorf("blob directory belongs to another metadata store")
}
~~~

b is the marker's bytes; id is the expected store identity. A valid marker is a regular 65-byte file: a 64-character identity plus a newline. A mismatch stops setup or maintenance.

When creating it, Bind writes a temporary marker, syncs and closes it, creates the final name only if absent, then syncs the directory. A hard link gives the already-written file its final name without copying its bytes; creating that link fails if the name already exists.

The marker checks pairing, not a person's identity. Adopting an unmarked directory still requires the operator to choose the right directory. Database and file backups must also be restored as a matching pair.

Finally, collection can fail after deleting some unused files. A database rollback will not bring those files back. Retrying can finish the remaining work, but this is not an all-or-nothing batch of file deletions.

### 5. Which work is frequent, what grows, and what makes requests wait?

Fetching and saving shared results are frequent request work—the hot paths. Retention and collection are occasional maintenance—the cold paths. That distinction describes frequency, not duration. Fetching a shared result is ordinary request work. Before loading its description and output, the server takes shared locks. Several readers can proceed together.

Cleanup takes exclusive locks. It must wait for existing readers; after it obtains the locks, new readers and writers must wait for cleanup. Thus a request with very little reading to do can still be slow because it is waiting behind maintenance.

Saving a result also takes exclusive locks. Before accepting more data, the server calculates current usage by inspecting the file inventory and database metadata/evidence. It does not simply read four continuously maintained counters.

As an illustrative example, uploading a 1 KiB result into a store with thousands of files can still inspect that existing inventory. The work depends on what is already stored, not only on the new result's size. Making other writers wait prevents two uploads from both admitting data against the same remaining budget.

Collection reads surviving references once, builds the live map, inspects the files and optionally deletes unreferenced ones. If A is the number of result records and B the number of files, the traversal has roughly O(A + B) work, plus validation and file operations. That means both populations matter.

Its map and inventory also take memory. If U is the number of unique referenced hashes, memory grows approximately with U + B. Repeated references to X do not create repeated map entries, but each inventoried file still has a representation.

Map lookup usually avoids scanning all references again for every file. It does not make the whole request constant-time: reading records, hashing output, walking directories and waiting for locks remain.

Stats performs usage calculation too. Calling it is not equivalent to reading one saved integer. Retention finds and deletes eligible database rows; its actual duration depends on row counts, indexes and the database's query plan—the method the database chooses to execute the query.

We have not measured typical or worst-observed production request delays at increasing store sizes. Current correctness choices should be defended as simple coordination for a small deployment, not as demonstrated high throughput.

### 6. What is protected, and where do those protections end?

For current operations that follow the lock rules, cleanup cannot enter between a writer publishing X and committing X's record. It also cannot enter between a reader finding X's record and finishing the output read. Those are the concrete failures the locks prevent.

The condition matters: every participant must use the rules. An older writer that ignores the locks can create a file while cleanup is deciding what to delete. A hostile local process can also change paths despite our checks. The current design assumes a controlled directory and a supported filesystem-lock implementation.

Storage limits cover selected quantities, not all physical host usage. For example, a small JSON record may occupy space in database pages and indexes too. Transaction logs, filesystem allocation and temporary copies can use additional space. A 64 MiB logical metadata budget therefore does not promise a 64 MiB PostgreSQL data directory.

There are two different “full evidence budget” cases. If a key already exists, Receipt can still mark it quarantined and mark that detailed evidence was capped. It can remember “do not reuse K” without saving another full observation.

If K is new, creating that warning record may itself exceed a budget. The shared report can fail. The client can keep local quarantine, but there is no durable retry queue guaranteeing that the shared server later receives the warning. Other clients may therefore not learn it from this failed report.

Retention also has a precise meaning. It removes old storage records; it does not revoke every client's local copy. Suppose the server deletes an old, nonquarantined result while Alice still has it locally. An absence of server quarantine is not a prohibition on Alice's replay. If we want deletion to mean revocation, we need another rule.

Known quarantine is retained rather than expired. Collection preserves its referenced output, but does not repair missing files or verify every retained file's full content. Reads do content verification when retrieving them.

These are deliberately separate promises: coordinated deletion, bounded selected quantities and remembered conflicts. There is no single rollback covering both SQL and files, no assumed network-filesystem guarantee and no claim of protection against arbitrary hostile local changes.

### 7. What could we do differently, and what would it change?

These comparisons explain the design after implementation. They are not claims that every alternative was debated before it was built.

First consider writer concurrency. We could lock by input key, letting uploads for A and B proceed together. But A and B may both reference X, and both consume one global remaining budget. Separate key locks alone do not settle who may delete X or who may claim the last available space. We would need additional coordination for those shared resources.

The current broader lock makes that reasoning simpler by letting writers take turns. The cost is waiting even when their output is unrelated.

Next consider collection. We could store a reference count beside X: the number of result records that need it. Removing A would decrease the count; saving B would increase it. A zero would be a deletion candidate.

Now imagine saving B succeeds but increasing the count does not. X can appear unused even though B needs it. A reference-count design therefore needs reliable updates through retries and failures, plus a way to check or repair counts. The current collector rebuilds its set from surviving records instead of trusting a maintained count.

We could delay deletion, perhaps by five minutes. That gives many normal uploads time to finish. It does not prove that a paused process has finished: a machine or operation can remain stuck longer. A grace period may help an existing coordination design, but is not a replacement for deciding which operations still use the file.

We could maintain running usage totals too, making admission cheaper than repeated scans. The difficult case is a file successfully published before metadata fails. The total must account for that orphan until collection removes it. Faster counters need reconciliation: a way to compare them with what is really stored and correct mismatches.

Finally, storing output in PostgreSQL could place output and metadata changes in one transaction. Moving output to an object-storage service could support independent server machines. These are distinct solutions with different costs and failure rules, not small variations of the current local-file lock.

### 8. Which tests support the important claims?

[The maintenance tests](../internal/cache/maintenance_test.go) make the failure situations above executable.

TestRetentionWaitsForReaders pauses a reader and checks that retention waits. TestConcurrentUploadsAndCollection overlaps publication and collection. They test specific waiting/deletion behaviour rather than only checking that individual methods return success.

TestLifecycleCancellationKeepsFilesystemExclusion pauses blob work, cancels the upload and tries collection with a short deadline. Collection must not enter the still-active publication. After releasing the paused work, the test checks that the lock was not leaked.

TestDatabaseConnectionLossKeepsFilesystemExclusion goes further: it terminates the upload's PostgreSQL connection while blob work is paused. Collection must still wait on filesystem exclusion. This is the reason for the second lock, tested directly.

Other tests check quota/deduplication, evidence saturation, orphan collection, retained quarantine, corrupt descriptions, unsafe directory entries and wrong-store binding.

Recorded verification includes uncached, race-enabled integration tests using a disposable PostgreSQL 14 database. Race-enabled Go tests can detect some cases where goroutines access the same memory without safe coordination; the explicit scenario tests exercise database and file coordination that the race detector alone cannot certify.

We still lack systematic power-loss, disk-full and broad I/O-error testing. Multi-machine deployment, network filesystems, sustained load and hostile path mutation are also not established. Those gaps should remain visible when defending the design.

### 9. What observations would make us revisit these choices?

Measure time spent waiting for locks separately from time spent doing work. If ordinary uploads or reads wait too long, we can investigate shorter maintenance batches or more detailed coordination. The trigger should be measured behaviour, not the word “distributed.”

If most upload time goes into usage scans, investigate maintained counters with orphan accounting and reconciliation. If collection memory is too high, consider processing inventory in chunks—but retain a safe view of the references throughout deletion.

Before running server instances on independent machines, choose a shared output-storage protocol and test its failure rules. Do not assume a filesystem lock on one machine coordinates another machine's objects.

If quarantine accumulates indefinitely, define who may clear it and why clearing is safe. Automatic age-based deletion would change what the system remembers about conflicting results.

If users expect a physical disk ceiling or immediate revocation, those are different requirements from logical budgets and retention. Name them explicitly before changing the implementation.

## Coding-quality review: explaining our implementation conventions

These changes reorganize and check code; they do not add another product phase. Their detailed rules remain in [CODING-PRACTICES.md](CODING-PRACTICES.md).

### When should an error stop the next step?

Consider opening a file, writing it and publishing it. If opening fails, there is no valid file to write. If writing fails, we should not publish it as complete. Returning immediately after these errors keeps that dependency visible.

Printing several CLI lines is a narrower problem. If printing the first line fails, later printing is unlikely to help. A small helper remembers the first error and skips subsequent printing.

From [internal/app/printer.go](../internal/app/printer.go):

~~~go
type printer struct {
	out io.Writer
	err error
}

func (p *printer) println(args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintln(p.out, args...)
	}
}
~~~

out is the destination. err starts empty. Each println checks it; after a failed write, later calls do not write. The caller must still return/check err at the end.

This helper belongs to one command and is not safe for concurrent use. It must not become shared error state in a server: one user's failure should not stop another user's request. Closing files, unlocking and rolling back must also still run after failure; those are not optional later printing steps.

For independent validation, stopping early can be less useful. If two limits are negative, the user should be able to see both problems in one response. Limits.validate collects these field errors and finishes with this actual line from [internal/cache/maintenance.go](../internal/cache/maintenance.go):

~~~go
return errors.Join(errs...)
~~~

errs is the slice of validation errors collected for the four budgets. Join combines them into one returned error, or returns nil when there are no errors. Here nil means no error was recorded. This is safe because checking one limit does not require another limit to be valid. Continuing a dependent file-writing sequence would be a different matter.

Must helpers turn failure into a panic. They can fit programmer-owned constants, such as a hard-coded regular expression that should never be invalid. Missing credentials or an unreachable database are normal operational failures; they should produce useful errors instead.

A general closure-chain framework was not added. Named operations keep sequencing, cancellation and cleanup explicit. It can be reconsidered for a genuinely uniform local sequence, not to hide necessary error checks.

### Why use typed command names?

A CLI receives strings such as “stats” and “gc”. We validate those strings, then represent known choices with named string types and constants. Later dispatch code uses the constants instead of independently spelling the same command everywhere.

Go still allows constructing an invalid value through a conversion. The type is not a completely closed set. The parser remains responsible for rejecting unrecognized input.

The exhaustive lint rule checks whether switches cover the known constants. Adding a new command should make missing dispatch cases visible. It does not prove that every received string is valid.

### What do the automated checks actually check?

Formatting gives code consistent layout. Lint tools inspect source for selected mistakes and maintainability problems. Tests execute selected behaviours. They answer different questions.

golangci-lint is the main engine. Enabled checks include govet, staticcheck, errcheck, goconst, revive, depguard, ineffassign, unused, nolintlint, exhaustive, gocognit, nestif and errorlint; gofumpt formats code.

errcheck flags ignored errors. errorlint checks common problems with wrapped errors. depguard restricts selected package imports. gocognit and nestif flag complicated branching/nesting, with thresholds of 30 and 5. The scores are review prompts, not proof that a function is well designed.

Some cleanup errors deliberately do not replace the main error, and tests currently have an explicit errcheck exception. These exceptions are documented; they are not claims that ignored operations cannot fail.

### Why isn't a struct enough to validate JSON?

Imagine a request repeats a field or omits a required value. Decoding into a struct may not reject every distinction the product cares about.

A schema is the set of rules for accepted fields and values. A strict decoder must define required fields, unknown fields, duplicates, nulls and limits, then test them. A named InputKind alone does not enforce those rules.

Configuration decoding rejects unknown fields and extra JSON values. Result/report decoding is not yet a complete strict firewall. Strengthening it is proposed work, with compatibility tests needed so intentional existing clients are not silently broken.

## Where to find the implementation

| Location | Question to ask when reading it |
| --- | --- |
| cmd/receipt | How does the executable start and return an exit status? |
| internal/app | How does one user's command combine lookup, execution and publication? |
| internal/fingerprint | What inputs are recorded, and how is their key calculated? |
| internal/runner | How is the subprocess started, streamed and cancelled? |
| internal/cache | How are results stored, served, quarantined and cleaned up? |
| docs | What did we decide, test and leave unfinished? |

Go's internal directory limits which Go packages may import these packages. It does not restrict a program's file or network permissions. The package layout organizes responsibilities inside the program; it is not a deployment diagram.

## Decision history and current explanations

Architecture decision records—ADRs—are short records of choices and reasons. Their current summary follows; earlier committed wording can be retrieved from Git history.

ADRs 001–004 chose Go, separated storage from HTTP transport, introduced content identity/first publication and selected pytest/CodeCraft as the first workload. The current shared conflict handling was added later; first publication never proved identical execution.

ADRs 005–009 described Git-aware inputs plus extras, sorted manifests and payload digests, environment/executable identity, bounded successful output and pre/post input scans. The limitations discussed in Phase 1 still apply: broad input discovery is not a snapshot or complete dependency discovery.

ADRs 010–011 covered a trusted HTTP service and approximate local pruning. The original shared-storage plans are now partly implemented by Phases 2–3, not all still future work.

ADRs 012–015 recorded PostgreSQL metadata with output references, conflict evidence/quarantine, shared status before local replay and bounded requests/aggregate metrics. The status check is not universal immediate revocation, and server-side file deduplication does not deduplicate every upload over the network.

Phase 3 adds full-store accounting, reference-based collection, combined database/filesystem coordination and preservation of quarantine during retention. Its review explains their costs. Newly added alternative comparisons are teaching material, not an invented account of earlier debates.

## Verification: implementation evidence versus documentation checks

The following implementation results were previously recorded for ff45e74:

| Check | What was exercised |
| --- | --- |
| make check | Formatting, lint configuration/checks and unit tests |
| make race | The race-enabled test suite |
| make build | Executable build, including the macOS linker setup |
| Fresh PostgreSQL 14 integration tests with race detection | Uncached shared-storage scenarios on a disposable database |
| CodeCraft smoke timing | One historical miss/hit comparison |
| Temporary added enum constant, removed afterwards | Lint caught missing switch coverage |

These are scoped observations. They do not establish crash recovery under every failure, universal deterministic outputs or performance at production load.

For this documentation update, checks cover the three nine-question reviews, twelve attributed Go excerpts, matching HTML/Markdown content, local links and the copies' integrity before their subsequent removal at the user's request. Browser checks at 320, 390 and 1200 pixels found no page-wide horizontal overflow; code and wide tables keep their own scrolling. Runtime tests are not being rerun for prose-only changes.

The style guide also requires a manual reading check. In this revision, the local input/slice explanation, the shared upload/conflict explanation and Phase 3 questions 5–7 were reviewed for named actors, concrete sequences and costs. Those are editorial checks, not evidence from a study of beginner readers.

## What should the next learning phases teach?

The original 10–12 week idea is a sequence for learning and useful product development, not a statement about elapsed weeks. The personal cache and small shared-store foundations already exist.

One useful next step is input explanation: help a user see which recorded change caused a miss. That makes dependency and serialization ideas visible through a real problem.

Another is stronger request validation and deliberate failure tests. For example, force a write failure after a temporary file exists and check what the next operation sees. That teaches recovery through observable behaviour rather than only reading error-handling rules.

Identity and security can remain a shared platform learning track across projects. Start with concrete access questions: who uploaded this result, who may read it, who may quarantine it and who may administer storage? Authentication establishes identity or possession of a credential; authorization decides what that identity may do.

WorkOS/Clerk-style topics would include sessions, organizations, token lifecycle and those access decisions. Infrastructure-oriented topics would include service credentials, TLS, network boundaries, deployment configuration and finding the cause of failed requests. These are future topics, not current features.

Before adding independent server machines, learn and specify the output-storage rules. Before claiming a performance improvement, measure representative runs and separate lock waiting, file work, database work and network time.

A module can be reused when its requirements fit the next project. The concepts may transfer sooner than the implementation: another project may need different ownership and access rules.

## Future updates

This revision follows [the prose style guide](PROSE-STYLE-GUIDE.md). Future phase reviews should use the same nine questions and explain the normal sequence before exceptions. Keep attributed code, evidence scope and revisit conditions.

Redundant document snapshots were removed from the repository at the user's request on 2026-10-08. Keep the current HTML and Markdown guides in agreement; use Git history for earlier committed versions.

A future handoff should let the user find both the explanation and its evidence. It should not describe a document as beginner-friendly simply because it has a glossary or because its links pass a check.

## Phase 4 review: checking incoming data and interrupted writes

Implemented and reviewed 2026-10-08. This is a behaviour change after e80950e, not another prose-only rewrite. It implements the validation/failure-testing phase agreed in chat. Identity, roles and multi-machine storage are not part of this phase.

Imagine Alice uploads a successful pytest result. Her request says exit_code is zero, but repeats that field later with the value one. Which value should Receipt believe? It now rejects the request. The client must supply one clear value rather than depend on how a decoder handles repeated names.

The exact acceptance rules are in [JSON-INPUT-CONTRACT.md](JSON-INPUT-CONTRACT.md). The review explains how those rules are applied and what the failure tests show.

### 1. What data structures are used, and why?

The decoder needs two small collections. One records which field names the type permits. Another records which names have already appeared in this object. Actual lines from [internal/strictjson/object.go](../internal/strictjson/object.go):

```go
allowed := make(map[string]Field, len(fields))
for _, field := range fields {
	allowed[field.Name] = field
}
seen := make(map[string]bool, len(fields))
```

For Alice's result, allowed contains exit_code and its presence/null policy. After reading the first exit_code, seen records true for that name. Reading it again produces a duplicate-field error. A map lets the decoder ask about a name directly. These maps belong to one decode call; one request cannot add names to another request's map.

Field is a small struct describing a name and whether it is optional, nullable, a string or a string array. Most result fields are required and cannot be null. Empty output streams are an intentional exception: earlier Go writers used null for nil byte slices, so that spelling remains accepted. The output itself must otherwise be a base64 string—a text encoding of bytes—not a numeric JSON array.

A slice of field descriptions keeps required-field checks in a stable order. A slice alone could also track encountered names, but would need repeated searches. Reflection could discover struct tags automatically, but required/null rules would still need additional metadata. This phase chooses explicit declarations because a reader can inspect the contract beside the type's hook. These are comparisons made during this phase, not invented earlier decision history.

### 2. What is the architecture and flow? Where can it extend?

Alice's HTTP entry follows this sequence: bound the body size, check the envelope's fields, compare the checksum with the payload bytes, decode and validate the result, compare its key with the requested key, then call storage. A checksum catches changed bytes; it does not decide whether those bytes describe a valid result.

Each important type has an UnmarshalJSON method. Go calls this method when decoding that type. The method first reads into a temporary value and assigns the real receiver only after its checks pass. These actual lines from Result.UnmarshalJSON in [internal/cache/json.go](../internal/cache/json.go) show the final steps:

```go
if err := Validate(Result(next)); err != nil {
	return err
}
*r = Result(next)
return nil
```

Here next is the temporary decoded result. Validate checks its values and relationships. The assignment to r happens only after success, so a failed decode cannot leave a partly updated result that a caller might reuse.

Nested types have their own checks: Result contains Manifest, Manifest contains Input records, and shared metadata contains BlobRefs. The helper checks object shape; the owning type checks meaning. For example, a known input kind and a matching manifest/key are semantic rules—rules about what values mean—not just JSON syntax.

Direct Go struct construction does not invoke a JSON hook. Disk.Put, Postgres.Put and Postgres.Report therefore still validate values before storage. Configuration has optional fields and preserves defaults. It copies existing slices before decoding so even a late type error cannot mutate the caller's old list through a shared backing array.

To add a future report type, declare its fields and semantic rules without adding storage work to the decoder. Adding fields to an existing strict version is a compatibility change: old readers reject unknown names. That needs a version or migration decision rather than just a new Go field.

### 3. What concurrency, networking and fundamental primitives are used?

No new locks, goroutines or endpoints were needed for validation. The decoder's maps and temporary values belong to one request. Concurrent requests do not share that state. The existing HTTP body limits, admission slots and storage locks continue to do their separate jobs.

The shape check reads JSON tokens: names, delimiters and values in their received order. Reading names before converting the object to a map matters. If we first decoded Alice's repeated fields into a normal map, the earlier value could already have been replaced. Actual code from readField in [internal/strictjson/object.go](../internal/strictjson/object.go):

```go
if seen[name] {
	return fmt.Errorf("duplicate field %q", name)
}
seen[name] = true
```

name is the decoded JSON field name, so an escaped spelling of the same name also counts as a repeat. Required names use exact case; Exit_Code is not treated as exit_code.

Independent semantic errors use errors.Join: checking a negative duration does not require the manifest to be valid first. Dependent operations still stop on failure. We do not continue writing files after a failed file operation merely to collect more errors. Existing shared-storage locks still make cleanup wait while an upload is publishing files and metadata; JSON validation does not replace that coordination.

### 4. What is tricky to implement or debug?

Missing, zero and null are different inputs. A success exit code of zero is valid; a missing exit_code is rejected. A null output stream remains valid; null duration_ns is rejected. A null member of a command array is rejected even though ordinary Go decoding can otherwise turn it into an empty string. An explicit empty string argument after the executable remains valid.

A hook calling json.Unmarshal directly into its own type would call itself again. Each hook uses a local named wire type with the same fields but without that outer method. Nested field types retain their own methods. This avoids recursion without turning off validation for nested inputs.

PostgreSQL JSONB keeps normalized JSON rather than the original text. Duplicate names from a manually inserted row are already gone by the time Receipt reads it. That is why incoming JSON must be checked before persistence. Read-time checks can reject an unknown field still present in a row; they cannot reconstruct erased information.

The local publication failure test found a real bug. Disk.Put used to treat an existing destination as success without reading it. A directory or corrupt file was therefore enough to produce a false acknowledgement. The changed branch in [internal/cache/store.go](../internal/cache/store.go) now reads the existing result:

```go
if os.IsExist(e) {
	// First publication wins only if the existing entry is actually reusable.
	// A directory or corrupt file at the destination is not a successful Put.
	_, e = d.Get(ctx, r.Key)
	return e
}
```

This does not overwrite or repair a damaged destination. It reports the failure, removes the temporary file through existing cleanup, and lets the caller decide what to do next. A valid earlier result still wins even if a later local observation differs; shared conflict handling remains a separate policy.

### 5. What are the hot and cold paths? What do they cost?

Result decoding is frequent work on a cache hit or upload, so it is a hot path. Each object is inspected once for its field rules and decoded again into typed values. Nested values are inspected by their hooks too. This adds CPU work and temporary allocations; it is not a speed optimization.

The helper temporarily holds the current field's raw bytes. Decoding output strings also allocates their byte slices. The existing 32 MiB entry/body limit and 8 MiB stream limits bound selected quantities, not total process memory across many requests. Configuration now has a separate 1 MiB read limit. Previous-manifest history and arbitrary operator-written database rows do not gain a new read-size limit from this helper.

For a manifest with many inputs, validation checks each record and remembers each kind/name pair to detect repeats. The work and memory grow with the number of inputs. A duplicate local publication now reads and validates the existing entry before returning success, adding disk and checksum work to that less common path.

Failure tests and fuzzing are development-time work, not production request handlers. Fuzzing generates and mutates many inputs to explore decoder behaviour. Its run count is evidence about that test session, not a requests-per-second benchmark. No production latency or memory distributions were measured in this phase. Shared write accounting and cleanup retain the store-size costs described in Phase 3.

### 6. What are the guarantees, assumptions and non-goals?

Under the documented schema, malformed HTTP entries/reports fail before storage is called. Direct invalid result/report writes fail before publication. Decode returns an empty Result and an error matching ErrCorrupt on failure. Metadata readers check the typed record before opening its output files; collection checks all inspected records before beginning deletion.

Those checks do not prove Alice's tests actually ran. A holder of the trusted-team token can still invent a structurally valid observation. Identity, permissions and execution evidence remain separate questions. Recorded keys still do not prove deterministic output.

The failure tests make the two storage systems visible. Suppose stdout file X is published, then saving stderr fails. No database result becomes visible, but X remains. Suppose both files are published and the database insert fails. SQL rollback removes the attempted database changes, not the files. Collection can remove those unreferenced files later. A failed operation is therefore not a promise that nothing changed on disk.

If cleanup deletes one orphan and the next deletion fails, the first deletion stays done. The next cleanup recomputes the surviving references and remaining files. This phase tests that retry and checks that live output stays readable. It does not introduce a transaction covering SQL and filesystem changes.

Valid v1 writer encoding and key calculation remain unchanged. Required zero fields, zero timestamps, null output/input slices and empty context strings remain accepted where documented. Previously tolerated malformed records are intentionally rejected, not automatically repaired. The helper uses the standard decoder's remaining Unicode-escape and base64 behaviour; it is not a replacement JSON implementation.

### 7. What alternatives were considered?

Using only DisallowUnknownFields would be shorter, but would not distinguish all our required cases: repeated names and omitted zero-valued fields still need rules. Decoding into an arbitrary map first would lose repeats. Adding checks only in the HTTP handler would leave local files, shared metadata and direct storage calls with different acceptance rules.

We chose small type hooks plus semantic validation. The hooks cover serialized boundaries; direct-write validation covers Go values. Pointer-only wire structs could distinguish omission from zero, but would add another representation for every field and would not solve duplicate names by themselves.

A full JSON Schema engine could become useful if external clients need generated contracts or many independently evolving message types. Today it would add a dependency and still need tests for key/checksum relationships and storage behaviour. It is deferred, not inherently wrong.

For failure tests, a large fake filesystem would allow every write/sync/close operation to be injected. We instead exercised the actual local file code with an occupied destination, wrapped the real BlobStore for selected failures, and used a real PostgreSQL constraint failure after file publication. This keeps the tests close to actual storage, but leaves low-level sync/disk-full faults untested. These choices were made in this phase.

### 8. What evidence supports the claims? What remains untested?

TestTypedJSONBoundaries removes, repeats, changes case and supplies disallowed nulls for every declared field of results, reports, references, shared records and envelopes. Separate manifest/input tests cover unknown kinds, bad hashes, repeated input identities and null command members. TestHTTPInvalidInputNeverCallsStorage uses a store that counts calls; all tested malformed requests returned 400 without a write/report call.

TestVersionOneEncodingUnchanged compares the current writer with the previous field representation and decodes the resulting envelope. The existing input-kind/key compatibility fixture still passes. Configuration tests check optional defaults, explicit zero, invalid names, bounded reads and preservation of existing slices after failure. These tests do not cover every historical third-party client.

The local tests check temporary-file cleanup after an occupied-destination failure and rejection of a corrupt existing entry. PostgreSQL tests check failure on the second blob, a real database constraint failure after both blobs, cleanup failing on its second removal, malformed stored metadata and invalid direct writes. They inspect files, database rows and later reads/retries, not only returned errors.

Verification on macOS arm64 with Go 1.22.1 and a fresh disposable PostgreSQL 14.17 database: make check, make race, make build, and an uncached race-enabled full suite with the database configured passed. The database tests created isolated schemas. Two 15-second fuzz sessions with two workers completed without a failure: 378,575 executions for FuzzDecode and 390,885 for FuzzResultJSON. The latter directly explores result payloads without first needing to pass the envelope checksum. These are exploratory evidence, not exhaustive coverage or performance measurements.

Power loss, disk-full errors, individual file write/sync/close failures, database commit-acknowledgement loss and arbitrary operating-system/filesystem behaviour were not established by these new tests. Existing cancellation and terminated-database-session tests also passed in the full integration run. Network filesystem and hostile local path changes remain outside the stated assumptions.

The Markdown and HTML reviews are kept in agreement, with local links, headings and source excerpts checked separately from runtime tests. Editorial checks use the prose guide's actor/sequence/example questions; they are not a study proving beginner comprehension.

### 9. When should we revisit these decisions?

If external clients need additional fields or kinds, decide how versions coexist before modifying the schema. If legitimate old records fail, retain a representative fixture and decide whether to migrate or adjust a specific compatibility rule. Do not broadly ignore unknown data just to hide the failure.

If parsing appears in CPU or allocation measurements for representative workloads, measure the shape pass and typed pass separately before replacing them. A different decoder must preserve the same malformed-input and compatibility tests. If manifests become large, measure the repeated-identity map and source-size bounds too.

If operations must promise that no files remain after a failed upload, the current orphan-and-collect policy is insufficient; it needs a different publication/recovery protocol. If orphan accumulation routinely exhausts budgets, revisit collection scheduling and observability. If hardware/commit failures matter for deployment, add controlled fault or process-crash tests before claiming recovery under those conditions.

If Receipt moves beyond trusted token holders, add identity and per-operation authorization rather than asking JSON validation to establish trust. The next learning phase can separate permission to read, publish, report conflicts and administer storage. That work is proposed, not implemented here.

### Amendments to earlier reviews and decisions

Phase 1's first-publication explanation now includes verification of an existing local destination; corruption or an occupied directory is not acknowledged as a successful Put. Phases 1–3 and the coding-quality section previously described stronger schema validation as future work. Phase 4 implements the contract above while retaining checksum, key, lifecycle-lock and authorization responsibilities separately.

Phase 4 decisions: keep existing v1 writer/key bytes; explicitly preserve documented null/zero compatibility; place object-shape rules in a small shared helper and value rules with their owning types; validate direct writes; retain orphan-and-collect recovery and test selected interruptions. Making ManagedBlobs an explicit constructor requirement, identity/permissions, multi-machine storage and performance measurement remain unfinished work.
