# Receipt coding practices

Agreed 2026-10-08. This is an implementation reference, not a feature roadmap.
Read it alongside AGENTS.md. The working documents record decisions and evidence;
this file records how to apply those decisions in future code.

## Error handling

- Prefer ordinary early returns when the next operation depends on success.
  Opening, writing, syncing, closing and publishing are different obligations.
  Do not execute later steps using invalid resources merely to check once.
- Use `errors.Is` for sentinel errors and `errors.As` for typed errors, so error
  wrapping does not change behavior. Add useful context with `%w`; do not leak
  credentials or captured output into diagnostics.
- Use a short-lived sticky-error helper only for homogeneous operations.
  `internal/app/printer.go` remembers the first output error and skips later
  prints. Every caller must return/check its final error. It does not stop other
  computations, undo earlier writes, own cleanup, or support concurrent use.
- Do not add sticky error state to shared stores or long-lived services.
  A failed request must not poison unrelated requests.
- Use `errors.Join` for independent validation or independent cleanup outcomes.
  Keep validation order stable and include field names. Decode structure first,
  validate fields second, and check cross-field/stateful rules separately.
  `Limits.validate` checks all four negative budgets before database access.
- Do not place rollback, unlock, close, or temp-file cleanup inside a pipeline
  whose steps become no-ops after failure. Cleanup must still run.
- Preserve the distinction between required output and best-effort diagnostics/
  optional cache bookkeeping. Tests must cover the intended exit-status policy.
  Explicit cleanup suppressions currently preserve primary errors; they are not
  evidence that cleanup cannot fail.

## Must helpers and pipelines

- `regexp.MustCompile` is appropriate for programmer-owned constant patterns.
  Similar standard-library Must helpers may wrap embedded, fixed templates.
- User configuration, environment variables, database connectivity and external
  files return actionable errors, even if startup cannot proceed. Do not turn
  these expected operational failures into panics or hide them in init().
- There is no generic Must or closure-chain framework in Receipt.
  Prefer named operations with explicit inputs/results. Consider a local
  pipeline only for genuinely linear, uniform steps; retain context, step names,
  ownership and cleanup. Stopping a pipeline is not transactional rollback.

## Fixed choices and boundary parsing

- Use named string types and typed constants for fixed domain choices.
  Root commands, cache commands, shared maintenance modes and fingerprint input
  kinds now follow this convention. Use standard HTTP method/status constants.
- Validate untrusted command strings at the boundary. Typed constants alone do
  not form a closed set: Go still permits explicit casts of invalid strings.
- Exhaustive switches must list known values; a default branch must not conceal
  a newly added enum member. Keep rejection of unknown runtime input.
- `goconst` detects repeated literals, not domain meaning. Do not couple unrelated
  concepts or convert open-ended paths, arguments, hashes or labels into enums.
- InputKind retains the existing JSON strings. Manifest/Input decoding hooks now
  reject unknown kinds; Manifest.Validate also checks directly constructed values.
  Typed constants alone are still not validation. Never silently change
  fingerprint serialization or key version.

## Function design and abstractions

- Split large functions by responsibility, not arbitrary line counts or lint
  scores. The CLI separates lookup, replay, verification and publication;
  HTTP admission/authentication stays outside endpoint-specific operations.
- Keep dependencies and cancellation explicit. A runSession belongs to one CLI
  invocation; it is not shared mutable state or a generalized workflow engine.
- Introduce interfaces for real behavioral substitution or capability boundaries.
  Do not add a generic repository/service layer merely to shorten functions.
- Document resource ownership, lock requirements, publication order and failure
  behavior. A method signature is not a concurrency or durability guarantee.
- Store, BlobStore and ConflictStore are narrow contracts; ManagedBlobs is local
  filesystem-specific. Postgres currently discovers the maintenance capability
  at runtime. Making it explicit is deferred; cloud storage is not a free swap.

## Parsing and safety boundaries

- Prefer typed payloads and explicit semantic validation over arbitrary JSON maps.
  Bound bodies before decoding; verify checksums and key relationships afterward.
