# Receipt

Receipt wraps a test command, fingerprints its declared inputs, and replays a
previous successful result when those inputs match. Implemented in Go; the local
version uses the standard library, while the optional shared backend uses the
pinned `lib/pq` PostgreSQL driver. No database or cloud account is needed locally.

The documentation is a book with a sidebar, search and a question bank,
published privately at <https://receipt-docs.sidsharma.dev>. Its source is the
Markdown under `docs/`, which also reads well on GitHub: begin at
[Start here](docs/START-HERE.md), then follow [the working guide](docs/WORKING-DOC.md).
To defend the design under questioning, read [the design defense](docs/DESIGN-DEFENSE.md):
the questions an expert reviewer would ask, with alternatives, costs, failure
behaviour, a threat model and known gaps. Test yourself with the
[question bank](docs/questions/question-bank.md). The [dated phase reviews](docs/PHASE-REVIEWS.md)
retain detailed decisions, alternatives, and verification evidence.

To build the book locally, install [mdBook](https://github.com/rust-lang/mdBook)
0.5.4 and run `make docs`. It writes the site to `docs/book/` and runs the
documentation checks: code links still point at their symbols, relative links
and heading anchors resolve, and chapter questions match the bank. CI builds the
same book on every push to `main` that touches the docs and deploys it to
Cloudflare Pages.

## Current state (15 October 2026)

- Phases 1 to 7 are implemented: the local cache, PostgreSQL metadata with
  content-addressed output files and shared quarantine, quotas and garbage
  collection, strict JSON contracts, scoped service credentials, timing
  diagnostics, and bounded `cargo test --frozen` support.
- One private cache server is live at `https://receipt.sidsharma.dev` on a
  DigitalOcean droplet. Caddy terminates HTTPS and forwards to Receipt on
  `127.0.0.1:7777`, which runs with `--postgres --credentials --max-inflight 2`
  against a local PostgreSQL. See [the deployment record](docs/DIGITALOCEAN-SETUP.md).
- Receipt's own CI uses that server over HTTPS, next to an independent job that
  always runs the full suite. Its token expires on **12 November 2026 at 03:28
  UTC** and must be renewed by hand; see [the CI guide](docs/RECEIPT-CI.md#secrets-and-renewal).
- There is no scheduled backup and no monitoring. One encrypted backup was
  restored successfully in a drill on 13 October 2026.
- The [Fly deployment proposal](docs/FLY-DEPLOYMENT.md) includes local templates,
  an architecture picture and a preliminary budget. It was never deployed.

## Start

Phase 6 adds opt-in diagnostics: `receipt run --timings -- ...` prints a timing
summary to stderr, and `receipt serve --request-log ...` emits one redacted JSON
summary per request with an `X-Receipt-Request-ID` response header. See
[measurement instructions and limits](docs/MEASUREMENTS.md) for repeatable experiments.

```sh
make build
./receipt help
./receipt run -- uv run pytest
./receipt run -- uv run pytest       # local hit when inputs match
./receipt explain -- uv run pytest  # compare with the last run's manifest
```

Run from the workload's Git directory. Receipt includes the entire repository
even when you run just one test file. For CodeCraft, using an absolute executable:

```sh
cd /Users/siddharthsharma/Desktop/interview-prep/CodeCraft
/Users/siddharthsharma/Desktop/projects/receipt/receipt run -- uv run pytest 01_data_structures/tests/test_15_merkle_tree.py
```

An existing uv environment is required for a cache lookup. If the probe fails,
Receipt runs the command normally. If uv creates or updates the environment
during that run, Receipt skips storage until the inputs stabilize.

Supported probes: `uv run pytest`, `python -m pytest`, `python3 -m pytest`,
an absolute-shebang `pytest`, `go test`, and bounded `cargo test --frozen`.
The [Cargo contract](docs/CARGO.md) lists accepted options and refusal cases;
build scripts and procedural macros currently force uncached execution.
Other commands require
`--allow-generic`, which explicitly accepts responsibility for declaring their
inputs. Run flags go **before** `--`; test flags go **after** it.
For `go test`, the probe records `go env -json`. Go puts a random scratch
directory name (`go-build` followed by digits) into `GOGCCFLAGS` on every call,
so Receipt replaces only those digits with `<scratch>`; any other change to the
Go environment still changes the key.

## Useful commands

```sh
receipt run --bypass -- uv run pytest     # no lookup, fingerprint or storage
receipt run --refresh -- uv run pytest    # execute and refresh local result
receipt run --verify -- uv run pytest     # execute a hit and compare output
receipt cache list
receipt cache prune --max-bytes 104857600
```

`--verify` compares stdout, stderr and exit code byte for byte. Timing text,
randomized test ordering and output paths can cause mismatches without a test
failure. A mismatch exits nonzero and quarantines that key **on this client**.
Quarantined keys are neither replayed nor republished locally. Quarantine is
persistent; there is deliberately no automatic "trust it again" policy.
`--verify` exits 0 on a match, 1 when inputs changed during the rerun
(inconclusive), and the rerun's own nonzero code, or 1, on a mismatch.
`--refresh` quarantines a key only when the fresh run fails after a cached
success on unchanged inputs; a successful refresh replaces the local entry.
The three modes are mutually exclusive. See
[run modes](docs/DESIGN-DEFENSE.md#d6b-what-do---verify---refresh-and---bypass-do).

Misses stream output immediately. Hits replay stdout then stderr; original
cross-stream interleaving, terminal behavior and test side effects are not
reproduced. Commands read standard input from `/dev/null`, so a prompt gets
end of file at once. Failures and interrupted commands
are never stored. Cache/network errors fall back to execution where possible.

## The input contract

Each manifest includes:

- Git-tracked and untracked nonignored working-tree files, including permissions
  and deleted tracked-file markers. The Git commit alone is not the identity.
- Exact arguments, absolute working directory and repository root, OS and CPU
  architecture, executable bytes, and resolved toolchain metadata.
- Environment variable names with SHA-256 hashes of their values.
- Python implementation, interpreter location and installed distribution versions
  for supported pytest invocations.
- Explicit extra inputs from `.receipt.json`.

The cache defaults to `.receipt/`, which is excluded from fingerprints. Symlinks
and submodules cause an uncached run rather than an incomplete fingerprint.
Input changes between pre-run and post-run scans prevent publication. A file
changed and restored during execution is not detectable with these two scans;
run tests on a stable working tree.

**This is a declared-input cache, not a hermetic execution sandbox.** Ignored
data files, external databases, network responses, time, randomness, external
editable packages, and changes to installed package bytes without version
changes are not discovered automatically. Add files to `extra_inputs`, keep the
environment immutable, or bypass the cache for those tests. In particular,
do not blindly cache CodeCraft's timing/performance/concurrency suites.

Absolute paths deliberately prevent reuse between different checkout locations.
Portable keys and sandboxed remote execution are future phases. All environment
values are hashed by default, which favors extra misses over unsafe hits. Hashes
are not encryption; low-entropy values can be guessed. Only share manifests with
trusted users.

## Configuration

Optional `.receipt.json` at the workload's repository root:

```json
{
  "extra_inputs": ["fixtures/private-data.csv"],
  "ignore_environment": [],
  "max_bytes": 1073741824,
  "remote": ""
}
```

Extra input paths are relative to the repository root; absolute files and
directories are also allowed. Missing extra inputs cause an uncached run.
Ignoring an environment variable is a correctness decision: only ignore it if
it cannot affect the test. Receipt's own `RECEIPT_TOKEN` is never fingerprinted
and is removed from the test command's environment. The config file itself is always fingerprinted,
including when Git-ignored.

Configuration is limited to 1 MiB. JSON names are exact and case-sensitive;
duplicate names, unknown fields and null scalar settings are rejected. The same
strict-shape checks apply to stored results and shared requests. See the
[JSON input contract](docs/JSON-INPUT-CONTRACT.md) for required fields, permitted
nulls, compatibility and size limits. Existing v1 writer/key encoding is unchanged.

The local cache has a 1 GiB default entry limit. Hits touch entry modification
times; pruning evicts least recently touched entries. This disk LRU is an
approximation across processes, with a soft size bound during concurrent runs.
Each entry is written to a temporary file, synced to disk, then published with a
hard link, which creates the entry only if none exists. The first complete entry
for a key wins and is never overwritten, so a crash never leaves a partial entry.
History and quarantine metadata are outside that entry-byte budget.

## Shared cache (trusted clients)

For separate caller credentials, expiry, revocation and per-operation permissions,
use `receipt serve --postgres --credentials`. Manage credentials locally with
`receipt auth issue`, `receipt auth list` and `receipt auth revoke`; see the
[service-credential guide](docs/SERVICE-CREDENTIALS.md) for setup and limitations.
Each token has the form `rcpt_<32 hex id>_<64 hex secret>` (102 characters
from 48 random bytes). The server stores the id and the token's SHA-256 hash,
never the token itself. At most 1,024 credential rows may exist, and expired and
revoked rows still count. There is no purge command, so issuance fails once the
cap is reached until old rows are deleted by hand.
All callers still share one cache. Legacy shared-token mode remains available
without `--credentials`; credential mode never falls back to that token.

For legacy mode, set `RECEIPT_TOKEN` to a randomly generated secret of at least 24 characters on
both server and clients. Keep it out of command arguments and config files.

```sh
receipt serve --listen 127.0.0.1:7777 --cache-dir /path/to/private/server-cache
receipt run --remote http://127.0.0.1:7777 -- uv run pytest
```

For another machine, put the server behind a TLS reverse proxy and use an HTTPS
URL. The client rejects plain HTTP except on loopback. Both GET and PUT require
the token; uploads are limited to 32 MiB and verified against the manifest key
and payload digest. This is integrity validation, **not proof that a client ran
the tests honestly**. The token grants shared read/write access. There is no
tenant isolation, per-client role model, or anonymous access. The legacy disk
server has no shared quarantine; Phase 2's Postgres backend does.
Do not expose this service to untrusted writers. Without `--postgres`, server
storage uses the Phase 1 disk implementation and is pruned explicitly with
`cache prune --cache-dir ...`. Shared maintenance/quotas are now available below.

## Phase 2: Postgres metadata + content-addressed blobs

Use a dedicated database and a private blob directory. Keep credentials in
environment variables, not CLI arguments. Never point integration tests at a
production database. No existing database/server is modified by default.

```sh
export RECEIPT_DATABASE_URL='postgres://receipt:password@localhost/receipt?sslmode=disable'
# sslmode=disable is for a trusted local setup only; use verified TLS remotely.
receipt serve --postgres --cache-dir /absolute/private/receipt-shared --max-inflight 4
receipt run --remote http://127.0.0.1:7777 -- uv run pytest
```

`RECEIPT_TOKEN` is still required. Startup creates `receipt_entries_v2` and
`receipt_evidence_v2` transactionally under a migration lock. Provision this
dedicated database/user yourself; startup needs schema creation privileges.
The server stores output in SHA-256-addressed files under `blobs/` and metadata
with version-2 blob references in Postgres. Empty/repeated output deduplicates
across entries. The v1 HTTP payload and Phase 1 local files remain unchanged.

Publication writes/verifies blobs first, then publishes metadata in a
transaction. Every upload holds the exclusive storage lifecycle lock described
below, so uploads for any keys run one at a time. The row lock taken during
publication is defence in depth. Equal exit/output identities are
idempotent despite timing differences. Different output quarantines the key and
retains both observation identities in the evidence table. Explicit verification
reports can also quarantine failed observations; failed runs are not reusable
results. Reports are assertions by trusted clients, not proof of execution.

GET/PUT of quarantined keys return HTTP 409. New clients check
`GET /v1/status/<key>` before local replay whenever a remote is configured.
Unavailable status means execute, not offline replay. Conflict reports use
`POST /v1/conflicts/<key>`. A legacy disk server returns 501 for these operations,
so new clients conservatively rerun local hits against that backend. Old clients
can still read v1 payloads but do not participate in local-copy invalidation;
upgrade all clients for the new policy. Checks cannot recall already-read output.
Removing the remote setting opts back into standalone local semantics.

Authenticated `GET /v1/metrics` returns aggregate request, active, rejection,
conflict, miss, failure and cumulative-duration counters. Counters reset on
restart and have no token/path/output labels. `--max-inflight` bounds admitted
API work (default 16, range 1–128); excess returns 503 with Retry-After. Admitted
work has a 25-second context deadline. Each storage operation also holds one of
the 16 pooled database connections, so values above 16 cannot run more storage
work at once. The live server uses `--max-inflight 2`, and credential mode gives
authentication the same number of slots. These are not a hard process-memory limit
or a substitute for reverse-proxy connection limits and disk quotas.

Missing blobs are rejected; re-uploading their original bytes restores them.
Corrupt blobs are rejected and never silently overwritten. Metadata failure can
leave orphan blobs: the maintenance commands below collect them. Automatic
corruption repair, cloud/S3 storage and multi-node blob availability remain deferred. `cache prune`
is for legacy/local entries only; it does not prune Postgres or blob references.
Multiple servers must share the same verified blob storage as well as Postgres.

```sh
export RECEIPT_TEST_DATABASE_URL='postgres://receipt:password@localhost/receipt_test?sslmode=disable'
make integration
make race
```

Integration tests create/drop only uniquely named test schemas. Database tests
skip when the variable is absent; `make integration` requires it and disables
test-result caching. Read the nine-question Phase 2 review in the working doc.

## Shared storage maintenance

Stop all older server versions before upgrading. Every reader/writer/collector
must use the lifecycle protocol; direct blob writes or mixed-version servers can
break GC safety. This implementation supports private local Unix filesystems
with `flock` (tested on macOS), not arbitrary distributed filesystems or cloud
objects. Writers/maintenance serialize; concurrent readers share locks.

Postgres stores common limits: 1 GiB blob payloads (including orphan/pending
bytes), 64 MiB logical JSON metadata/evidence, 10,000 action rows and 100,000
distinct evidence rows. Repeated blob bytes are charged once; rejected uploads
return HTTP 507 and clients still return their test command's outcome. These
are not hard disk/RSS quotas: filesystem blocks/inodes, temporary duplication,
indexes, WAL, database bookkeeping and lock/identity files are outside the byte
accounting. Use filesystem/database resource controls as well.

```sh
# RECEIPT_DATABASE_URL selects your dedicated shared metadata database.
receipt shared stats --cache-dir /absolute/private/receipt-shared
receipt shared limits --cache-dir /absolute/private/receipt-shared --max-blob-bytes 2147483648
receipt shared limits --cache-dir /absolute/private/receipt-shared --max-blob-bytes 2147483648 --apply
receipt shared prune --cache-dir /absolute/private/receipt-shared --older-than 720h
receipt shared prune --cache-dir /absolute/private/receipt-shared --older-than 720h --apply
receipt shared gc --cache-dir /absolute/private/receipt-shared
receipt shared gc --cache-dir /absolute/private/receipt-shared --apply
```

Changes default to dry-run; `--apply` is required to update limits, expire
metadata or remove objects. Initial connection can still create/migrate schema
and policy/identity files. An explicit cache directory is required. Limit changes
below current usage are rejected. `--timeout` defaults to 5 minutes.

Pruning removes old **nonquarantined** server metadata and its evidence. It does
not enforce result freshness or delete client copies. Quarantine tombstones,
their evidence and referenced original logs are retained. Run GC afterward to
remove unreferenced blobs and stale known temporary files. GC checks all stored
references before deleting anything and refuses malformed metadata, unknown
paths and symlinks. Completed deletions are permanent; interrupted GC can be
retried, but does not roll filesystem deletions back. Back up before applying
retention or GC to important data.

A database store ID binds it to `.store-id` in the blob directory, preventing
GC through the wrong metadata store. `.lifecycle.lock` protects filesystem work
even on SQL cancellation/session loss. Never replace/delete these marker files
while running servers. Restore matching database and blob backups together at
the configured path. Binding failures require inspection, not automatic adoption
of an existing directory. A crash during first-time binding may require manual
recovery; unsupported filesystems/path moves are not automatically migrated.

At an evidence limit, an existing-key conflict still commits quarantine and sets
`evidence_capped`, without storing the new report. Stats expose capped entries.
At an action-row limit, a report for a previously unknown key can be rejected;
clients retain local quarantine but shared propagation has no durable retry
queue. No automatic quarantine deletion or periodic maintenance is scheduled.

The Phase 3 review is recorded in the HTML and phone-readable working documents.

## Development and phases

```sh
make lint-install  # checksum-verified, pinned project-local golangci-lint
make fmt           # apply gofumpt formatting
make lint          # formatting check and configured analyzers
make check         # lint plus unit tests
make test
make race
make vet
```

Linting uses golangci-lint v2.14.0 with govet, staticcheck, errcheck, goconst,
revive, depguard, ineffassign, unused, nolintlint, exhaustive, gocognit, nestif
and errorlint. In v2, gofumpt runs as
a formatter; `make lint` checks formatting without changing files. The tool
lives in ignored `.tools/bin/`; it does not change the project's Go version.
The installer needs network access on first use and supports macOS/Linux on
arm64/amd64. Re-run `make lint-install` to restore the pinned binary.

Depguard permits the standard library, Receipt packages and the pinned SQL
driver; fingerprint/runner packages may not import orchestration or storage.
Errcheck applies to production code, including blank assignments. Test fixtures
are currently exempt from errcheck (not other analyzers); some deliberately
write corrupt data. Narrow, explained suppressions cover best-effort resource
cleanup and concrete JSON types that cannot currently fail to marshal.
Diagnostic output is best-effort; command output and replayed test streams
propagate write failures. Linting is static evidence, not a substitute for
PostgreSQL integration, race tests or crash-recovery testing.

Implementation conventions are in [docs/CODING-PRACTICES.md](docs/CODING-PRACTICES.md),
required reading for code changes by AGENTS.md. Enum switches must be exhaustive;
cognitive complexity and nested-condition checks guide responsibility-based
refactoring, not removal of necessary error checks.

`make` selects the system linker and refreshes the local ad-hoc binary signature
on macOS because this machine's Go 1.22 internal linker triggered a `missing
LC_UUID` loader error. Since Go 1.27.2 became the default toolchain here
(15 October 2026), plain `go test ./...` also works. `go.mod` still declares
Go 1.22 and CI uses 1.22.12. None of this changes cache logic.

Implemented, by phase:

1. Local CLI, versioned fingerprints, success-only immutable entries, corruption
   checks, bounded output capture, bypass/refresh/verification, input diffs,
   cache listing/pruning, and the HTTP client/server with a shared token.
2. PostgreSQL metadata, content-addressed output files, shared quarantine,
   status checks before local replay, and aggregate counters.
3. Shared quotas, retention, orphan garbage collection and lifecycle locks.
4. Strict JSON contracts and tests of interrupted storage operations.
5. Scoped, expiring, revocable service credentials.
6. Opt-in timings, request logs and repeatable measurements (including CodeCraft).
7. Bounded `cargo test --frozen` support.

Deployed: one DigitalOcean droplet behind Caddy and HTTPS, used by Receipt's CI
(see [Current state](#current-state-15-october-2026)).

Next steps, none of them started:

1. Renew the CI token before 12 November 2026 03:28 UTC. Since 15 October 2026
   the client no longer hashes `RECEIPT_TOKEN` into keys, so renewal keeps them.
2. Scheduled backups with a second copy of the recovery key, and monitoring.
3. Cheaper upload accounting, so that an upload no longer scans the whole store.
4. Portable input policy, sandboxed execution, dependency selection, and object
   storage, each with explicit correctness gates.

The [design defense](docs/DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls)
lists every known gap with a proposed fix.

The living design and decision record is [the working guide](docs/WORKING-DOC.md);
[docs/DESIGN-DEFENSE.md](docs/DESIGN-DEFENSE.md) answers the questions a reviewer would ask. Each implemented phase includes a nine-question review: data structures, architecture, primitives, pitfalls, hot/cold paths, guarantees/non-goals, alternatives, evidence/risks and conditions for revisiting decisions.
