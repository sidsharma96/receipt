# Receipt — mobile working companion

This is the phone-readable companion to [the living HTML document](WORKING-DOC.html). If a phone's file viewer displays literal HTML, that is not evidence the page is broken: the viewer may be treating the attachment as text instead of rendering a web page. Use this Markdown companion in a Markdown/text viewer. Serving the HTML through a web server is another option; no public deployment is configured.

## Phase review convention

After **every implemented phase**, update both documents and answer these nine questions in the chat, using actual code snippets where applicable: data structures; architecture and extension boundaries; concurrency/networking primitives; pitfalls; hot/cold paths; guarantees/assumptions/non-goals; alternatives and tradeoffs; evidence/untested failures/risks; conditions for revisiting decisions. Connect substantial claims to code, evidence and limitations. Label retrospective alternative comparisons honestly. Distinguish implemented behavior from future plans. Keep earlier reviews as dated records; append a new review when behavior changes. Sections 6–9 below expand this review on 2026-10-08 without changing implementation behavior.

Current status: **Phase 2 local shared-storage milestone implemented**. Postgres metadata, content-addressed filesystem blobs, shared conflict evidence/quarantine and HTTP counters/admission control are available. The original review below is historical; the appended Phase 2 review supersedes its shared-storage limitations where stated. Cloud object storage, blob GC/retention, tenant isolation and deployment are not implemented.

## Review: 2026-10-08 — local tool + trusted-client HTTP baseline

This is a review of the implementation delivered so far, not a claim that the entire 12-week roadmap has graduated. Postgres, separate output blobs, tenant identity/authorization, sandboxed execution and distributed conflict coordination remain future work. The CLI supports run, explain, cache inspection/pruning and serving the shared cache.

### 1. Data structures: what and why?

From `internal/fingerprint/manifest.go` (field tags omitted here):

```go
type Input struct {
    Kind string
    Name string
    Hash string
}

type Manifest struct {
    Version   int
    Root      string
    Directory string
    Platform  string
    Command   []string
    Inputs    []Input
}
```

A **struct** is a named group of fields. An Input records one dependency, such as a file or environment variable. Its hash is a compact fingerprint of its value. The Manifest collects the declared dependencies of a command, including its location and platform.

A **slice** (`[]Input`, `[]string`) is a variable-length sequence. The command must preserve argument order: `pytest tests -q` and a different argument sequence are not interchangeable. Input discovery order must *not* affect identity, so inputs are sorted before serialization and hashing:

```go
func (m Manifest) Key() string {
    m.Inputs = append([]Input(nil), m.Inputs...)
    sort.Slice(m.Inputs, func(i, j int) bool {
        if m.Inputs[i].Kind != m.Inputs[j].Kind {
            return m.Inputs[i].Kind < m.Inputs[j].Kind
        }
        return m.Inputs[i].Name < m.Inputs[j].Name
    })
    b, _ := json.Marshal(m)
    return Hash(b)
}
```

The first line copies the slice: a Go struct copy does not copy the slice's underlying elements. Without it, sorting could unexpectedly modify the caller's manifest. Stable keys mean stable identity for the recorded inputs, **not deterministic test results**.

Maps (`map[string]bool`) act as sets to deduplicate file paths. Manifest comparison uses maps from input identity to hash so it can find additions, deletions and changes without comparing every pair.

`cache.Result` groups the manifest, key, stdout/stderr byte slices, exit code, duration and creation time. Entries use versioned JSON plus a SHA-256 payload checksum. This makes inspection/debugging simple, but reading an entry also reads its logs; byte slices become base64 in JSON, increasing size. Output is inline, not a separate deduplicated blob store.

Eviction uses a slice of entry metadata sorted by modification time. This is **approximate disk LRU**, not a hash map + doubly linked list giving constant-time LRU operations. With E entries, pruning needs roughly O(E log E) sorting plus directory/metadata reads. No Merkle tree is implemented or required for this baseline.

### 2. Flow, architecture and extensibility

```text
cmd/receipt → app: flags, configuration, policy
                 ↓
             fingerprint: declared inputs → key
                 ↓
             local disk lookup → optional HTTP lookup
                 ↓                         ↓
             valid hit                 no usable hit
             replay output             runner executes command
                                       fingerprint again
                                       store eligible success

HTTP server → authentication + validation → Store → disk
```

