# Updates and documentation records

## 15 October 2026: known-gap fixes

Several [known gaps](../DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls) were
fixed in code, each with a regression test. Credential issuance now has its own
advisory lock key, so renewing a token no longer pauses cache traffic. The
client no longer hashes its own `RECEIPT_TOKEN` into cache keys or passes it to
tests, so renewing a token no longer empties CI's effective cache. That change
alters, once, the key of every run made with the token set. The
[review reference](../PHASE-REVIEWS.md#known-gap-fixes-15-october-2026) lists
each fix and its test.

## 15 October 2026: the documentation becomes a book

The docs are now an mdBook with a sidebar, search and next and previous links, built from the Markdown in `docs/` and published privately on Cloudflare Pages. Each chapter of this guide is its own page under `docs/guide/`. A new [Start here](../START-HERE.md) page gives reading paths, the [glossary](../GLOSSARY.md) defines each term with a link to where it is taught, and the [question bank](../questions/question-bank.md) has newcomer, practitioner and expert questions with hidden answers. Each chapter ends with three of them. The generated `WORKING-DOC.html`, the hand-maintained `PHASE-REVIEWS.html` and their renderers were retired; the documentation checks moved to `scripts/docs/` and now also check links and anchors in the built site. No program behaviour changed.

## 15 October 2026: design defense and corrected claims

Added chapter 11 and the [design defense](../DESIGN-DEFENSE.md). Code references
in this guide now link to exact lines on GitHub, and a documentation test
checks that each linked line range still contains the named symbol. Corrected
claims elsewhere: credential issuance shares the storage lock key rather than
using its own, commands read standard input from `/dev/null` rather than a
closed descriptor, and the CI token expires on 12 November 2026 at 03:28 UTC.
The [review reference](../PHASE-REVIEWS.md#corrections-15-october-2026) records
these corrections with dates. No program behaviour changed.

## 13 October 2026: final four-case walkthrough

Chapter 10 follows a miss, hit, verification conflict and backup recovery with
four offline diagrams, text equivalents and attributed code excerpts. This is
a teaching update; the recovery evidence comes from the completed drill, not
a new server operation. Earlier phase reviews remain historical records.

## 13 October 2026: deployment teaching review

Chapter 9 now follows a request through DNS, HTTPS, access checks and storage.
Three diagrams show the public/private boundary, actual execution versus replay,
and a save that fails between two stores. The dated
[nine-question review](../PHASE-REVIEWS.md#deployment-review-one-server-over-https--13-october-2026)
preserves the evidence and limits. This update changes documentation, not the
server or CI. Monitoring and automated backups are next.

## 9 October 2026: harden Cargo before remote deployment

The Phase 7 review now has a dated hardening amendment. It covers changed
registry bytes with an unchanged lockfile, refusal after a saved success, more
actionable diagnostics, and separate timings for tool checks, dependency queries
and extra source reads. Three additional Rust repositories were cloned for the
lab. Supported commands remain deliberately narrow; no build-script support
was added. The next proposed milestone is a single-server remote deployment,
then object storage—not another language adapter. Provisioning still requires
provider, account and budget decisions.

## 9 October 2026: Phase 7 bounded Cargo support

Cargo reuse now checks installed tools, the selected dependency graph, a frozen
lockfile and registry source bytes. Build scripts and other unsupported cases
run uncached. The [nine-question review](../PHASE-REVIEWS.md#phase-7-review-bounded-cargo-support)
records source/dependency/feature changes, refusal checks and the small real
regex experiment. The adapter does not normalize test output or change storage
formats. Existing Go and Python probes remain separate.

## 9 October 2026: Phase 6 diagnostics and local measurements

Implemented opt-in CLI timings and redacted request summaries, a fixed-size
per-operation recorder, and repeatable local/shared experiments. See the
[nine-question Phase 6 review](../PHASE-REVIEWS.md#phase-6-review-find-where-the-time-went)
and [raw samples](../measurements/phase-6-2026-10-09.json). Runner adapters, durable
caller history and production deployment remain separate future work.

## 9 October 2026: separate teaching from the review record

This revamp introduces a current-behavior reading path, five sequence figures, explained source excerpts, and questions with optional answers in the HTML reader. The existing phase reviews and evidence were moved to the linked reference without changing their recorded conclusions. No Go implementation changed for this revamp.

The writing approach follows [Receipt's prose guide](../PROSE-STYLE-GUIDE.md), the [technical-writing guide](https://github.com/cursor/plugins/blob/main/pstack/skills/technical-writing/SKILL.md), and [unslop](https://github.com/cursor/plugins/blob/main/pstack/skills/unslop/SKILL.md). Netscope's local field notes provided the chapter-and-example model. These sources guide presentation, not claims about Receipt's implementation.

## 9 October 2026: illustrations in the phase reviews

The [phase-review reference](../PHASE-REVIEWS.md) now includes ten illustrations:
manifest fields, changed input keys, shared storage, an interrupted save,
reader and cleanup coordination, upload validation, permission checks, and
revocation, upload inspection costs, and the collector's map of referenced
files. Each appears beside the explanation it supports and includes a
text version in the [Markdown reference](../PHASE-REVIEWS.md). The numbered events
show possible execution orders rather than measured timings.

## 9 October 2026: rebuild the review illustrations

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

## 9 October 2026: show what remains after failure

The review adds five illustrations beside previously unpictured examples:
temporary changes missed by two input scans, committing a conflict warning,
files left after a failed save, partial cleanup and retry, and failure to print
an already-created credential. Phase 4's guarantees section now shows the
filesystem and database outcomes explicitly and explains why a well-formed
result is not proof that tests ran. The review has seventeen figures; this
count is not a claim that every explanation needs or now has an illustration.

## 9 October 2026: try real upstream test suites

The [real-repository lab](../REAL-REPO-LAB.md) records initial tests in pytest,
Hugo, regex, Ghostty and Vue core. Each selected baseline passes. Only the
Python experiment currently demonstrates Receipt reuse: Go's probe includes
a changing temporary path, and the other three runners execute uncached.
A deliberately failing Python test also shows why a changed file must trigger
execution instead of replaying an earlier success. This is experimental evidence
and a list of next fixes, not a completed new runner-support phase.

## 9 October 2026: correct Go's changing temporary-path input

Hugo's selected tests can now be saved and replayed. Go invents a new temporary
directory for each probe; Receipt used to mistake that generated name for a
meaningful change. The correction replaces only the random suffix in the known
compiler mapping, keeping real compiler settings in the key. A deliberately
failing source change forced execution; restoring the source restored the hit.
The [dated correction review](../REAL-REPO-LAB.md#9-october-2026-go-scratch-path-correction)
explains the comparison, alternatives, evidence and remaining limits. This is
a small maintenance fix, not completed Cargo/Vitest/Zig support.

## 9 October 2026: prepare a first Fly deployment

The [deployment proposal](../FLY-DEPLOYMENT.md) adds templates for one Receipt
server and a separate, private PostgreSQL server, each with its own disk. Its
picture follows a result from the laptop to both stores. Existing Fly apps are
not part of this proposal and have not been changed.

The proposed planning budget is $20/month, pending San Jose pricing and account
allowance checks. Local configuration checks and Linux cross-compilation do not
establish a working cloud deployment. Container startup, Fly validation, live
reuse and backup recovery remain to be checked. This is preparation, not a
completed remote-deployment phase.

## 12 October 2026: initial private DigitalOcean setup

The single Ubuntu Droplet now has a separate administrator account, an SSH-only
firewall, and a localhost-only Receipt service backed by local PostgreSQL.
Without a domain, the first client connection will use an encrypted SSH tunnel.
The [setup record](../DIGITALOCEAN-SETUP.md) explains the connection and checks.
This is not a completed deployment phase: real workload reuse, backup recovery,
public HTTPS and CI validation remain outstanding.

## 12 October 2026: remote-cache experiment passed

A small Go test ran on the Mac and saved its result on the Droplet. A second
invocation with an empty local cache fetched that result without running the
test again. Changing the function from 42 to 43 forced a new execution and
returned the expected failure instead of yesterday's success. An external
execution counter distinguished replay from actual execution.

The [experiment record](../DIGITALOCEAN-SETUP.md#12-october-amendment-first-remote-cache-experiment-passed)
contains the three outcomes and limits. The short-lived credential was revoked
and the SSH tunnel closed. This validates one controlled laptop workflow, not
cross-job CI reuse or production readiness.

## 12 October 2026: update, reboot and recovery drill

The Droplet's pending Ubuntu updates were installed and it rebooted into the
new kernel. Receipt and PostgreSQL started automatically, and the saved result
remained readable. Before updating, we paused writes and copied both stores into
an encrypted backup on the Mac. Decrypting that actual copy and restoring into
a separate database/file view returned exactly the same saved-result bytes.

The [recovery record](../DIGITALOCEAN-SETUP.md#12-october-amendment-encrypted-backup-and-isolated-restore)
explains what was backed up, the isolated test, and the remaining limits. This
was one manual backup and a same-host isolated restore, not a scheduled backup
system or a full rebuild on a new machine. The recovery key still needs separate
secure custody. Public HTTPS and CI validation remain outstanding.

## 12 October 2026: two CI runners share a cached result

The first GitHub Actions job ran a small Go test and saved its result through
an SSH tunnel. The second job used a fresh runner and an empty local cache:
it fetched the saved result, and the test's execution marker stayed absent.
Changing the function from 42 to 43 made the test run and fail. Restoring 42
and closing the tunnel made it run locally and pass.

Follow the [CI experiment](../CI-EXPERIMENT.md) for the sequence, evidence and
access restrictions. CI uses its own restricted SSH account, not the owner's
administrator key. This is a controlled Linux fixture with matching paths,
toolchains and environment—not automatic reuse across arbitrary CI projects.
No public Receipt port or new VM was added.
Both CI runs passed. Afterwards the experiment credential was revoked, its SSH
key disabled and the GitHub secrets removed. The workflow remains as a tested
example; another run requires fresh access rather than the administrator key.

## 12 October 2026: Receipt's own CI suite

Receipt now has a workflow for its actual Go tests, alongside an independent
uncached job. The cached job skips the opt-in database tests; the independent
job runs against disposable PostgreSQL and enables Go's race detector. A saved
success therefore cannot be the only evidence that Receipt itself still works.

The [Receipt CI guide](../RECEIPT-CI.md) explains the two paths and the 30-day
credentials. Pull requests receive no private-cache credentials. The initial
live run passed both jobs. A same-commit rerun fetched the result on another
runner, while the independent PostgreSQL/race suite passed again. CodeCraft
has not been configured yet.

## 13 October 2026: HTTPS without an SSH tunnel

A client can now use `https://receipt.sidsharma.dev`. Its encrypted connection
ends at Caddy on the droplet; Caddy forwards the request to Receipt inside that
same machine. Receipt still checks the caller's credential before allowing
protected operations. PostgreSQL and Receipt's internal port remain private.

An authenticated request from the Mac fetched the saved result unchanged.
Missing, invalid and revoked credentials were rejected. The certificate and
normal DNS lookup passed verification. This checks reading and authentication,
not a fresh upload/replay sequence or performance under internet traffic.
At this initial HTTPS check, CI still used its SSH tunnel. The follow-up below
records its migration.

See the [HTTPS deployment record](../DIGITALOCEAN-SETUP.md#13-october-amendment-public-https-with-caddy)
for the connection diagram, configuration, checks, rollback and remaining work.

## 13 October 2026: CI connects directly over HTTPS

A fresh Go fixture uploaded a result and replayed it from a second empty local
cache. Its execution marker stayed at one; changing the source made it execute
again and fail. The temporary credential was revoked afterwards.

[PR #2](https://github.com/sidsharma96/receipt/pull/2) moved Receipt's CI to
HTTPS and was merged after fresh runners demonstrated a miss followed by a
remote hit. The independent PostgreSQL/race job passed both attempts and again
on main. The old CI tunnel key was disabled and its two GitHub secrets removed;
the application token and its November expiry remain unchanged.

Your administrator SSH access is unaffected. These CI changes were made in
`/tmp/receipt-https-ci-20261013`, preserving this checkout's uncommitted work.
See the [deployment record](../DIGITALOCEAN-SETUP.md#13-october-follow-up-fresh-https-upload-and-replay)
for the fixture evidence and the linked PR for the current CI guide.

## 13 October 2026: CI connects over HTTPS

Receipt's CI now connects to `https://receipt.sidsharma.dev` instead of creating
an SSH tunnel. Caddy accepts the encrypted connection and forwards the request
to Receipt on the same droplet. Receipt still checks the caller's token and
permissions; its internal port and PostgreSQL remain private.

A fresh local test uploaded a result, then replayed it from an empty local cache.
An execution marker stayed at one, showing that the second invocation did not
run the test. Changing the source made it run again and fail. Next, fresh GitHub
runners demonstrated a miss followed by a same-commit remote hit over HTTPS.
The independent PostgreSQL/race suite passed on both attempts.

The [CI guide](../RECEIPT-CI.md#13-october-moving-the-connection-to-https) records
the checks and limitations. This changes the connection, not the rules for
deciding whether a saved result can be reused. The existing token retains its
November expiry, and unavailable caching still falls back to local execution.

## 13 October 2026: one-off R2 recovery exercise

One encrypted backup was uploaded to the private `receipt-backups` bucket,
downloaded, decrypted on the Mac, and restored into isolated storage on the
droplet. The live database was not replaced. The temporary restored database and
decrypted files were removed afterwards. The encrypted archive remains in R2
and on the Mac; the private recovery key stayed on the Mac throughout.

The [dated deployment review amendment](../PHASE-REVIEWS.md#r2-drill--1-data-structures)
records the nine review questions. No nightly schedule or monitoring workflow
was enabled. This supersedes the earlier plan to do automated backups next.

## Maintaining these documents

The guide is part of the documentation book. Each chapter is its own Markdown file under `docs/guide/`, listed in `docs/SUMMARY.md`. Edit the chapter that teaches the changed behaviour, then append the dated nine-question review and its evidence to [the review reference](../PHASE-REVIEWS.md). Preserve historical amendments.

Build the book with `mdbook build docs` and run the documentation checks with `python3 -m unittest discover -s scripts/docs -p 'test_*.py'`. The checks confirm that code links still point at their symbols, that relative links and heading anchors resolve, and that question answers stay in sync with the chapters that quote them.
