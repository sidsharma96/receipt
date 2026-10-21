# Chapter overview

The book follows what Receipt does, rather than the order in which it was built.
Read the first five chapters together; choose the later topics as you need them.

1. [Remember a successful run](guide/01-remember-a-successful-run.md): what must match before output can be reused.
2. [Share a result](guide/02-let-another-client-reuse-the-result.md): files, database records and conflicting results.
3. [Delete old data safely](guide/03-delete-old-data-without-breaking-a-reader.md): why cleanup waits for readers and writers.
4. [Check incoming data](guide/04-check-data-before-accepting-it.md): reject malformed records before saving them.
5. [Give callers different permissions](guide/05-give-callers-different-permissions.md): issue, check and revoke credentials.
6. [Find the code](guide/06-find-the-code-and-defend-a-decision.md): follow a command through the Go packages.
7. [Find where the time went](guide/07-find-where-the-time-went.md): separate computation from waiting.
8. [Understand Rust inputs](guide/08-check-what-a-rust-test-depends-on.md): why some Cargo commands run uncached.
9. [Follow a network request](guide/09-follow-a-request-to-the-deployed-server.md): HTTPS, the server and its database.
10. [Trace success and failure](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md): connect the mechanisms in one example.
11. [Understand storage tradeoffs](guide/11-defending-the-design.md): compare designs against the same failure.
12. [Understand cache overhead](guide/12-profile-before-optimizing.md): input scanning, memory and network costs.

Use the [design reference](DESIGN-DEFENSE.md) for precise behavior and alternatives,
and [selected measurements](PERFORMANCE.md) for the evidence behind performance
examples. Neither is required reading before trying Receipt.