`cmd/receipt/main.go` is intentionally small:

```go
func main() {
    os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr))
}
```

The entry point connects the operating system to the application. `app` decides when to reuse, execute, verify or quarantine a result. `fingerprint` defines identity; `runner` owns subprocess behavior; `cache` owns storage and HTTP transport. Separating these concerns lets us test them independently.

From `internal/cache/store.go`:

```go
type Store interface {
    Get(context.Context, string) (Result, error)
    Put(context.Context, Result) error
}
```

An **interface** describes required behavior without choosing an implementation. The HTTP handler depends on this contract rather than filesystem details. Another store can implement it, and its behavior can be tested against the same expectations.

This is an extension seam, not free reuse. The CLI still directly manages disk history, quarantine and eviction. Postgres/object storage would require consistency rules and a versioned result format for blob references. New runner adapters currently mean extending fingerprint probing, not installing plugins. Shared identity/authorization can become a reusable platform module, but today's bearer token is not that module yet. Those changes deserve explicit phases rather than pretending the current interface solves them all.

### 3. Concurrency, networking and fundamental primitives

From `internal/runner/process.go`:

```go
c := exec.CommandContext(ctx, args[0], args[1:]...)
c.Dir = dir
configureProcess(c)
c.Stdout = io.MultiWriter(out, stdout)
c.Stderr = io.MultiWriter(errOut, stderr)
c.WaitDelay = 2 * time.Second
```

`exec.CommandContext` starts a subprocess using separate arguments, without automatic shell interpretation. A **context** carries cancellation/deadlines. `MultiWriter` sends each stream both to your terminal and a bounded capture buffer. Capture stops growing at 8 MiB per stream; output continues streaming, but oversized results are not cached. Stdin is closed: interactive input is outside the replay contract.

On supported Unix platforms, a process group lets cancellation target the command and descendants that remain in its group:

```go
c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// Inside the cancellation callback:
err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
```

This is not a sandbox, and a process that escapes the group is not covered. The fallback on other platforms does not provide equivalent group termination.

Disk publication uses a temporary file in the destination directory, writes and syncs it, then:

```go
e = os.Link(tmp, p)
if os.IsExist(e) {
    return nil
}
```

The hard link publishes a complete file only if the destination does not already exist. Concurrent writers therefore follow **first publication wins**; readers do not see a partly written final file. File and directory syncs address crash durability separately. There is no application mutex around this publication, but it is misleading to call the whole system lock-free: the filesystem performs coordination. Also, two processes can both execute tests before publishing. There is no singleflight, lease or distributed execution coordinator.

Networking uses `net/http`: GET and PUT of bounded, validated entries, bearer authentication, request contexts, client/server timeouts, and redirect refusal in the default client. The client requires HTTPS outside loopback. The server uses concurrent request handling; shutdown uses a goroutine (independent task) and channel (notification mechanism) to respond to cancellation. HTTP sharing shares stored results, **not remote test execution**.

### 4. Pitfalls and debugging

The most important safety check in `internal/app/app.go` is:

```go
after, e := fingerprint.Build(ctx, dir, command,
    cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
stable := e == nil && after.Key() == key
```

Changed inputs prevent storage, but a file could change and change back while tests run. This is a before/after check, not a snapshot or hermetic execution environment.

Other tricky boundaries:

- **Missing dependencies:** ignored files, external files, network responses, clocks and randomness are not automatically fully modeled. Declare extra inputs, or bypass caching. Python package versions assume installed packages are unchanged; editable package source needs explicit inclusion.
- **Surprising misses:** environment values and absolute checkout paths participate in identity. Different shell state or checkout paths can prevent reuse even when code appears unchanged. Use `receipt explain -- <command>` to compare recorded inputs; it reports local cache state, not remote availability.
- **Verification noise:** `--verify` compares stdout/stderr byte-for-byte. A pytest duration change can trigger a mismatch even when the assertions match. Do not casually strip output until a semantic contract is defined.
- **Replay limits:** replay writes stdout then stderr; it does not preserve their original interleaving or reproduce test side effects. A displayed cached test duration belongs to the original run, not the current invocation.
- **Trust:** checksums detect corruption, not malicious fabrication. Trusted token holders can publish plausible fake successes. One bearer token provides no tenant isolation or read/write roles. Environment-value hashes are not encryption and may expose guessable values.
- **Conflict scope:** quarantine is local, so another client may still reuse the remote entry. Existing-entry publication does not itself detect differing outputs. Verification is explicit, not automatic on every hit.
- **Operational edges:** refresh removes an old entry before publishing a replacement; this is not an atomic swap. Some history, cache population and timestamp errors are best-effort. Large file reads consume memory; hard links/directory syncs depend on filesystem support. These deserve failure-injection tests and clearer diagnostics as the tool matures.

