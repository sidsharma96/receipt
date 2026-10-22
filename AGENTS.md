# Receipt collaboration conventions

Preserve existing user changes. Keep implemented behavior distinct from proposed work.
Do not commit, push, deploy or publish without user authorization.

Before changing Go code, read `docs/CODING-PRACTICES.md`. Before changing prose,
read `docs/PROSE-STYLE-GUIDE.md` and apply its required review passes. After each
coherent revision, read changed pages in learning order without source code beside
them: can a newcomer explain the problem, follow the example and predict a result?
Repair gaps before continuing. Then verify claims against implementation and run
`make docs`. Report editorial findings separately from check results; neither a
passing build nor a shorter page establishes teaching quality.

## Documentation structure

The README serves someone deciding whether to install Receipt. Keep it focused on
purpose, installation, one working example, supported commands and important limits.
The book teaches current behavior through concrete examples. Reference pages hold
precise contracts and selected evidence. Operator procedures belong in the operator
guide; contributor policies belong in the contributor guide.

Do not append a chapter or review for every implementation step. Update the page
that teaches the affected mechanism. Merge repeated explanations and remove
obsolete sections. Git history preserves earlier versions; do not copy whole
historical logs into an archive merely to keep them in the book.

Remove date-based narration, implementation phases, diary entries and dated headings
from documentation. Keep version numbers and experimental conditions that affect
interpretation. Put identifiers inside descriptive evidence links rather than
printing full hashes. Exact identifiers belong in actionable commands or contracts
only when a reader must copy or compare them. Do not alter real resource paths or
external URLs just to hide a date embedded in an identifier.

For substantial implementation work, review structures, flow, concurrency, failures,
costs, guarantees, alternatives, evidence and revisit conditions. These are review
prompts, not a mandatory nine-part chapter or user-facing handoff. Record the useful
outcome in the relevant page and summarize verification in the handoff. Do not
require a dated updates entry or an ever-growing phase-review file.

## Reader and illustration requirements

Let the reader’s question determine the lesson, not a source file, recent change
or test report. Exercises must be answerable from concepts already introduced;
advanced reference questions need not appear in the chapter. A refactor alone does
not justify reorganizing a lesson. Apply these rules to existing and future prose,
diagrams, exercises and explanatory comments.

Write for a beginning computer science student with no assumed Go knowledge. Explain unfamiliar terms where
needed. Begin with one concrete sequence, then explain why it fits and compare a
plausible alternative. Use actual, attributed code excerpts when they teach the
mechanism. Keep test inventories and configuration trivia out of that sequence.

Show relationships and partial failures with purposeful diagrams beside the text.
Keep text equivalents, offline reading and phone-width layouts. Inspect rendered
pictures and interactions when they change. Link and test checks do not establish
teaching quality; report verification gaps honestly.

Never equate stable input keys with deterministic outputs, safe file publication
with system-wide lock freedom, or an interface with free future reuse.

## Book tooling

The mdBook source is `docs/`; `docs/SUMMARY.md` lists every book page. Its overview
is `docs/WORKING-DOC.md`. Keep book tooling in `tools/docs/` and configuration outside `docs/`, since
mdBook copies non-Markdown source assets into the generated site.

Questions live in `docs/questions/{newcomer,practitioner,expert}.md`. A
`<!-- check: guide/NN-....md -->` marker selects a chapter. Edit the bank and run
`go run ./tools/docs sync`. Edit SVG illustrations in `docs/assets/` directly
and keep their explanations in `docs/FIGURES.md` consistent. Preserve measured
values and evidence links; no generator now recalculates chart values.

Run `make docs` before committing documentation. It checks source and built links,
code anchors, attributed excerpts, question synchronization and SVG syntax.
Keep those checks meaningful when pages are merged; do not require historical
page, question or excerpt counts as a substitute for checking existing content.
CI builds the book and deploys it when its publication secrets are configured.

## Measurement artifacts and dependencies

Do not add JSON, JSONL or NDJSON files to source control, including raw benchmark
records. Keep generated data under ignored `.tools/` locally or upload it as a
CI artifact with an explicit retention period. `tools/artifacts/`
enforces this rule in ordinary CI, including files added with `git add -f`.
JSON remains Receipt's configuration, cache and wire format; do not remove or
change JSON serialization to satisfy a source-artifact policy.

Historical raw evidence remains linked from docs/PERFORMANCE.md. Completed
investigation and recovery-drill scripts are retired; Git history preserves their
source. Normal builds and docs checks must not require those tools, archived data
or network access to old measurements. Do not rename raw archives to bypass policy.

Receipt and maintained repository tools use Go. Do not commit Python source or
bytecode (.py, .pyw, .pyc, .pyo), embed shared Python in another file type, or hide
a workflow dependency in an ignored directory. Disposable personal Python may
live in ignored scripts/utils/. This policy does not remove Python workload
support or change Receipt's interpreter probes. Keep shell entrypoints and
YAML/TOML configuration when needed. Explain application versus development
dependencies in the README and Chapter 6 when changing them.

## Commit hygiene

Before committing, inspect the staged diff and run checks appropriate to the
change (`make docs` for documentation). Enable the versioned local hook once per
clone with `make hooks-install`; do not silently replace an existing custom hook
configuration. The hook checks the actual index using
`go run ./tools/artifacts --staged`, then checks staged whitespace.
CI repeats the index artifact check. Source files must be at most 1 MiB each,
regardless of extension; discuss intentional larger assets before changing this
rule. No forced additions, renamed archives or bypass flags as a workaround.

Keep future raw profiler captures and deployment-tool state out of commits;
`.gitignore` covers `.pprof`, `.trace`, `.pcap`, `.pcapng`, `.wrangler/` and
`.DS_Store` in addition to the JSON and build-output rules. Existing tracked
binary measurement evidence remains intentional historical documentation.
A local hook is advisory and requires installation in every clone; CI is the
shared check. Do not equate these checks with full tests or a server-side ban
on every possible generated-data format.

## Local utilities versus shared source

Use ignored `scripts/utils/` for disposable personal probes/helpers. The artifact
checker rejects staged paths inside it, including forced additions. A fresh
clone does not contain these local utilities; create the directory when needed.
Do not make application, docs, CI or deployment steps depend on its contents.
Promote a helper into a versioned, purpose-specific directory before making it a
shared dependency, using Go for maintained helpers. Keep required configuration
versioned. Optional execution alone is not evidence that source should be ignored.
Retire obsolete tools with their callers, tests and current instructions together.
