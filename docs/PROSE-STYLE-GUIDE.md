# Receipt documentation style guide

Agreed 2026-10-08. Follow this guide when updating the living documents, writing phase reviews, or explaining implementation decisions in a handoff. Read it before drafting, not only before proofreading.

The reader is a beginning computer science student. They can follow a short code example, but may not know what a transaction, lock, hash, subprocess or cache means. The goal is that they can explain how Receipt works and why its choices were made. Recognizing our vocabulary is not enough.

This guide applies to teaching prose. It does not require deleting precise names from API references, code or agent-facing implementation rules.

## Reading structure, revised 2026-10-09

The working guide is an explanation for a learner, not a complete implementation
report. Follow one test run through bounded chapters. Keep setup instructions in
the README and task guides, exact contracts in reference documents, and dated
nine-question reviews with their evidence in PHASE-REVIEWS.md and PHASE-REVIEWS.html.
Link those records where the reader may want to inspect a claim.

Do not put every available fact in the main reading path. A fact belongs there
when the reader needs it to understand the current problem or consequence.
Exact token byte counts, timeout settings, test function names, runtime versions,
and fuzz execution totals usually belong in the reference. Important limitations
still belong beside the behavior they limit. Moving evidence out of the lesson
does not mean deleting it or strengthening a claim.

Introduce unfamiliar mechanisms one at a time. Before naming a lock, show the
reader and cleanup operation that need coordination. Before naming a transaction,
show which changes must succeed together. A sentence with several unexplained
mechanisms fails even if it is short and technically accurate.

Use figures for sequences or relationships that readers would otherwise have
to reconstruct. Explain what a figure represents, including whether it is a
possible execution rather than a measurement. A diagram is not decoration.
Keep a textual equivalent for phone reading and assistive technology.

Apply this to the phase reviews as well. Place each figure beside the decision
it explains and link the relevant implementation. Show concrete fields for a
data structure, separate the participants in a concurrency example, and mark
where a request stops in a failure sequence. Label hypothetical failures and
sample hashes as illustrative. Preserve the dated review around additions.
The review HTML and Markdown use matching `review-figure` comment markers so
each illustration and its text equivalent can be found together.

Each phase chapter ends with a prediction or explanation question. In HTML,
the answer may be collapsed; the main explanation must remain visible.
Exercises do not replace teaching or require the learner to discover missing
background alone.

Use one Markdown source for the current guide and generate its offline HTML.
Do not maintain two independent copies of the teaching prose. Historical reviews
remain dated records; the current guide states today's behavior directly.