### 5. Hot and cold paths

**Hot path** means the repeated path we want fast; **cold path** is work done less often or when reusable state is missing. These labels depend on workload: frequent misses make the miss path hot too.

```text
Local hit: fingerprint → read/check entry → replay → touch/history
Remote hit: fingerprint → local miss → GET/check → local write → replay
Miss: fingerprint → lookups → execute → fingerprint again
      → encode/sync/publish → optional PUT → prune
Verify: lookup → execute anyway → fingerprint again → compare/quarantine
```

A local hit skips tests, but still reads/hashes all selected files, probes the tool environment and decodes logs. Roughly, fingerprinting costs O(B + N log N), where B is input bytes and N is input records, plus subprocess/filesystem overhead. Replay costs scale with entry/output size. It is **not O(1) for the whole hit**.

Remote hits add network latency and local disk publication. Misses pay execution plus a second fingerprint, syncs and pruning. Pruning currently runs after successful miss storage and can become a bottleneck with many entries. Explain, list and maintenance are normally cold paths; listing currently decodes entire entries, so it is not a cheap metadata index. Separate blobs/indexes, incremental hashing and background pruning are future options that need correctness evidence first.

### 6. Guarantees, assumptions and non-goals

The contract is reuse of successful observations under declared inputs, not proof of deterministic behavior. `cache.Validate` checks version 1, a valid key matching `Manifest.Key()`, and exit code zero. The application also avoids publishing interrupted, oversized or input-changing runs.

From `internal/cache/store.go`, after writing and syncing a temporary file:

```go
e = os.Link(tmp, p)
if os.IsExist(e) {
    return nil
}
```

Successful linking is the visibility point for a new entry: readers see a complete file. File and directory syncs address durability, subject to operating-system/filesystem behavior. If the destination already exists, success does not establish that its output matches the submitted observation. HTTP 204 therefore does not mean the server retained that writer's particular bytes. A lost reply leaves the client uncertain whether publication happened; retry does not replace an existing entry, but deletion/pruning can change that state.

Assumptions: stable working trees, correctly declared dependencies, supported filesystem operations, and trusted shared-cache writers. Non-goals today: hermetic execution, reproduced side effects, remote execution, deterministic results, exactly-once execution, global quarantine, portable checkout keys or tenant isolation. Refresh is removal followed by publication, not an atomic replacement.

### 7. Alternatives and tradeoffs

This is a **retrospective comparison**, not a claim that every alternative was formally evaluated before implementation.

- Whole-repository hashing versus a dependency graph: broad simple coverage now, at the cost of misses from unrelated edits. Dependency selection needs evidence that omitted files cannot affect a command.
- JSON with inline output versus separate blobs: easy inspection and fewer moving parts now; base64 and whole-entry reads cost space/memory. Blobs need versioned references, cleanup and missing-object handling.
- Filesystem versus a database: no service dependency now; weaker indexing and shared coordination. A database introduces operations and transaction design, and does not automatically make external blob writes atomic.
- First publication wins versus execution leases: simple complete publication now; duplicate execution and conflicting observations remain possible. Leases require expiry, crashed-owner recovery and protection against stale owners.
- Shared token versus per-client identity/roles: sufficient only for a small trusted group; no isolated authorization or individual revocation. Identity is deferred, not solved by the token.
- Byte-exact versus semantic verification: simple, auditable comparison now; harmless timing text can mismatch. Normalization must specify what differences it may hide before becoming trusted behavior.

### 8. Evidence, untested failures and remaining risks

Claim-to-evidence examples:

