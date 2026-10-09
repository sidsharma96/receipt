# Receipt documentation style guide

Agreed 2026-10-08. Follow this guide when updating the living documents, writing phase reviews, or explaining implementation decisions in a handoff. Read it before drafting, not only before proofreading.

The reader is a beginning computer science student. They can follow a short code example, but may not know what a transaction, lock, hash, subprocess or cache means. The goal is that they can explain how Receipt works and why its choices were made. Recognizing our vocabulary is not enough.

This guide applies to teaching prose. It does not require deleting precise names from API references, code or agent-facing implementation rules.

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

Keep the nine phase-review questions, but answer each with connected paragraphs. Lists suit execution steps and short checks. Tables suit comparisons or exact mappings. Neither should replace the explanation.

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

If the answer is no, rewrite the explanation. Adding a glossary, passing a spelling check or shortening sentences is not sufficient.

Automated checks should verify links, excerpt accuracy, matching HTML/Markdown content and the nine-question structure. They cannot certify that a reader understands the prose. State that distinction in the handoff.

## 11. Publication and history

Keep both working documents in agreement. Use responsive HTML without dependencies that make offline reading fail. Code and wide tables may scroll within their own boxes; the whole page should not overflow on a phone.

Preserve dated decisions and evidence when revising a review. Use Git history for previous committed document versions; do not create duplicate full-file snapshots unless requested. If the user asks to remove existing snapshots, remove their links too. Record documentation verification separately from historical runtime test results. Do not rerun or claim to rerun implementation tests merely because prose changed.

Do not commit, push or publish unless the user authorizes it.

## Examples that informed the guide

Allen Downey's [dictionary chapter in Think Python](https://greenteapress.com/thinkpython2/html/thinkpython2012.html) introduces a concrete mapping, demonstrates it and then explains alternatives for a counting task.

Julia Evans's [Learning DNS in 10 years](https://jvns.ca/blog/2023/05/08/new-talk-learning-dns-in-10-years/) uses an unexpected result to motivate investigation and treats confusion as a normal part of learning.

These are references for teaching methods, not voices to imitate. Receipt's prose should remain direct, technically careful and specific to this code.
