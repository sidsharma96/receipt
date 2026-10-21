# Receipt

Receipt saves successful test results so you can reuse them when the recorded
inputs have not changed. It runs in front of commands such as `pytest` and
`go test`.

Run a test suite once to save its result. Run the same command again, with the
same recorded inputs, and Receipt prints the saved output instead of running
the tests. A changed input causes the tests to run again.

Receipt works locally without a database or cloud account. Teams can optionally
share results through a server. It is an early release: use it for tests whose
inputs you understand, and keep an uncached CI job for independent verification.

## Install

With Go installed:

```sh
go install github.com/sidsharma96/receipt/cmd/receipt@latest
```

Add Go's binary directory to your `PATH` if needed, then run `receipt help`.
For a fixed version, replace `@latest` with `@v0.1.0`. Source builds require Go
1.22 or later. On recent macOS, use Go 1.27.3 or newer; an older toolchain
produced a loader error in our installation check.

Without Go, [download a release](https://github.com/sidsharma96/receipt/releases/latest).
Choose `darwin` for macOS or `linux` for Linux; `arm64` for Apple Silicon or
ARM64 Linux, and `amd64` for Intel/AMD 64-bit. Extract `receipt` and put it in a
folder on your `PATH`. The release includes checksums for verifying downloads.
Windows binaries and Apple notarization are not provided.

You need Git and the tools for your test command. The downloaded executable
needs no Go compiler, Python installation or PostgreSQL database of its own.

## Try it

Inside your workload's Git repository, with its dependencies already installed:

```sh
receipt run -- go test -count=1 ./...
receipt run -- go test -count=1 ./...
```

The first successful run saves a result. The second can reuse it. `-count=1`
disables Go's own test-result cache so you can distinguish the two mechanisms.
For a Python project, use `receipt run -- uv run pytest` instead.

Receipt checks the repository files, command, working directory, environment
and supported toolchain information. It scans inputs before and after execution
and saves only successful results whose recorded inputs stayed the same.

```sh
receipt explain -- go test -count=1 ./...  # inspect input changes
receipt run --bypass -- go test ./...     # run without the cache
receipt run --verify -- go test ./...     # rerun and compare saved output
receipt cache list
receipt cache prune --max-bytes 104857600
```

Receipt flags go before `--`; test-command flags go after it. The default cache
is `.receipt/` in the workload repository. See [usage and configuration](docs/USAGE.md)
for extra input files, run modes and cache settings.

## When to use it

Receipt recognizes Go tests, several pytest invocation forms, and a restricted
set of `cargo test --frozen` commands. See the [Rust command reference](docs/CARGO.md)
for restrictions. Other commands require the explicit `--allow-generic` option.

A saved success is useful only when Receipt has recorded the inputs that matter.
Tests that depend on a changing database, network response, clock or random value
may produce a different answer without changing those recorded inputs. Bypass
Receipt for those tests, or make their inputs explicit and stable. Ignored files
must be declared when they affect the test.

Replaying output does not reproduce test side effects. Verification compares
output bytes, so changing timestamps or test ordering can cause a mismatch even
when assertions pass. Receipt also records absolute paths; different checkout
locations will not share a result. For very fast tests, checking inputs can cost
more than simply running them.

## Learn more

- [Read the book](docs/START-HERE.md): follow a run and learn how the cache works.
- [Measure a slow run](docs/MEASUREMENTS.md): compare direct execution with reuse.
- [Set up a shared cache](docs/SERVICE-CREDENTIALS.md): server requirements and access.
- [Contribute](docs/CONTRIBUTING.md): build, test and documentation checks.

Receipt is written in Go and licensed under [Apache 2.0](LICENSE).
Dependency licenses are in [THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES).