- Stable ordering: `Manifest.Key()` sorts input records; `TestCanonicalKey` checks reordered inputs keep their key and changed arguments alter it. This does not establish complete input discovery.
- Concurrent publication: `Disk.Put` uses create-if-absent linking; `TestDiskAtomicConcurrent` uses 32 goroutines and checks the first entry survives a later different output. This is not a separate-process crash/durability or concurrent-conflict-resolution test.
- Input-change protection: the post-run manifest comparison is exercised by `TestChangedInputsNotStored`. Change-and-restore during execution remains undetected.
- Remote authentication/transport checks: `TestHTTPConformance` exercises missing/wrong credentials and valid round trips; `TestRemoteRejectsPlaintextNonLoopback` checks unsafe URL rejection. This is not a full security audit or live TLS deployment test.

Prior unit/race/vet/build checks and the CodeCraft smoke test are recorded below. The race detector checks exercised in-memory races; it does not prove cross-process or distributed correctness. One miss/hit timing pair is not a benchmark.

Untested or insufficiently characterized scenarios include independent-process publication, crash injection around sync/link, disk-full/permission errors, lost network replies, refresh/prune/read races and aggregate overload. A 32 MiB upload limit does not cap memory across concurrent uploads. Server pruning is explicit, so valid uploads can exhaust disk. Crashes may leave pending temporary files. There is no documented key/schema migration or correctness-bug invalidation policy yet.

Security/operations risks also include secret-bearing output, guessable environment hashes, shared-token compromise, absent per-client quotas, and undefined retention/token-rotation/deletion procedures. These are release gates before expanding trust, not reasons to claim production readiness now.

### 9. Conditions for revisiting decisions

- Add selective/incremental fingerprinting when repeated measurements show hashing or unrelated-edit misses materially reduce value; require equivalence/invalidation tests first.
- Split output into blobs when measured storage, transfer or whole-entry decoding costs justify the added consistency and cleanup work.
- Add shared metadata/transactions when indexing, global conflict handling or multi-client lifecycle rules exceed the filesystem contract. Define authority and partial-failure recovery before choosing storage.
- Add execution deduplication when measured duplicate expensive runs matter; tolerate duplication until a correct ownership/expiry design exists.
- Replace the shared token before untrusted or mutually isolated clients are admitted. Define tenants, permissions, revocation and data handling first.
- Introduce semantic verification only after observed false mismatches yield an explicit normalization contract and tests preserving meaningful differences.
- Change input-key semantics only with a documented version/invalidation policy; version fields alone are not a migration plan.

Measure value using direct execution versus full Receipt invocation, with repeated trials, input/tool state, local/remote hit rates and timing distributions recorded. Include workloads where Receipt is slower. Do not add distributed infrastructure simply to make the project appear more advanced: each new phase should serve a measured product need or an explicitly labeled learning experiment.

### Verification evidence and next learning gates

Previously completed checks: `make test`, `make race`, `make vet`, `make build`; toy pytest fixture; CodeCraft's selected run-length-encoding suite (15 passing tests), then a hit. Observed invocation times were approximately 1.69 s miss and 0.22 s hit on one machine. This is a smoke test, not a controlled benchmark or proof of universal cache correctness. No implementation changes were made for this review.

Next gates: define cache eligibility per workload; collect repeated end-to-end and fingerprint timings; exercise concurrent conflicting publications, refresh/prune races and crash recovery; define semantic versus byte-exact verification; then design shared conflict evidence and tenant authorization before broadening trust.

Review validation: `make test` passed again (cached results). Direct `go test ./...` hit the known macOS/Go linker issue, `missing LC_UUID`; the Makefile selects the external linker on Darwin. Use the project test command on this machine. Both document formats now contain all nine review sections. HTML mobile layout scrolls long code blocks and tables; a phone viewer that treats HTML as text still needs the companion or a browser-served page. The nine-question expansion is documentation-only; prior test evidence is not a new test run for this amendment.

## Phase 2 review: 2026-10-08 — locally verified shared storage