- Phase 4 (2026-10-08) implements typed decoding hooks for Config, Manifest,
  Input, Result, ConflictReport, BlobRef, blobRecord and the result envelope.
  internal/strictjson only checks declared object shape; each type owns its
  semantic rules. Check JSON-INPUT-CONTRACT.md before changing them.
- Reject unknown/duplicate/wrong-case names, missing required fields, disallowed
  nulls and wrong types. Reject null string-array members and non-string output
  encodings. Preserve documented null-list/output compatibility and optional
  configuration defaults. Decode into a temporary value and assign only on success;
  clone any existing slices before decoding optional settings into that temporary.
- Invoke semantic validation on direct storage writes too. Hooks do not run on
  Go struct literals. Structural validation cannot replace authorization,
  request byte limits, checksum/key checks or transaction invariants.
- JSONB normalizes duplicate names before reading; validate incoming JSON before
  storing it. Hooks cannot recover distinctions already erased by the database.
- Do not trim filenames or secrets indiscriminately. Empty and whitespace-only
  strings can have different meanings.
- Retain filesystem safety checks when extracting helpers. Enumeration must
  finish validation before deletion; cooperating lifecycle locks remain required.

## Service credentials and permissions (Phase 5, 2026-10-09)

- internal/auth owns token generation, explicit scopes and PostgreSQL credential
  lifecycle. Use crypto/rand, not math/rand, for secrets. Store only the SHA-256
  token digest; never add bearer tokens to metadata, metrics, URLs or diagnostics.
  Issuance output intentionally reveals a new token once; list only public metadata.
- HTTP authentication identifies a credential; authorization checks an explicit
  scope before decoding/writing storage. New scoped routes need an explicit mapping;
  do not silently dispatch an unmapped route to the legacy handler.
- Credential mode is opt-in with --postgres --credentials; it must never fall back
  to RECEIPT_TOKEN or accept an authentication backend error. Legacy mode remains
  a separate compatibility choice, not a scoped-permission deployment.
- Use current database expiry/revocation state on every request. A successful
  check is a snapshot, not cancellation of already admitted work. Any caching of
  permissions must introduce and test an explicit staleness policy.
- Do not equate operator-assigned subjects with verified people, scopes with tenant
  isolation, or request identity with a durable upload audit. All are distinct.
- Issuance and migration share a dedicated advisory lock; the 1024-row retained
  credential budget includes expired/revoked rows and is separate from cache quotas.
  Do not auto-purge records or add an administrative network scope without a decision.
- Authentication work has its own bounded admission and two-second context; storage
  admission remains separate. Cancellation must release slots. Public errors remain
  generic, and metrics use aggregate counters without caller labels.
- API keys must travel over HTTPS outside loopback, using the existing client rule
  and an operator-managed TLS proxy. These changes do not provision TLS or DB roles.

## Timing diagnostics (Phase 6, 2026-10-09)

- Use internal/observe's fixed stage vocabulary and per-operation context record.
  Do not introduce dynamic labels from paths, keys, callers or error strings.
- A timing completion must run on failures too. Keep timers outside existing
  cleanup obligations; measurement must not change locks, transactions or results.
- State whether a duration includes waiting or child spans. Do not add nested
  stage totals and call them end-to-end latency.
- Request IDs are generated by the server, not copied from arbitrary headers.
  Diagnostics are not durable attribution, and child stderr is not sanitized.
- Request-log writes are synchronous after handler return. Preserve the explicit
  slow-writer limitation until a bounded asynchronous policy is designed/tested.
- Measurements require workload, sample count, environment, failure counts and
  limitations. A test or small benchmark is not a production capacity claim.

## Go environment probe

Go probe correction (2026-10-09): `resolved-go-environment-v2` retains all
`go env -json` string fields and normalizes only a numeric `go-build` suffix in
GOGCCFLAGS's generated file/debug prefix-map, under the reported GOTMPDIR (or
the process temporary directory), targeting `/tmp/go-build`. Retain flags,
roots and destinations. Unknown forms stay unchanged; prefer a miss to erasing
an input. Do not apply this normalization to test output or other runners.
The manifest schema stays v1; the new probe name deliberately separates Go keys.

