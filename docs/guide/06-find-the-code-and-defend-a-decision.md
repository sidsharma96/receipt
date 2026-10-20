# 6. Find the code and defend a decision

The chapters explain behavior. To follow a change in the source, start with the part responsible for it. A Go package groups related source files. These packages run in one client or server process; a package is not a separate service.

## The main responsibilities

- internal/app reads the command and combines lookup, execution, verification, and publication. Start at [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L334).
- internal/fingerprint describes inputs and calculates their key. Start at [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L110-L153) and [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L65-L76).
- internal/runner starts the command and captures output. Start at [`Run`](https://github.com/sidsharma96/receipt/blob/main/internal/runner/process.go#L49-L87).
- internal/cache stores results. Start at the [`Store`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L40-L44) interface; its other files implement HTTP, shared storage, and maintenance.
- internal/strictjson checks the declared JSON structure in [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61). Each decoded type owns its value rules.
- internal/auth defines service credentials and permissions in [`Principal`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/auth.go#L51-L60). Its PostgreSQL implementation, starting at [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L51-L88), manages their lifetime.

These boundaries keep responsibilities identifiable. They do not make future work automatic. A cloud output store must still solve publication, deletion, and failure handling. Another project may need different permissions and account rules. Receipt's current internal packages are not a ready-made shared identity platform.

## What needs to be installed?

Suppose you copy the built Receipt executable to a Linux runner and run
`receipt run -- go test ./...` inside a Git repository. Receipt discovers the
repository with Git, records the selected Go toolchain and input files, looks
for a saved result, and starts `go test` only when it needs to execute. That
runner needs Git and the workload's Go tools. It does not need this repository's
Python profiling scripts. A Python/pytest workload needs its own Python
environment instead; Receipt even probes that environment on an ordinary hit.

The executable begins in [`main`](https://github.com/sidsharma96/receipt/blob/main/cmd/receipt/main.go#L9-L11),
which calls `os.Exit(app.Main(os.Args[1:], os.Stdout, os.Stderr))`. This passes
command arguments and the two output streams into the Go application. It does
not hand control to a Python application. To rebuild the executable, retain
`cmd/`, `internal/`, `go.mod` and `go.sum`; the module downloads the pinned
PostgreSQL driver even when you intend to use only local files.

A shared server has a different job. The deployed
[service configuration](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service)
starts `receipt serve`, which receives and saves results. It does not execute the
client's tests. Our deployed server needs PostgreSQL and writable output
storage, with Caddy providing HTTPS and systemd supervising the process. It does
not need pytest, Cargo, Go's compiler or the profiling scripts to serve requests.
Other server modes have different storage requirements; this describes the
current credential-enabled deployment.

| Repository part | Why we keep it | Needed by a built Receipt process? |
|---|---|---|
| `cmd/`, `internal/`, `go.mod`, `go.sum` | Build and test the Go program | Compiled into the executable; source files are not needed afterward |
| `docs/`, `scripts/docs/` | Write, build and check the book | No; book checks need Python 3.11+ and mdBook 0.5.4 |
| `deploy/ci/` | Run Receipt's CI cache helpers and policy tests | No; current CI uses these Python scripts |
| `scripts/profile/`, `scripts/measure_local.py` | Repeat measurements and draw experimental figures | No; optional profiling tools |
| `deploy/digitalocean/` | Configure deployment and perform operator checks/recovery | Selected configuration is used by deployment; Python scripts are operator tools |

To build the book, `make docs` first asks mdBook to turn Markdown and pictures
into a static website, then runs Python checks for links, excerpts, figures and
questions. The small exact values used by checked teaching figures live in
`scripts/docs/figure_measurements.py`. The build does not fetch old benchmark
records. Reading the finished site needs only a browser. Publishing through the current
Cloudflare workflow additionally uses Node.js 22, Wrangler 4 and the Cloudflare
account ID/API-token secrets; these tools do not run in Receipt.

Raw measurements are different from these required sources. On 20 October 2026
we removed 74 JSON evidence files from the current tree and linked readers to
the same files in Git history. New raw output stays in ignored `.tools/` or CI
artifacts, so it does not become part of every future source scan or book build.
Deleting it does not erase earlier Git objects or make old clones smaller.
[Archive instructions](../MEASUREMENTS.md#where-raw-measurements-live) explain how
to restore evidence locally for the optional checks. The
[cleanup review](../PHASE-REVIEWS.md#source-artifact-cleanup-20-october-2026)
records checks and limitations. We kept Python rather than rewriting these tools
in Go: it remains useful for docs and current CI, and replacing a script would
require checking the replacement's behavior too. Revisit that choice if we
retire those workflows or decide to require a Go-only development environment.

## Which helpers belong in Git?

Imagine a new contributor running `make docs`. Python discovers the docs tests,
and one of them opens `deploy/fly/api.toml` and `postgres.toml` to check the
proposal's templates. Those TOML files are not used by the deployed DigitalOcean
server, but removing them would still break that documented docs build. Likewise,
a manually started profiling workflow needs its Python entry point and helper
files in the runner's checkout. Optional execution does not make shared source
safe to ignore.

Keep `scripts/docs/`, CI helpers, the artifact checker, lint configuration and
`docs/book.toml` versioned. Keep shared profiling tools and deployment templates
while their workflows, documented experiments or recovery procedures remain.
The earlier remote-cache experiment and Fly proposal are optional features we
could retire; doing so requires updating their callers, checks and instructions
together. The audit found no current automated-test references to the tiny Python example
under `testdata/`, so it moved to ignored local storage at
`scripts/utils/examples/python-project/test_demo.py`. The [README inventory](https://github.com/sidsharma96/receipt/blob/main/README.md)
lists all Python groups and the YAML/TOML roles as audited on 20 October 2026.

For your own throwaway probe, create `scripts/utils/` and write it there. That
directory is ignored; the commit checker also rejects any of its staged files,
including a Python helper added with `git add -f`. A new clone will not contain
these utilities. If a helper becomes necessary for CI, the docs or another
contributor, move it into an appropriate versioned directory and review it as
shared source. Keep its generated records in `.tools/` or CI artifacts.

This choice affects source maintenance and repeatability. It does not remove
work from a running Receipt binary, which does not load these repository scripts.
We did not move existing shared scripts into ignored storage or silently disable
manual investigations. Retire a feature explicitly if maintaining it is no
longer worth its cost.

## Keep generated records out of commits

Suppose a benchmark writes a large `run.json`. Save it under `.tools/`, then put
the result you want readers to learn in the chapter. Git ignores that local
output. If someone force-adds the raw file anyway, a local commit check rejects
it. Enable that check once after cloning with `make hooks-install`.

A staged file is Git's saved copy of what will go into the next commit. Editing
the working copy afterward does not change that staged copy. Our hook runs
`python3 scripts/check_source_artifacts.py --staged` to inspect those staged
contents, rejecting JSON/JSONL/NDJSON and any individual source file over 1 MiB.
It also checks staged whitespace. CI repeats the artifact rule after checkout,
so skipping the local hook does not produce a passing CI result. Existing small
binary profiles remain historical evidence; future captures belong in ignored
storage or CI artifacts.

Before committing, inspect `git diff --cached --stat` and `git diff --cached`,
then run the checks relevant to the change. Docs changes require `make docs`.
The lightweight hook does not run application tests. Each clone needs the setup
command, and the rules do not recognize every possible generated file. If a
legitimate teaching image grows beyond 1 MiB, first try reducing it; discuss an
explicit policy change if that is insufficient. Do not rename a raw archive or
force-add it as an alternative.

## Frequently performed work versus occasional work

People call frequently performed work the [hot path](../GLOSSARY.md#hot-path). In Receipt, input scanning and result lookup happen on normal commands. In credential mode, each server request also checks its credential. A cache hit avoids pytest, not all computation or disk access.

Occasional work is called the [cold path](../GLOSSARY.md#cold-path). Issuing credentials and collecting unused files fit here. Occasional does not mean cheap: cleanup can inspect the whole store and make other operations wait.

We have tested selected behaviors and failures, but have not established production latency or capacity. The [verification records](../PHASE-REVIEWS.md#verification-implementation-evidence-versus-documentation-checks) and each phase's evidence section say what actually ran. Their test names and execution counts are available for inspection rather than part of the introductory lesson.

## How to explain a choice to someone else

Start with the problem. Then describe the chosen behavior and one reasonable alternative. Finish with its cost, what we checked, and what would make us change our mind.

For cleanup, that becomes: a reader needs its output file to remain available; locks make cleanup wait; delaying deletion alone cannot establish that a paused reader has finished; the chosen lock can make other work wait; selected concurrency tests check this sequence. If measured waiting becomes unacceptable, we need a different protocol, not just a smaller timeout.

For permissions, it becomes: a stolen secret must stop working on later checks; fresh database lookups support that behavior; cached permissions can remain stale; queries cost work and depend on the database; selected tests cover denial and revocation. If queries become a measured bottleneck, any cache must state how long revoked access can remain accepted.

The full [nine-question reviews](../PHASE-REVIEWS.md) cover structures, architecture, coordination, pitfalls, costs, guarantees, alternatives, evidence, and reasons to revisit choices. Read them when you want to inspect a particular decision in depth.


## Check your understanding

<!-- questions:start -->

### Which package does what?

**Level:** Newcomer

You want to change how Receipt describes inputs. Which Go package do you open, and which packages hold the run flow, process execution and storage?

<details>
<summary>Answer</summary>

`internal/fingerprint` describes the inputs and calculates the key, starting at `Build` and `Manifest.Key`. `internal/app` reads the command line and combines lookup, execution, verification and publication, starting at `run`. `internal/runner` starts the test command and captures its output. `internal/cache` stores results locally, over HTTP and in shared storage. `internal/strictjson` checks JSON structure, and `internal/auth` defines credentials and permissions. All of these packages run inside one client or server process; a package is not a separate service.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [`Build`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L110-L153)

</details>

### The order of a run, from lookup to prune

**Level:** Practitioner

Walk through `receipt run -- uv run pytest` on a miss that succeeds, with a remote cache configured. In what order do lookup, execution, local storage, upload and pruning happen?

<details>
<summary>Answer</summary>

`run` parses flags and refuses more than one of `--bypass`, `--refresh` and `--verify`. It loads `.receipt.json`, resolves the cache directory and builds the manifest; if that fails, it runs the command uncached. `runSession.lookup` treats a locally quarantined key as a miss, then reads the local entry. With no local entry it asks the server with a GET, and a remote hit is stored locally and replayed.

On a miss, Receipt runs the command, streaming output while capturing a bounded copy. It builds the manifest again and calls the run stable only if that succeeds with the same key. It records the history file used by `receipt explain`. A successful, stable run goes to `runSession.publish`, which stores the result locally, uploads it to the server, and only then prunes the local cache. A failed upload is reported on stderr but does not change the exit code.

**Learn more:** [The main responsibilities](../guide/06-find-the-code-and-defend-a-decision.md#the-main-responsibilities) · [D5. Why scan the inputs twice?](../DESIGN-DEFENSE.md#d5-why-scan-the-inputs-twice-and-what-can-the-two-scans-miss) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L334) · [`runSession.publish`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L216-L245)

</details>

### What would you change first?

**Level:** Expert

You may make three changes to Receipt before a second team starts using the shared cache. Which would you pick, and what does each cost?

<details>
<summary>Answer</summary>

A defensible choice, ordered by how soon it hurts:

1. Record a last-used time and expire by it instead of by `created_at`. Today a result used every day still expires 30 days after it was first stored, so the busiest results are deleted on schedule. The cost is a write on the read path, done outside the read lock or in batches, and a new crash argument for that timestamp.
2. Size the database pool from `--max-inflight`, or reject values above 16. Today the pool silently caps storage concurrency at 16 connections while the flag accepts 128, and authentication competes for the same connections. The cost is one more limit to configure and explain.
3. Add a scheduled, tested backup with a second copy of the recovery key. Today the only off-machine copy is a one-off archive from 13 October, and only one private key on one Mac can decrypt it. The cost is an operating task to schedule, monitor and rehearse.

(Keeping `RECEIPT_TOKEN` out of keys, giving credential issuance its own lock key and replacing per-upload scans with running totals were on this list until they were done on 15 October 2026.) Close runners-up are an administrator command to clear quarantine with a recorded reason and a purge command for expired credential rows before the 1,024 cap. Any answer that names the invariant each change must preserve, and its cost, is a good answer.

**Learn more:** [Part 4. Known gaps and pitfalls](../DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls) · [D15](../DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow) · [G21. Operations](../DESIGN-DEFENSE.md#g21-operations)

</details>

<!-- questions:end -->
