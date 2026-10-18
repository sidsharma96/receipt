# Receipt collaboration conventions

Preserve existing user changes. Keep implementation status distinct from roadmap plans.

Before writing or revising living documents, phase reviews or explanatory
handoffs, read docs/PROSE-STYLE-GUIDE.md and apply its draft-review checklist.
Teach the execution sequence through concrete examples before summarizing
guarantees or alternatives. A glossary alone does not make a review readable.

Before implementing or reviewing Go code, read `docs/CODING-PRACTICES.md`.
It records this repository's error handling, enum, abstraction, validation and
verification conventions. Follow its enforced rules; distinguish suggestions
and deferred work from implemented guarantees. Update it when an agreed coding
decision changes, rather than silently introducing a conflicting convention.

After every implemented phase:

1. Update the relevant chapter under `docs/guide/` and append a dated entry to `docs/guide/updates.md` that links the full review in `docs/PHASE-REVIEWS.md`.
2. Answer the same nine questions in the user-facing handoff, with actual, attributed code snippets where applicable:
   1. What data structures are used, and why?
   2. What is the architecture/flow, why, and what are the extension boundaries?
   3. What concurrency, networking and fundamental primitives are used, and why?
   4. What are the pitfalls and debugging challenges?
   5. What are the hot/cold paths and their costs?
   6. What are the guarantees, assumptions and non-goals?
   7. What alternatives were considered, and why were they rejected or deferred?
   8. What evidence supports the claims, what failure modes remain untested, and what risks remain?
   9. What conditions would make us revisit each major decision?
   If alternatives were not previously recorded, label the comparison retrospective rather than inventing decision history. Connect substantial claims to implementing code, evidence and limitations.
3. Record verification performed, measurement scope, remaining limitations and decisions. Never equate stable input keys with deterministic outputs, safe filesystem publication with system-wide lock freedom, or an interface with free future reuse.
4. Explain new concepts for a learner with no assumed prior knowledge. Keep old phase reviews as historical records and append amendments if their conclusions change.
   Define unfamiliar terms, use a concrete example before abstract explanations,
   and compare plausible alternatives with their costs and revisit conditions.
   Avoid vague architectural slogans. Preserve dated decisions and evidence
   during readability rewrites. Use Git history for earlier committed document
   versions; do not create duplicate full-file snapshots unless requested.
   Remove existing snapshots only at the user's explicit request and update links.

Do not deploy, publish, or change external repositories without authorization.

## Persistent reader preferences (agreed 2026-10-09)

These are repository instructions for future agents, not optional polish:

- Write for a beginning computer science student. Use simple, precise language,
  explain unfamiliar terms where they arise, and teach one mechanism at a time.
- Start with a concrete example and execution sequence. Explain why the choice
  fits, what alternatives could work, and what would make us change it.
- Use actual, attributed code excerpts and explain them. Keep test inventories,
  configuration minutiae and verification logs out of the main teaching flow;
  preserve them in the linked reference without hiding important limitations.
- Audit sections, not diagram counts. Include guarantees, failure cases, costs
  and alternatives, not just each phase's architecture. Illustrate relationships
  or changing state that a reader would otherwise have to reconstruct mentally.
- Put illustrations beside the relevant explanation. Show actors, arrows,
  before/after states, waiting and failure points—not prose boxes as decoration.
  For partial failures, show what remains in each store and what a retry does.
- Prefer purposeful SVG/HTML diagrams and manual step controls when helpful.
  Keep text equivalents and phone-readable Markdown; preserve offline reading.
- Follow docs/PROSE-STYLE-GUIDE.md, including its LearnHub visual references.
  Inspect rendered pictures and check interactions/layout when possible. State
  verification gaps honestly; passing tests is not proof of teaching quality.

Keep these preferences in version-controlled files. Do not rely on chat history
or an assumed personal memory to carry them into another session.

## Living-document structure, revised 2026-10-09

The main working document is the current beginner reading path. Update its
relevant teaching chapter and append a dated phase entry that links the full
nine-question review. Keep the complete dated review and verification record in
docs/PHASE-REVIEWS.md. This preserves the review requirements above without
making test inventories the teaching sequence.

## Documentation book, revised 2026-10-15

The docs are an mdBook (0.5.4) whose source is `docs/` itself; `docs/SUMMARY.md`
orders it. The working guide's overview is docs/WORKING-DOC.md and each chapter
is a file in docs/guide/. Every new page must be listed in SUMMARY.md.

mdBook copies every non-Markdown file under docs/ into the published site. Keep
scripts, tests, configuration and anything secret out of docs/: documentation
tooling lives in scripts/docs/.

Questions live in docs/questions/{newcomer,practitioner,expert}.md in the
format the checker enforces. A `<!-- check: guide/NN-....md -->` marker under a
question copies it into that chapter's "Check your understanding" block; edit
the bank, then run `python3 scripts/docs/sync_questions.py`. Phase-review
figures are drawn by `python3 scripts/docs/review_figures.py`.

Run `make docs` before committing documentation. It builds docs/book/ and runs
the checks in scripts/docs/: code links name symbols within their line anchors,
relative links and heading anchors resolve in the source and in the built site,
attributed Go excerpts match the source, and questions are well formed and in
sync. CI (.github/workflows/docs.yml) runs the same target and deploys the book
to Cloudflare Pages when its secrets are present.

## Optimization chapters, agreed 2026-10-18

After each implemented optimization, add a new numbered teaching chapter under
`docs/guide/` and list it in `docs/SUMMARY.md` and the working-guide overview.
Explain the measured bottleneck, chosen change, compatibility evidence and
before/after costs. Keep prior measurements and dated reviews as history; update
older chapters with current-status notes and links rather than making readers
infer which optimization is now implemented. Append the full nine-question
review and dated updates entry as usual.