For the expanded plain-language walkthrough, see the [Phase 2 learning amendment](#phase-2-learning-amendment--2026-10-08). The original review below is preserved.

Scope: turn the trusted-client HTTP baseline into an inspectable, persistent shared cache. This completes the local shared-storage milestone, not cloud deployment or every future operational requirement. No existing external database was changed; verification used an isolated PostgreSQL 14.17 cluster on a private Unix socket.

### 1. Data structures and reasons

From `internal/cache/blob.go`:

```go
type BlobRef struct {
    Hash string `json:"hash"`
    Size int    `json:"size"`
}
type BlobStore interface {
    Get(context.Context, BlobRef) ([]byte, error)
    Put(context.Context, []byte) (BlobRef, error)
}
```

A reference replaces inline output in stored metadata. Its SHA-256 identifies bytes; its size helps reject damaged or oversized objects. Equal output shares one object, including empty output. A version-2 `blobRecord` contains a v1 result header without logs plus stdout/stderr references. PostgreSQL tables use primary keys for action identity and `(key, digest)` for distinct observation evidence. Observation digest includes exit code and output hashes, **not** execution/creation time. Reports retain hashes, not an additional complete log copy.

### 2. Architecture, flow and extension boundaries

```text
client → bounded/authenticated HTTP → Postgres Store
PUT: validate → publish/check output blobs → metadata transaction
GET: read metadata/quarantine → fetch/check blobs → v1 result response
verify mismatch → POST conflict report → persistent shared tombstone
local copy → remote status check → replay only if status allows it
```

`Postgres{DB *sql.DB, Blobs BlobStore}` composes database metadata and object bytes. `Store` remains unchanged; HTTP v1 and standalone local disk entries remain compatible. `ConflictStore` adds status/report behavior rather than assuming all stores implement it. A cloud adapter can implement `BlobStore`, but must supply the same integrity and durable publication behavior. Multiple API servers must share blobs as well as the database. Filesystem blobs are the implemented object backend; S3/R2 are not.

### 3. Concurrency/networking primitives and reasons

From `internal/cache/shared.go`, the publication transaction:

```sql
INSERT INTO receipt_entries_v2(key,digest,record,quarantined)
VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING;
SELECT digest,quarantined FROM receipt_entries_v2
WHERE key=$1 FOR UPDATE;
```

The primary key coordinates the first insert; the row lock serializes subsequent decisions for that key. Differing observations set quarantine and append evidence before commit. Blobs are written first, outside the transaction, avoiding database locks during filesystem writes. Startup migrations use a transaction-scoped advisory lock. These are explicit locks, not lock-free algorithms. Consult [PostgreSQL INSERT](https://www.postgresql.org/docs/current/sql-insert.html) and [row locking](https://www.postgresql.org/docs/current/explicit-locking.html).

HTTP admission uses a bounded channel; nonblocking acquisition returns 503 when full. Atomic counters track aggregate outcomes. Admitted requests have a 25-second context deadline; client operations retain 10-second timeouts. Database pooling caps connections. Parameters carry values separately from SQL text. The pinned [lib/pq driver](https://github.com/lib/pq) connects `database/sql` to Postgres.

### 4. Pitfalls and debugging

- Database transactions do not include blob writes. A failed commit may leave orphan objects; GC must later prove an object is unreferenced before deleting it.
- Missing blobs fail reads; a valid re-upload restores them. Corrupt blobs fail reads and re-uploads rather than silently overwriting immutable data. Automatic repair is not implemented.
- A local hit with a configured remote now needs a status request. A network outage or legacy server's 501 causes execution, not offline replay.
- Status and replay are separate operations: a later quarantine cannot recall output already read. Old clients lack status checks; removing remote configuration opts back into standalone semantics.
- Reports are trusted assertions. A compromised writer can quarantine valid keys. Report failures retain local quarantine but have no durable retry outbox; shared propagation can fail.
- Byte-exact output still includes timing noise. Captured oversized outputs may produce only partial output identities in reports; local verification treats exceeding the capture limit as non-reusable, not proof of semantic failure.
- Environment drift remains significant: the CLI smoke test observed a `SHLVL` change causing a miss, correctly explained by the tool.

### 5. Hot/cold paths and costs

Standalone local hits are unchanged. Shared local hits add a metadata-only status request. Remote hits read one metadata row, verify up to two output blobs and rebuild inline v1 JSON; deduplication currently saves server disk space, **not wire bytes**. Miss publication adds blob sync/checks, a metadata transaction and evidence insertion. Output hashing still costs proportional to output bytes. Contending same-key uploads wait on database coordination.

Migration is a startup/cold path. Counters are cheap aggregate operations but not latency histograms. Request admission limits active API work, not all connections or exact process RSS. GC, index-only admin listing and controlled workload benchmarks remain future work.

### 6. Guarantees, assumptions and non-goals

Eligible successes become visible at metadata commit after complete blobs exist. Same exit/output identities are idempotent even if timing differs; conflicting uploads return 409 **after committing quarantine and evidence**. Explicit reports create persistent quarantine even before an entry exists. Future successful uploads cannot clear it. Failed observations can be evidence but are never replayable successes.

From `internal/cache/shared.go`:

```go
if err = tx.Commit(); err != nil {
    return err
}
if conflict {
    return ErrConflict
}
```

Returning a conflict after commit is deliberate: rolling back would erase the safety decision. A lost reply remains ambiguous; a retry consults the existing state. Database and filesystem durability are assumed to follow their documented storage guarantees. We do not promise two-resource atomicity, exactly-once tests, malicious-writer resistance, hermetic inputs, tenant isolation, cross-path portability or instant revocation of in-flight replay.

Compatibility policy: metadata uses separate `_v2` tables and a record version 2; key semantics, wire result version 1 and local entries are unchanged. Old disk data is not automatically imported. Unsupported stored record versions are rejected. Future key-policy changes need a new manifest version and explicit invalidation strategy.

### 7. Alternatives and reasons

This phase deliberately chooses Postgres rather than adding an ad-hoc filesystem lock protocol for shared evidence. JSONB keeps metadata inspectable; query-critical key/digest/quarantine fields remain explicit columns. A dedicated database is an optional operational cost, not a local requirement.

Filesystem CAS precedes a cloud adapter so integrity/publication tests run without credentials. Blob-first publication accepts recoverable garbage rather than making partially uploaded output visible. Row locks provide simpler same-key conflict reasoning than leases, which would solve duplicate execution rather than observation consistency. Shared-status checks favor safety over offline availability. Cloud storage, GC, execution leases and semantic normalization are deferred for the reasons above.

### 8. Evidence, untested failures and risks

Verification performed with database tests enabled and caching disabled: full unit/integration suite; full race suite; vet; build. `make integration` requires `RECEIPT_TEST_DATABASE_URL`; ordinary tests skip the database cases without it. Tests create/drop uniquely named isolated schemas, not the application's tables.

Evidence includes 32 equal concurrent database uploads; 20 differing concurrent observations; timing-insensitive equality; persistent quarantine/evidence; report-before-upload tombstones; missing-object re-upload; corrupt-object rejection; HTTP status/report behavior; overload 503 and metrics authentication; local-copy invalidation and outage behavior. Six independent subprocesses exercise blob publication, in addition to goroutine tests. The built CLI fetched a stored result through the still-running HTTP service into a fresh client cache after a real database stop/start. This verifies restart recovery, not power-loss durability.

Remaining tests: power-loss/crash injection at every publication boundary, disk-full/permissions, lost commit acknowledgements, independent API processes racing metadata operations, prolonged load/memory measurements, TLS deployment and repair/GC races. No controlled performance improvement is claimed for Phase 2. Counters reset on API restart; no durable monitoring stack is deployed.

Operational limits: no shared disk quota, retention, GC or per-client limits. A request cap does not prevent repeated uploads filling storage. Even bounded admitted requests can allocate substantial memory for payloads and JSON copies. Secret-bearing logs and environment hashes still need a data-handling policy. Do not expose the service to untrusted writers.

### 9. Conditions for revisiting decisions

Add cloud objects when clients need multi-node blob availability or filesystem storage is operationally inadequate; implement conformance tests before trusting the adapter. Add GC/quotas before sustained shared use; design reference-safe deletion with concurrent uploads. Add durable report retries if connectivity failures make conflict propagation unreliable. Add histograms/tracing when aggregate counters no longer explain measured latency.

Replace shared tokens before adding mutually isolated tenants. Revisit status-check availability only with an explicit bounded-staleness policy. Add remote blob transfer/deduplication only if measured wire costs justify a protocol version change. Upgrade the pinned SQL driver/toolchain through tested dependency changes, not silently. Execution deduplication and selective input hashing still need measured product value and separate correctness gates.

### Phase 2 decisions

- ADR-012: optional Postgres authority plus content-addressed filesystem blobs; v2 metadata, unchanged v1 wire/key semantics.
- ADR-013: record differing output identities and quarantine in the same transaction; explicit trusted-client reports create permanent tombstones.
- ADR-014: new remote-configured clients check shared status before local replay and execute on unavailable status; acknowledge the in-flight race and legacy-client boundary.
- ADR-015: authenticated aggregate counters, bounded admission and request deadlines; no production-resource or observability claim.

Follow-up gates: GC/retention and quota design; durable conflict reporting; measured deterministic workloads; cloud adapter conformance; identity/authorization before broadening trust. Commands and operational boundaries are in README.

## Phase 2 learning amendment — 2026-10-08

This amendment expands the nine-question review in plain language. The earlier review and its evidence remain intact. No implementation or guarantee changed, and no new runtime tests were performed for this documentation amendment.

Phase 1 asks, “Can I safely reuse my own recorded result?” Phase 2 adds, “How do several clients share results, and what happens when they disagree?” The goal is understandable ownership and failure rules, not a claim of production readiness.

### 1. Data structures: separate the description from the bytes

A blob is simply a chunk of bytes. Here, stdout and stderr are blobs. From `internal/cache/blob.go`:

```go
type BlobRef struct {
    Hash string `json:"hash"`
    Size int    `json:"size"`
}
```

Instead of naming output after a test run, we name it using a SHA-256 hash of its contents. Identical output shares a stored object, even across different results. This is content addressing. The size checks expected length; the hash checks contents.

```text
Result A → stdout reference ─┐
                            ├→ one blob containing "PASS"
Result B → stdout reference ─┘
```

From `internal/cache/shared.go`:

```go
type blobRecord struct {
    Version int     `json:"version"`
    Head    Result  `json:"head"`
    Stdout  BlobRef `json:"stdout"`
    Stderr  BlobRef `json:"stderr"`
}
```

The header keeps the manifest, exit code and timing, with inline output cleared. The references say where to retrieve output. Postgres stores this metadata, not the full output bytes.

Two identities matter: the **action key** identifies declared inputs and command; the **observation digest** identifies exit code and output hashes. Same action with different observations signals disagreement. It does not tell us whether the cause is randomness, missing inputs, timing text or dishonest reporting. The evidence table has one row per distinct `(key, digest)`; it is not a complete event-by-event audit log.

### 2. Architecture: who owns which decision?

The shared store composes two systems (`internal/cache/shared.go`):

```go
type Postgres struct {
    DB    *sql.DB
    Blobs BlobStore
}
```

Postgres answers, “Does this result exist, and is it eligible for reuse?” The blob store answers, “Where are the output bytes, and are they intact?”

```text
Upload: validate → write/check blobs → database transaction → commit
Read: check metadata/quarantine → fetch/check blobs → reconstruct response
```

Blobs come first so published metadata never intentionally points at output that has not finished uploading. `BlobStore` is the extension boundary for future cloud storage. The interface makes substitution possible; it does not supply cloud durability, permissions or recovery behavior. Multiple servers need access to the same blobs as well as the same database.

### 3. Concurrency and networking: different mechanisms solve different problems

Two writers for one action key must not independently make contradictory eligibility decisions. Publication uses these statements inside one transaction (`internal/cache/shared.go`):

```sql
INSERT INTO receipt_entries_v2(key,digest,record,quarantined)
VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING;

SELECT digest,quarantined FROM receipt_entries_v2
WHERE key=$1 FOR UPDATE;
```

The first statement creates the entry if absent without replacing an existing one. The second locks its row while deciding whether observations agree. Other writers for that key wait; different keys can generally proceed independently. The transaction makes evidence and quarantine changes commit together or roll back together.

HTTP admission has another job: avoid unlimited active cache work. From `internal/cache/http.go` (comments shortened):

```go
select {
case slots <- struct{}{}:
    // Acquired a slot; process the request.
default:
    metrics.rejected.Add(1)
    w.Header().Set("Retry-After", "1")
    http.Error(w, "busy", 503)
    return
}
```

The bounded channel acts as available slots. Excess requests are rejected rather than placed in an unbounded queue. Contexts carry cancellation/deadlines; atomic counters allow concurrent updates without lost increments. Row locks protect decisions, slots limit active work, and deadlines limit how long cancellable work waits. None of these prevents duplicate test execution.

### 4. Pitfalls: two stores do not become one transaction

A database transaction cannot undo a filesystem write. If blobs succeed but metadata fails, the blobs remain with no references: they are orphans. We accept extra unused bytes rather than advertising missing output. Future garbage collection must not delete a blob another upload is about to reference.

The most easily misunderstood code is (`internal/cache/shared.go`):

```go
if err = tx.Commit(); err != nil {
    return err
}
if conflict {
    return ErrConflict
}
```

Why commit before returning an error? Detecting disagreement successfully produced a safety decision: quarantine the key. Rolling back would erase it. Conflict is a meaningful application outcome, not automatically a database failure.

Another race remains: checking shared status and replaying local output are separate operations. A quarantine can happen between them. We cannot recall output already read. Failed conflict reporting also has no durable retry queue; local quarantine can survive while shared propagation fails.

### 5. Hot and cold paths: what work did we add?

Shared local hits now add a metadata-only status request. Remote hits read metadata and up to two blobs, verify their hashes, and build the response. Uploads hash output, write/sync objects, and run a transaction.

Deduplication saves server disk space, **not network bytes**: HTTP still returns full output. Repeated uploads are not free either; the filesystem implementation writes a temporary file before discovering and checking the existing object. Standalone local hits still hash declared inputs as before.

Migration is startup work, usually a cold path. Counters give totals, not detailed latency distributions. Admission bounds active API work, not exact process memory or all incoming connections. Garbage collection is planned maintenance, not implemented behavior.

### 6. Guarantees: what can we honestly promise?

For the Postgres backend, equal exit/output identities do not conflict merely because creation time or execution duration differs. Different observations commit shared quarantine and evidence. Quarantined shared results are rejected. Failed executions can be evidence, never reusable successes. Retrieved output must match its reference's length and hash.

Important nuance: ignoring the separate duration field does **not** remove timing text printed inside stdout. That text still changes the output hash and can cause a conflict.

Explicit reports are trusted assertions, not proof that a conflicting execution happened. Complete input discovery, deterministic results, exactly-once execution, malicious-client resistance and immediate recall of in-flight reads are not guaranteed. Durability depends on the configured database/filesystem behavior. A reported mismatch is not automatically a genuine wrong hit.

### 7. Alternatives: why this design now?

This phase chooses Postgres for persistent, coordinated shared decisions rather than designing a custom filesystem lock protocol. Filesystem blobs make the content-addressing contract testable without cloud credentials. Blob-first publication favors orphan cleanup over visible missing output.

Shared-status checks favor safety over offline availability: if status cannot be established, run tests instead. Execution leases are deferred. A lease would try to prevent two clients running tests; our transaction coordinates decisions **after** they have observations. Those are different problems. Earlier broader alternative comparisons remain explicitly retrospective where decision history was not recorded.

### 8. Evidence: what did the tests actually demonstrate?

The implementation review records successful integration/race/vet/build checks. Database tests exercise equal and differing concurrent uploads, timing-insensitive equality, quarantine/evidence, report-before-upload tombstones, missing/corrupt output and HTTP behavior. Client tests check local-copy invalidation and outage behavior. Six independent subprocesses exercise blob publication. A database stop/start followed by a fresh-client fetch demonstrates persistence through that restart.

Relevant tests include `TestPostgresConformance`, `TestPostgresConcurrentConflictAndReport`, `TestPostgresMissingBlobAndHTTP`, `TestBlobIndependentProcesses`, `TestSharedQuarantineBlocksLocalReplay` and `TestSharedOutageDoesNotReplayLocalCopy` in `internal/cache/*_test.go` and `internal/app/shared_test.go`.

These do not prove power-loss durability, sustained-load performance, production security or safe GC. Goroutines exercising concurrent SQL operations are not independent API processes. The race detector does not prove distributed correctness. No performance improvement is claimed for Phase 2. Missing quotas/retention and secret-bearing logs remain operational risks.

### 9. Revisit conditions: what should cause the design to change?

- Add GC/quotas before sustained shared use can exhaust storage; deletion must respect concurrent uploads and references.
- Add cloud blobs when several servers need reliable shared output access, with the same conformance expectations.
- Add durable report retries when connectivity makes conflict propagation unreliable.
- Add tenant identity/authorization before mutually isolated users share the service.
- Add execution deduplication when measured duplicate expensive runs justify its ownership/recovery rules.
- Change verification normalization only after defining which output differences are safe to ignore.
- Add richer latency metrics when aggregate counters cannot explain measured delays.

The defense is not “Postgres plus blobs is scalable.” It is: “Each component owns a clear responsibility, the failure rules are explicit, and the evidence has a stated boundary.”