This structure draws on [pstack technical writing](https://github.com/cursor/plugins/blob/main/pstack/skills/technical-writing/SKILL.md)
and [unslop](https://github.com/cursor/plugins/blob/main/pstack/skills/unslop/SKILL.md).
Separate explanation, tutorial, how-to, and reference material. Use direct verbs,
consistent names, and complete sentences. Remove filler and abstract slogans.
Do not apply a sentence-length rule mechanically when it damages an explanation.

## 1. Build an explanation, not a summary of conclusions

For each substantial concept, use this order:

1. Show a concrete situation or question.
2. Walk through what the program does, in execution order.
3. Name the concept after its purpose is clear.
4. Show a short, attributed code excerpt and explain the important lines.
5. Explain why this approach fits Receipt.
6. Compare a plausible alternative against the same example.
7. Explain the cost or limitation, then the evidence and reason to revisit it.

This is a writing sequence, not seven headings to paste into every paragraph. A simple concept may need only a few sentences. A subtle race may need a longer trace. Do not introduce every exception before explaining ordinary behaviour.

## 2. Use one running example

Prefer an actual Receipt situation: a user running pytest, two clients uploading the same result, or cleanup deleting output file X.

Keep the same names and objects through the explanation. Do not switch between abstract “resources,” “objects,” “entities” and “artifacts” when all four mean one file.

Label invented counts and timings as examples, not measurements. Distinguish illustrative code from actual source excerpts. Do not fabricate an implementation or earlier decision history to make a story smoother.

An analogy may help introduce a concept, but return to what the code actually does. Avoid extended metaphors that create extra vocabulary or hide differences.

## 3. Make the actor, action and consequence visible

Prefer “cleanup waits until the reader finishes” over “read-side lifecycle exclusion is maintained.”

Each sentence should make it possible to identify:

- Who is doing the work?
- What data or resource are they using?
- What changes, waits or fails?
- Why does that matter to the user?

Avoid noun piles: “shared storage lifecycle quota admission coordination” is not an explanation.

Useful technical terms are allowed. Define them where they first matter and then use them consistently. A glossary is a reminder, not a substitute for teaching the concept in the main text.

## 4. Explain code; do not use it as decoration

Introduce the question the excerpt answers. Include only the relevant lines, usually one small type or one operation. Give the repository file and function when helpful.

After the excerpt, explain what the variables represent and what changes when it runs. For a map, show an example key/value and a lookup. For a lock, explain which operation waits. For an error helper, explain what still runs after failure.

Do not expect a beginner to infer slice aliasing, an empty struct, a SQL lock function or a bit shift just because the snippet compiles.

Actual excerpts must match the source. If unrelated lines are omitted, use separate excerpts or explicitly say the excerpt is abridged. Pseudocode must be labelled illustrative.

## 5. Compare alternatives as solutions to a problem

Do not list six structures and say “more complex” for each.

For example, to decide whether output file X is still needed:

- A slice can hold referenced hashes, but finding X may scan the whole slice.
- A map lets the collector ask about X directly, at the cost of memory for the lookup structure.
- A stored reference count makes the query cheap, but every change must update that count correctly.

Explain when the alternative would become useful. Distinguish “not suitable” from “possible, but not worth the added work yet.” Mark new comparisons as retrospective unless the original decision record documents them.

## 6. Explain costs in work units before notation

Start with what is read, scanned, sorted, sent or kept in memory.

Prefer “a tiny upload can still inspect thousands of existing files” before “write admission is O(B + M).”

If using Big O, define the letters and connect the notation to the example. Separate an algorithmic estimate from a measured duration. Do not invent a latency number or imply that a faster lookup makes a whole request constant-time.

Explain waiting separately from computation. A short operation may take a long time if it waits behind cleanup.

Define hot/cold paths as frequently performed work versus occasional work. “Cold” does not mean cheap or harmless.

## 7. Put guarantees next to the situations they cover

State normal behaviour first. Then give a concrete failure that the design prevents and another it does not prevent.

Separate:

- Behaviour enforced by code.
- Assumptions about callers and the environment.
- What tests actually exercised.
- Proposed work and untested cases.

Do not replace all explanation with defensive disclaimers. But do not strengthen a claim merely to simplify it. In particular:

- The same recorded input key does not prove the same output.
- Publishing one file safely does not make a multi-step operation indivisible.
- A SQL rollback does not undo filesystem writes.
- An interface does not make a new backend free to implement.
- Passing tests does not establish correctness in every possible execution.

If a condition matters, explain why it matters: “every writer must use this lock; an older writer can create a file while cleanup is deciding what to delete.”

## 8. Organize for reading, not checklist completion

Keep the nine phase-review questions in the linked review reference, answered
with connected paragraphs. They are inspection questions, not chapter titles
for the main teaching sequence. Lists suit execution steps and short checks.
Tables suit comparisons or exact mappings. Neither should replace explanation.

Use informative headings. Introduce a new topic with its question, not a promotional claim. Keep paragraphs centred on one idea, but do not chop every sentence into a separate paragraph.

Avoid unexplained shorthand, vague slogans and self-congratulation. Examples to avoid in explanatory prose include “load-bearing,” “battle-tested,” “robust by design,” “free reuse,” and “hidden scaling feature.” Replace them with the specific behaviour or cost.

A readable document may be longer than an engineering summary. Extra words should supply reasoning, examples or useful distinctions—not repeat the same warning.

## 9. Before-and-after examples

### Costs

Before: “Shared writes take exclusive locks and perform storage accounting.”

After: “Before saving a new result, the server checks how much space is already used. Today it does that by inspecting existing files and database records. Even a small upload therefore has more work to do as the store grows. The server makes other writers wait during this check so two uploads cannot both claim the same remaining space.”

### Guarantees

Before: “Readers cannot race participating cleanup in the pre-open window.”

After: “A reader can find the database record before it has opened the output file. Without coordination, cleanup could delete that file in between. Receipt makes cleanup wait until the reader has finished reading the record and its files.”

### Alternative designs

Before: “Reference counts require crash-consistent lifecycle coordination.”

After: “We could store a number beside each output file: how many results still need it? A zero would mean the file can be deleted. But if saving a result succeeds and increasing the number fails, the number is wrong. We would need rules for failed updates and a way to check the counts before trusting them for deletion.”

These are examples of explanatory prose, not replacements for checking the actual implementation.

## 10. A review that can reject a draft

Before declaring an update beginner-friendly, pick at least one dense section from each phase and ask:

1. Could the reader describe the normal sequence without looking up five terms?
2. Are all actors and stored objects identifiable?
3. Does each code excerpt have an explanation after it?
4. Does the alternative solve the same problem as the chosen design?
5. Is the cost described as actual work or waiting?
6. Are limitations explained through situations rather than a list of labels?
7. Can an external reviewer find the implementing code and scoped evidence?
8. Does the chapter teach one problem without making the reader learn several unrelated mechanisms at once?
9. Can the reader skip detailed evidence and still understand the explanation and its important limits?
10. Does a needed sequence have a figure or an equally clear execution trace?

If the answer is no, rewrite the explanation. Adding a glossary, passing a spelling check or shortening sentences is not sufficient.

Automated checks should verify links, excerpt accuracy, matching HTML/Markdown content and the nine-question structure. They cannot certify that a reader understands the prose. State that distinction in the handoff.

## 11. Publication and history

### Illustrations must do explanatory work

Audit every phase, not only the paragraph the reader last complained about.
Check guarantees, pitfalls, and alternatives as well as architecture sections.
For each multi-step failure, name what remains in each store after the error
and what the next attempt does. A diagram elsewhere in the phase does not
cover a different failure sequence; place the relevant picture beside it or
link the exact existing figure when it explains the same mechanism.
Choose the visual form from the question: connected references for shared files,
parallel lanes for overlapping operations, branches for decisions, and a common
scale for quantity comparisons. A collection of prose cards is not a substitute.

Introduce each figure with something to notice: “follow both arrows to X” or
“watch which operation must wait.” Keep object names stable. Let the picture
show the relationship instead of requiring the caption to describe an invisible
one. Use step controls when execution order matters; keep the complete picture
and a text equivalent available without JavaScript. Avoid automatic animation
that makes the reader chase the explanation.

Check the rendered picture, not just its markup. Check arrows, label collisions,
reading order, contrast, keyboard controls, and narrow-screen behavior. Keep
unmeasured time out of proportional charts. Review code behavior separately from
presentation inspiration: another project's diagram is not evidence for ours.

The LearnHub [rate-limiting](https://learnhub-three-tan.vercel.app/systems/building-blocks/rate-limiting)
and [blob-storage](https://learnhub-three-tan.vercel.app/systems/building-blocks/blob-storage)
pages illustrate this approach through changing quantities and visible execution
sequences. Use these as teaching references, not content to copy.

The review illustrations are generated by `python3 docs/render_review_figures.py`.
It updates marked HTML figures and their Markdown images/text; it leaves the
surrounding historical prose alone for Phases 1–6. Phase 7's review prose is
also generated from PHASE-REVIEWS.md by that command: edit the Markdown, not
its HTML copy. Run with `--check` after regeneration.

Edit WORKING-DOC.md and regenerate WORKING-DOC.html with
`python3 docs/render_working_doc.py`. Check it with the same command plus
`--check`. Keep both review-reference formats in agreement. Use responsive HTML
without dependencies that make offline reading fail. Code and wide tables may
scroll within their own boxes; the whole page should not overflow on a phone.

Preserve dated decisions and evidence when revising a review. Use Git history for previous committed document versions; do not create duplicate full-file snapshots unless requested. If the user asks to remove existing snapshots, remove their links too. Record documentation verification separately from historical runtime test results. Do not rerun or claim to rerun implementation tests merely because prose changed.

Do not commit, push or publish unless the user authorizes it.

## Examples that informed the guide

Allen Downey's [dictionary chapter in Think Python](https://greenteapress.com/thinkpython2/html/thinkpython2012.html) introduces a concrete mapping, demonstrates it and then explains alternatives for a counting task.

Julia Evans's [Learning DNS in 10 years](https://jvns.ca/blog/2023/05/08/new-talk-learning-dns-in-10-years/) uses an unexpected result to motivate investigation and treats confusion as a normal part of learning.

These are references for teaching methods, not voices to imitate. Receipt's prose should remain direct, technically careful and specific to this code.
