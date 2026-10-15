# Start here

Receipt is a command-line tool that sits in front of a test command such as `uv run pytest` or `go test ./...`. Before the tests run, it records what they depend on: the repository's files, the environment variables, the test tool's version and installed packages. It turns that record into a key. If a saved result for the same key exists and passed, Receipt prints the saved output and exit code instead of running the tests again. If not, it runs the tests and, when they pass, saves the result for next time.

Receipt can also run as a small server, so that a laptop and a CI worker can share saved results. The server keeps its records in PostgreSQL and the output bytes in files, checks each caller's credential, and refuses to reuse a key once two runs report different output for it. One such server runs on a DigitalOcean droplet and serves Receipt's own CI.

Receipt is a learning project written in Go. It is deliberately small: one server, a global lock for writes, and a narrow list of supported test commands. The documentation explains each of those choices, what it costs, and what would make us change it.

## Who this book is for

- A student or newcomer who wants to see how a cache, a lock or a database transaction is used in a real program.
- An engineer who wants to know how Receipt works and whether its guarantees hold.
- The project's owner, preparing to defend each design decision in front of an expert reviewer.

## What you need to know first

You need to be able to read a short Go function and to run commands in a terminal. You do not need to know about databases, file locks, hashing or HTTP. The working guide introduces each of those where it first matters, and the [glossary](GLOSSARY.md) gives a one-line reminder of every term with a link back to where it is taught.

Two facts make the rest of the book easier to follow:

- A saved result is replayed, not re-run. Receipt prints the bytes a past run printed; the tests do not execute, write files or contact the network.
- A matching key means the recorded inputs match. It does not prove the tests would behave the same today, because a test can depend on something Receipt does not record, such as the time or a website.

## Reading paths

Each path is an ordered list. Every chapter ends with a few questions under "Check your understanding"; open an answer only after you have tried the question.

### New to test caching

Read the working guide in order. It follows one test run from a laptop to a shared server.

1. [Follow one test run](WORKING-DOC.md)
2. [Remember a successful run](guide/01-remember-a-successful-run.md)
3. [Let another client reuse the result](guide/02-let-another-client-reuse-the-result.md)
4. [Delete old data without breaking a reader](guide/03-delete-old-data-without-breaking-a-reader.md)
5. [Check data before accepting it](guide/04-check-data-before-accepting-it.md)
6. [Give callers different permissions](guide/05-give-callers-different-permissions.md)
7. [Find the code and defend a decision](guide/06-find-the-code-and-defend-a-decision.md)
8. [Follow a request to the deployed server](guide/09-follow-a-request-to-the-deployed-server.md)
9. [Final walkthrough: miss, hit, conflict and recovery](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md)
10. [Newcomer questions](questions/newcomer.md)

Chapters 7 and 8 (timings and Rust support) can wait until the rest makes sense.

### Experienced engineer

Skim the guide's examples, then go to the decisions and the code.

1. [Final walkthrough: miss, hit, conflict and recovery](guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md)
2. [Find the code and defend a decision](guide/06-find-the-code-and-defend-a-decision.md)
3. [The decision tree in the design defense](DESIGN-DEFENSE.md#part-2-the-decision-tree)
4. [The JSON input contract](JSON-INPUT-CONTRACT.md) and the [Cargo command contract](CARGO.md)
5. [Threat and failure model](DESIGN-DEFENSE.md#part-3-threat-and-failure-model)
6. [Known gaps and pitfalls](DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls)
7. [Practitioner questions](questions/practitioner.md)

### Preparing to defend the design

Work through the design defense in order, then test yourself without notes.

1. [Defending the design](guide/11-defending-the-design.md), for how the defense is organised
2. [Foundations from first principles](DESIGN-DEFENSE.md#part-1-foundations-from-first-principles)
3. [The decision tree](DESIGN-DEFENSE.md#part-2-the-decision-tree)
4. [Threat and failure model](DESIGN-DEFENSE.md#part-3-threat-and-failure-model)
5. [Known gaps and pitfalls](DESIGN-DEFENSE.md#part-4-known-gaps-and-pitfalls)
6. [The DigitalOcean deployment record](DIGITALOCEAN-SETUP.md) and [Receipt's own CI](RECEIPT-CI.md)
7. [Practitioner questions](questions/practitioner.md), then [expert questions](questions/expert.md)

## How the book is organised

- **Follow one test run** is the working guide: current behaviour, taught through one example.
- **Defending the design** answers the questions an expert reviewer would ask, in the order they would ask them.
- **Questions** is a bank of questions at three levels, each answer linked to the section that teaches it.
- **Reference** holds exact contracts, measurements, coding conventions and the dated phase reviews.
- **Appendices** hold the deployment and CI records and the experiments behind them.

The source code is on [GitHub](https://github.com/sidsharma96/receipt). Code links in this book point at exact lines on `main`, and a documentation check fails if a linked range stops containing the symbol it names.