## Cargo adapter (Phase 7, 2026-10-09)

- Keep the command allowlist and refusal policy in `docs/CARGO.md` synchronized.
  Recognized Cargo refusals cannot fall through to generic reuse.
- Forward feature/manifest selection to frozen metadata; keep actual command
  execution unchanged. Never accept missing metadata as an empty dependency set.
- Follow root dev dependencies, not transitive packages' own dev dependencies.
  Validate unknown graph/target kinds and refuse build scripts/procedural macros.
- Hash the existing lockfile even when Git ignores it, resolved tool binaries,
  versions and reachable registry source bytes. Do not claim sysroot/system
  attestation, hermetic execution or complete discovery of ignored local inputs.
- Probe errors mean uncached execution, not successful validation. Keep probe
  stderr private; retain original command exit/output behaviour.
- Rust-backed app tests require `RECEIPT_TEST_CARGO=1` and installed tools.
  Ordinary unit tests must not download toolchains or registry dependencies.
- Registry-mutation tests require `RECEIPT_TEST_CARGO_REGISTRY` pointing at
  prepared data and must copy it before editing. Never mutate a user's registry
  sources. Clear only isolated fixture artifacts when checking Cargo recompilation.
- Cargo timing stages are nested in fingerprint time. Keep their fixed labels;
  never use crate names as timing labels. Refusal diagnostics may include a
  validated crate name, not package IDs, registry URLs or captured probe stderr.

## Enforced tooling

`make lint` uses pinned golangci-lint v2.14.0 and checks gofumpt formatting.
Enabled analyzers: govet, staticcheck, errcheck, goconst, revive, depguard,
ineffassign, unused, nolintlint, exhaustive, gocognit, nestif and errorlint.

- gocognit threshold: 30; nestif threshold: 5.
- exhaustive: a default does not satisfy coverage.
- errcheck checks blank assignments and type assertions in production.
  Tests currently have an explicit errcheck exemption; other analyzers still run.
- nolint comments require the analyzer name and an explanation. Never suppress
  a whole workflow simply to make complexity scores green.
- depguard keeps fingerprint/runner independent of app/cache and the SQL driver.
  Additional external dependencies require an explicit decision.

These thresholds are maintenance signals, not proofs of correctness or targets
to game. Revisit them with concrete false positives and readable alternatives.

## Verification and documentation

- Read and follow [the prose style guide](PROSE-STYLE-GUIDE.md) when writing
  explanations or phase reviews. Use its rejection checklist before claiming
  that a draft is beginner-friendly; automated checks cannot certify readability.
- Write phase explanations for a beginner computer science student. Define new
  terms, show concrete execution examples and attribute actual code excerpts.
  Explain which alternative structures could work, what they cost, and when to
  revisit the choice. Avoid vague architectural slogans or unexplained jargon.
- Preserve dated decisions and evidence when rewriting the guide. Use Git history
  for earlier committed versions rather than duplicate full-file snapshots unless
  requested. Clearly separate historical implementation evidence from checks
  performed for the rewrite.
- Run `make check`, `make race` and `make build` for cross-cutting refactors.
  Run PostgreSQL integration/race tests against an explicitly selected disposable
  database for shared-storage or HTTP error-classification changes.
- Add regression evidence for preserved wire/key encoding, wrapped errors,
  cancellation, quarantine and output failures where affected.
- Record skipped integration tests, measurement scope and remaining limits.
  Lint/race success does not establish crash safety, deterministic outputs,
  system-wide lock freedom, or unrestricted future reuse.
- Preserve user changes and historical phase reviews. Append amendments rather
  than replacing history. A quality refactor is not a new product phase.
- Do not commit, push, deploy or publish without the user's authorization.

References: [Errors are values](https://go.dev/blog/errors-are-values),
[errors.Join](https://pkg.go.dev/errors#Join),
[golangci-lint settings](https://golangci-lint.run/docs/linters/configuration/).
