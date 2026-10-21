# Write documentation people can use

A reader arriving at Receipt should be able to try a command before learning how
we developed it. A student reading the book should be able to follow one operation
without reconstructing a timeline from updates and amendments.

## Give each page one job

- **README:** explain the purpose, show installation and a first run, name the
  important limits, then link onward. Aim for roughly 600–900 words.
- **Teaching chapter:** follow one concrete situation and explain the mechanism.
- **How-to guide:** give the steps for a task, expected results and relevant failures.
- **Reference:** define exact behavior so a reader can look up a specific rule.
- **Operator or contributor guide:** keep maintenance and repository policy here.

Before adding a section, ask whether it belongs on the page. Updating a feature
usually means revising an existing explanation, not adding another chapter.
Remove obsolete claims instead of attaching a correction after them.

## Let the reader’s question set the structure

Write for a beginning computer science student with no assumed Go knowledge.
Organize a lesson around a question the reader can answer by its end. A source
file, function sequence, recent change or test report must not supply its outline.
A behavior-preserving refactor may need a corrected source link and no new lesson.

Use connected prose, stable names and concrete cause and effect. Vary sentence
length to fit the explanation. Avoid compressed notes, slogans and repeated
chapter templates. Brevity is useful only while the explanation remains complete.

## Teach the sequence

Start with an example: a reader finds a database record and then opens its output
file. Show the failure: cleanup deletes the file between those steps. Then explain
why the lock makes cleanup wait. Name the mechanism after its purpose is clear.

Use one example throughout a section. Define unfamiliar terms beside the behavior
that needs them. A glossary is a reminder, not a prerequisite. Prefer short code
excerpts that show the actual operation; name their source and explain the lines.
Explain new Go syntax beside the excerpt, including operators needed to read it.
A small illustrative example may precede production code when the latter introduces
too many ideas at once. Label it as illustrative and describe its expected result.
Put source links after the idea has given the reader a reason to inspect the code.
Label illustrative values as examples rather than presenting invented measurements.

Keep technical depth when it answers the reader's question. Removing jargon does
not mean removing the mechanism. Explain alternatives against the same example,
including their costs and the conditions that would make them useful.

## Examples of the standard

These are illustrative edits, not quotations from the implementation.

- Instead of “`Postgres.lifecycle` acquires advisory and filesystem locks,” start
  with “Cleanup must wait while an upload writes a file that no saved result yet
  refers to. Otherwise it could delete that file before the upload finishes.”
  Then explain which lock makes it wait and what happens if a connection fails.
- Instead of “Receipt safely publishes results,” write “Receipt finishes writing
  a temporary file before making it available under the cache key. Readers cannot
  open the incomplete file through that key.” Explain the remaining failure case
  beside this sequence; the word “safely” is not the explanation.
- Instead of opening a performance lesson with a list of profiles, ask “The tests
  did not run, so why did this command still take a second?” Separate reading
  inputs, waiting and replay before introducing the tools that measure them.

Exercises should ask readers to predict a result, explain a consequence or change
one part of a worked example. Supply the setup and teach the required concepts
before asking the question. Advanced reference questions may require more code
study; do not copy them into a beginner chapter merely to fill a difficulty quota.

## Cut the development diary

Documentation describes current behavior. Remove dates, phase numbers, amendment
narration, completed checklists and personal work-session details. Git history
preserves earlier versions. A version number may be needed to explain compatibility;
a machine, toolchain and sample count may be needed to interpret a benchmark.

Do not display full commit hashes, random result keys, archive byte counts or
machine-specific paths unless the reader needs to copy or compare them. Use a
descriptive evidence link. Keep exact resource URLs and operational identifiers
intact behind those links. Never rename a real resource just to remove a date.

Keep useful historical experiments as selected case studies with a clear question,
method, result and limitation. Do not create a second full copy of the old logs.
A benchmark should say whether it measures current behavior or an earlier algorithm.

## Keep limitations relevant

A matching key does not prove unchanged behavior if a test depends on a website.
That warning belongs beside reuse. A database rollback cannot undo a file write;
that belongs beside shared publication. State the consequence in the example.

Do not add a paragraph explaining what an unrelated cleanup or documentation edit
failed to prove about performance. Do not repeat the same safety disclaimer in
every section. Avoid phrases that defend the writing instead of explaining Receipt.

Keep checks and claims separate. A passing race test covers the executions tested;
it is not proof about every possible interleaving. Link evidence when useful,
without making the test inventory the teaching sequence.

## Make figures earn their space

Use arrows for order and relationships, before/after states for changes, and
visible waiting or failure points where they matter. Put a figure beside the
explanation it supports. Keep an equivalent text explanation and offline reading.
Manual step controls can help; automatic animation should not set the reading pace.

Inspect rendered figures for clipping, small labels and phone-width overflow.
Check keyboard access if controls change. Prose boxes are not an explanation just
because they are arranged as a diagram. The LearnHub examples of
[rate limiting](https://learnhub-three-tan.vercel.app/systems/building-blocks/rate-limiting)
and [blob storage](https://learnhub-three-tan.vercel.app/systems/building-blocks/blob-storage)
are visual references, not evidence about Receipt's implementation.

## Review each revision before continuing

After each coherent set of edits, perform these passes in order:

1. **Read without source code.** Read changed chapters in learning order, including
   the transition from the preceding chapter. Identify the reader’s starting
   question, the new idea and one consequence they can now predict. Check unfamiliar
   terms, syntax, diagram labels and exercise prerequisites. Repair missing steps
   and repetition before adding more material.
2. **Check the facts.** Compare claims and excerpts with current code and scoped
   evidence. Separate an illustrative example from an observed result or a guarantee.
   Compare plausible alternatives honestly; do not invent a history of deliberation.
3. **Check the delivered page.** Synchronize question-bank edits, run `make docs`
   and inspect changed figures or layouts at desktop and phone widths. If a repair
   changes the explanation, repeat the reading pass for the affected section.

These passes are required for prose changes, including edits to existing chapters,
diagrams, exercises and explanatory comments. For a small comment, review its
surrounding explanation; do not manufacture a chapter-sized report. Keep comments
brief: explain the reason, assumption or consequence and put longer lessons here.

Summarize meaningful editorial findings separately from automated check results in
the handoff. Do not append a review log to the book. A missing explanation is a
reason to revise, even if every automated check passes. Word counts and readability
scores are signals, not acceptance criteria.

## Review before calling a page finished

1. Can the reader tell what the page helps them do or understand immediately?
2. Does it begin with the task or example rather than our project history?
3. Can a beginner follow the normal sequence with terms explained in place?
4. Is each section necessary here, or does another page already explain it?
5. Do examples and code excerpts agree with current behavior?
6. Are important failure cases beside the mechanism they limit?
7. Are benchmarks scoped without drowning the result in identifiers and logs?
8. Are dates, phase narration, personal paths and unused full hashes absent?
9. Do links and figures help the reader continue, including on a phone?
10. Can we delete another paragraph without losing something the reader needs?

Use this checklist as an editorial judgment, not a requirement to paste ten answers
into the page. `make docs` checks links, excerpts, figures and questions. It cannot
certify that someone understands the explanation.
