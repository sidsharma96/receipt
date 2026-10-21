# Learn how Receipt works

Run `receipt run -- go test ./...` twice. The first successful run can save a
result. On the second, Receipt checks the inputs again and may print the saved
output instead of starting Go's tests. This book explains each step and the
situations where reuse would be wrong.

For installation and a first command, start with the
[README](https://github.com/sidsharma96/receipt#install). For everyday options,
use [usage and configuration](USAGE.md).

## Follow one run

The first five chapters follow a local result into a shared cache: saving it,
sharing it, cleaning up files, checking incoming data and controlling access.
You need basic terminal familiarity; new storage and networking terms are
explained when they become relevant.

Start with [Remember a successful run](guide/01-remember-a-successful-run.md).
The [chapter overview](WORKING-DOC.md) describes the rest of the book.

A matching input record does not prove that a test would behave the same now.
For example, a test may depend on a website that changed. Keep that distinction
in mind: Receipt reuses a recorded observation, not a fresh execution.

## Look up a specific problem

- A run is slow: [measure its stages](MEASUREMENTS.md).
- A Rust command runs uncached: [check the supported options](CARGO.md).
- A caller cannot access the server: [check credentials and permissions](SERVICE-CREDENTIALS.md).
- You want to change the implementation: [find the responsible package](guide/06-find-the-code-and-defend-a-decision.md).
- You want to compare storage choices: [read the design reference](DESIGN-DEFENSE.md).

The question bank is optional practice. You can read every chapter without
completing exercises or consulting an implementation log.
