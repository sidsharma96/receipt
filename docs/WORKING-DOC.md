# Follow one test run

Updated 18 October 2026. This guide explains Phases 1–7 and the single-server HTTPS deployment. Git history records committed revisions. Chapter 9 follows the deployed connection and a one-off R2 recovery exercise. Chapter 11 introduces the [design defense](DESIGN-DEFENSE.md), which answers the questions an expert reviewer would ask. Monitoring remains deferred; no backup schedule is enabled.

You have a Python project and run its tests with `uv run pytest`. [Pytest](GLOSSARY.md#pytest) checks your code. [Uv](GLOSSARY.md#uv) starts it in the project's Python environment. Receipt sits around that command: it can remember a successful run and show its saved output when the recorded inputs match next time.

We will follow that run through seven changes to Receipt: saving it locally, sharing it, cleaning up storage, checking incoming data, limiting what callers may do, measuring where time goes, and checking a Rust project's dependencies. The examples use Alice and a continuous integration worker, called [CI](GLOSSARY.md#ci). CI is a program that runs tests automatically.

You do not need to understand database locks or Go channels before starting. Each chapter introduces a problem before the code that solves it. Diagrams show sequences, not measured timings. Examples of output and filenames are illustrative.

The main distinction to keep in mind is that showing a saved result does not run the tests again. Receipt cannot recreate a report file or repeat a network request just by printing old output.

## The chapters

Read the chapters in order to understand the program. Each ends with a few questions under "Check your understanding"; the answers stay hidden until you open them.

1. [Remember a successful run](guide/01-remember-a-successful-run.md): the manifest, the input key, and what a hit replays.
2. [Let another client reuse the result](guide/02-let-another-client-reuse-the-result.md): the shared server, blobs, and quarantine.
3. [Delete old data without breaking a reader](guide/03-delete-old-data-without-breaking-a-reader.md): cleanup, locks, and storage limits.
4. [Check data before accepting it](guide/04-check-data-before-accepting-it.md): strict JSON and value rules.
5. [Give callers different permissions](guide/05-give-callers-different-permissions.md): credentials, scopes, and revocation.
6. [Find the code and defend a decision](guide/06-find-the-code-and-defend-a-decision.md): the packages and how to explain a choice.
7. [Find where the time went](guide/07-find-where-the-time-went.md): request timings and local measurements.
8. [Check what a Rust test depends on](guide/08-check-what-a-rust-test-depends-on.md): the bounded Cargo adapter.
9. [Follow a request to the deployed server](guide/09-follow-a-request-to-the-deployed-server.md): DNS, HTTPS, Caddy, and recovery.
10. [Final walkthrough: miss, hit, conflict and recovery](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md): one command through four situations.
11. [Defending the design](guide/11-defending-the-design.md): how the design defense is organised.
12. [Make a cache hit cheaper](guide/12-profile-before-optimizing.md): measured CPU, memory, waiting and query plans; streaming file-input hashing is implemented, with other candidates still deferred.
13. [Hash executables and measure their probes](guide/13-hash-executables-and-measure-probes.md): executable streaming, fresh probes, measured substeps and compatibility with old cache entries.
14. [Reuse the Git root within one invocation](guide/14-reuse-the-git-root.md): remove one redundant Git process while preserving fresh file discovery and post-run validation.
15. [Measure toolchain probe overhead](guide/15-measure-toolchain-probe-overhead.md): separate process cost from local normalization, preserve keys, and distinguish local savings from whole-hit latency.

16. [Resolve the selected Git once](guide/16-resolve-selected-git.md): keep custom Git choices, resolve Apple’s launcher per invocation, and measure resolution cost with compatible keys.

To inspect setup commands, use the [README](https://github.com/sidsharma96/receipt/blob/main/README.md). To examine the full nine-question phase reviews, test records, and dated decisions, use the [review reference](PHASE-REVIEWS.md). The reference preserves earlier claims and their amendments. This guide describes current behaviour directly so you do not have to reconstruct it from history. Dated changes to the guide itself are in [Updates and documentation records](guide/updates.md).
