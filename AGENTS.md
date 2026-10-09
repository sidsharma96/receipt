# Receipt collaboration conventions

Preserve existing user changes. Keep implementation status distinct from roadmap plans.

Before implementing or reviewing Go code, read `docs/CODING-PRACTICES.md`.
It records this repository's error handling, enum, abstraction, validation and
verification conventions. Follow its enforced rules; distinguish suggestions
and deferred work from implemented guarantees. Update it when an agreed coding
decision changes, rather than silently introducing a conflicting convention.

After every implemented phase:

1. Append a dated review to `docs/WORKING-DOC.html` and its phone-readable companion `docs/WORKING-DOC.md`.
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

Do not deploy, publish, or change external repositories without authorization.
