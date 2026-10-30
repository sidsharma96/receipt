# Receipt collaboration conventions

Preserve existing user changes. Keep implemented behavior distinct from proposed work.
Do not commit, push, deploy or publish without user authorization.

## Independent code review

Use the local Claude Code `code-reviewer` for completed code changes. Also use
`architecture-reviewer` for changes to package responsibilities, storage or wire
contracts, concurrency, dependencies, or performance strategy. Review a substantial
design before implementation, then review the resulting code separately. Both roles
use `claude-opus-5-5` with low effort; do not silently substitute another model.

The roles live in `.claude/agents/`; their selected Go project references live in
`review/upstream.json`. These are private local configuration, excluded through
`.git/info/exclude`. Never stage or publish `.claude/` or `review/upstream.json`.
Do not change or stage `.gitignore` as part of this setup. A fresh clone needs these
local files provisioned separately; ordinary builds and tests do not require them.

Before review, stage only the intended source changes and set aside other nonignored
edits. Write a concise evidence file under `.tools/reviews/` describing the problem,
checks actually run, results, skipped checks and unresolved questions. Do not copy
private book sources or secrets into it. Run each required role with a new report
path for every round, for example:

```sh
go run ./tools/review run --reviewer code --base origin/main \
  --evidence .tools/reviews/evidence.txt --report .tools/reviews/code-1.json
go run ./tools/review run --reviewer architecture --base origin/main \
  --evidence .tools/reviews/evidence.txt --report .tools/reviews/architecture-1.json
```

The runner gives Claude a copy of the staged source, the diff and supplied evidence.
It excludes ignored files and private documentation, disables Claude customizations
and inherited MCP servers, restricts file access to that snapshot, and permits only
Read, Grep and Glob. Reviewers inspect code but cannot run checks themselves. The
upstream catalog supplies specific examples, not a mandate to copy large systems.

Resolve supported material findings or provide a concrete rebuttal, then obtain
APPROVE for the final code from each required role. After two unresolved substantive
exchanges, discuss the disagreement with the user; do not rerun merely to get a
favorable verdict. INSUFFICIENT_EVIDENCE needs the missing check or clarification.
Use `--purpose design` or `--purpose baseline` for those reviews; neither approves
a subsequent code change.

Immediately before an authorized commit or push, replace `run` with `verify` in
each command, using the same evidence and final report. The tree, comparison base,
evidence, role and catalog must still match. Refresh the target branch before a
merge and repeat review if the comparison changes. Local reports are workflow
checks, not tamper-proof attestations or GitHub branch protection. A review does
not grant permission to commit or publish, and book review remains separate.

Before changing Go code, read `docs/CODING-PRACTICES.md`. Before changing prose,
read `docs/PROSE-STYLE-GUIDE.md` and `docs/BOOK-RULES.md` (the rules every
learning book shares; they win for teaching chapters) and apply the required review passes. After each
coherent revision, read changed pages in learning order without source code beside
them: can a newcomer explain the problem, follow the example and predict a result?
Repair gaps before continuing. Then verify claims against implementation and run
`make docs`. Report editorial findings separately from check results; neither a
passing build nor a shorter page establishes teaching quality.

## Review decisions at coherent checkpoints

Use the independent architecture reviewer before adopting a substantial algorithm,
input assumption or cache boundary, and after an experiment before relying on its
conclusion. Review the implemented change separately with the required code and
architecture roles. A checkpoint is a decision or a completed experiment, not each
edit or command. Reopen review when new evidence changes an earlier assumption.

Supply the concrete workload, proposed behavior, simplest credible alternative,
required guarantees, evidence and unresolved questions. Ask the reviewer to challenge
the proposal itself: why this structure or algorithm, what work repeats, which path
actually dominates, what state must survive, and which failure could invalidate the
result? Include cold-start, miss and recovery costs, existing tool caches, input
scope and command semantics when relevant. Distinguish measured costs from suspected
hot paths. An optimization must improve the complete relevant workflow without
silently weakening its contract.

Keep a concise working decision note with the review evidence under `.tools/reviews/`:
the question, alternatives, evidence, counterexample, chosen trade-off, unresolved
limits and condition for reconsideration. Update the note rather than accumulating
a development diary. A reviewer verdict is criticism, not experimental proof.
Resolve material findings or record an evidence-backed rebuttal before depending on
the decision; missing evidence means an experiment or a narrower claim is needed.

For the book, teach the useful reasoning in the existing relevant lesson: one
concrete example, the mechanism, a credible alternative, evidence and its limit.
Review that explanation separately in the private documentation context. A code
review cannot establish that a newcomer can explain or challenge the design; do not
copy review transcripts, private book prose or a mandatory decision template into
public review evidence or teaching chapters.

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
from documentation. A mistake that git or a log records is not diary: tell it in the
chapter that teaches its mechanism (rule 3 of `docs/BOOK-RULES.md`). Keep version numbers and experimental conditions that affect
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

Run `make docs` before deploying documentation. It checks source and built links,
code anchors, attributed excerpts, question synchronization and SVG syntax.
Keep those checks meaningful when pages are merged; do not require historical
page, question or excerpt counts as a substitute for checking existing content.
The book is not built or deployed by CI; see "Documentation is private" below.

## Code excerpts in the book

Every guide chapter follows Rule 7 of `docs/BOOK-RULES.md`: show the real code the
chapter is about, give one concrete input before it, walk the lines that matter
after it, show what they produce, and go one level down or up. A reader with only
the book should understand 95% of that code. A link adds to code on the page; it
never replaces it.

- Receipt uses its own excerpt format, not the Rule 7 marker: a link line ending
  in a colon, a blank line, then the block.

      [`symbol`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L232-L260):

      ```go
      …exact lines…
      ```

- `make docs` requires this attribution on every ```` ```go ```` block in
  `docs/guide` and checks the block against that range of the current source,
  ignoring whitespace. It accepts no cut comments: the block must be one unbroken
  run of the range. To leave lines out, split the code into several excerpts.
- Show a walk-through once. When another chapter already walks the code in full,
  give one sentence and a link to its section.
- Quote recorded measurements only, never timings rerun on a busy machine.
- `make docs` is the excerpt check here. `../learning-docs/check-excerpts.py`
  finds no markers in this book, so it passes without checking anything.

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

## Documentation is private

`docs/` is ignored by Git in this repository and must never be committed or pushed: no `git add -f`, no docs workflow under `.github/`, and no code, test or CI step that reads `docs/`. The sources are backed up to the private `sidsharma96/learning-docs` repository, checked out beside this one at `../learning-docs`. Edit the docs here in `docs/`, never in `../learning-docs`, because the next deploy overwrites that copy.

To publish, run `../learning-docs/deploy.sh receipt`. It copies `docs/` into the backup repository and pushes it, runs this book's checks, builds the book and deploys it to the Cloudflare Pages project `receipt-docs`, served privately behind Cloudflare Access at `https://receipt-docs.sidsharma.dev`. Add `--no-deploy` to back up and check without publishing. `deploy.sh` runs `make docs` first. Links to the book from tracked files, such as the README, use the site address rather than a `docs/` path.
