# Cargo support: deliberately bounded

Phase 7, 9 October 2026. The adapter recognizes one selected package's
`cargo test --frozen` command. Unsupported cases still run normally, **without
looking up or saving a Receipt result**. This is not a sandbox or support for
every Rust project.

## Try the supported path

Install the project's Rust toolchain and dependencies first. Keep its existing
`Cargo.lock`. In the cloned regex repository:

```sh
receipt run -- cargo test --frozen --manifest-path regex-syntax/Cargo.toml --lib --quiet
receipt run -- cargo test --frozen --manifest-path regex-syntax/Cargo.toml --lib --quiet
```

The first command executes; the second can replay. The selected package has
147 library tests in the recorded checkout. The main regex package uses build
scripts in its dependencies and is deliberately not cached by this adapter.
`--frozen` prevents Cargo from updating the lockfile or accessing the network;
it does not freeze the files or stop tests from accessing the network themselves.

## Accepted command grammar

- Required: executable basename `cargo`, followed by `test`, and `--frozen`.
- Optional switches: `--lib`, `--quiet`, `-q`, `--release`, `--all-features`,
  `--no-default-features`.
- Optional value pairs: `--manifest-path PATH`, `--features FEATURES`.
  Values must be separate arguments, not `--flag=value`.
- No `-p`, virtual-workspace selection, `--workspace`, `+toolchain`, `--target`,
  extra test arguments after `--`, config arguments or other flags yet.
  Select a member with `--manifest-path` instead. An installed toolchain can
  be selected through `RUSTUP_TOOLCHAIN`; explicit `cargo +name` is not parsed.

Unknown/repeated flags cause uncached execution. Even `--allow-generic` does not
override a recognized Cargo adapter's refusal. No command arguments are silently
removed or added to the test execution itself.

## What must be available?

Rustup must resolve installed Cargo, rustc and rustdoc executables. Each command
on PATH must be the Rustup shim or the corresponding resolved executable.
Receipt hashes the resolved executable bytes and each tool's verbose version
output. It assumes the remaining installed toolchain files, shared compiler
libraries, standard library and system linker are stable; it does not hash the
entire sysroot or operating system. This is not complete toolchain attestation.

Receipt asks Cargo for format-version-1 metadata with `--frozen`, the same
manifest/feature options and a host-platform filter. The selected root package
must belong to the current Git repository. Its normal and dev dependencies are
followed; dependencies' own dev dependencies are not needed. Shared dependencies
are visited once. Missing graph nodes or unknown dependency/target kinds cause
refusal. Metadata may over-approximate feature use across a workspace, so some
otherwise simple commands may be refused conservatively.

The lockfile is hashed even if Git ignores it. Local manifests and target entry
files are read explicitly. Other local source files retain Receipt's existing
Git/`extra_inputs` contract: ignored files, files elsewhere and external services
are not automatically discovered. External path dependencies are refused.

For registry dependencies, Receipt reads every regular file in each reachable
package's extracted source directory under `CARGO_HOME/registry/src`, including
checksum metadata. Changing extracted bytes therefore changes the recorded
identity even if the lockfile is unchanged. Git dependencies, unsupported source
layouts and symlink/nonregular files in these scans cause refusal. Trusted local
directory access is assumed; this does not defeat hostile concurrent path changes.

## Refusals before cache lookup

Build-script targets and procedural-macro targets are refused anywhere in the
selected dependency graph. Both can execute code during compilation and read
inputs we have not declared. The adapter does not inspect or trust their claimed
input declarations. Ordinary test code can also read outside inputs, so passing
this guard is still not a proof of isolation.

Cargo configuration files (`config` or `config.toml`) in the invocation
directory's ancestor `.cargo` directories or Cargo home cause refusal. This
avoids pretending to interpret configuration that can select wrappers, runners,
sources or compilers. Compiler override variables (`RUSTC`, `RUSTDOC`,
`RUSTC_WRAPPER`, `RUSTC_WORKSPACE_WRAPPER`, `RUSTFLAGS`, `RUSTDOCFLAGS`) also cause
refusal when nonempty. All nonempty `CARGO_*` variables except `CARGO_HOME`,
`CARGO_TARGET_DIR` and `CARGO_TERM_COLOR` are refused. These checks run even when
Receipt's general configuration ignores an environment variable for hashing.

## Cost and failure behaviour

The extra input record uses versioned names: `cargo-tool-v1:*`,
`cargo-version-v1:*`, `cargo-metadata-v1`, `cargo-sources-v1`. Manifest schema v1
is unchanged. Raw metadata/registry URLs are hashed, not stored in the manifest.

Cargo probes share a 30-second context and each has a 16 MiB stdout bound. The
extra lockfile/package scan allows 20,000 file reads and 256 MiB in aggregate.
This is separate from the existing Git scan and resolved tool-binary reads.
Read cancellation is checked between files; it is not a hard filesystem-I/O
deadline. Any prerequisite failure prevents reuse, then the original command
runs and retains its own exit code. Probe stderr is not copied into diagnostics;
actual test output is still passed through and is not a sanitized log.

Diagnostics distinguish an unsupported policy from a tool-resolution failure
and a failed frozen metadata query. A build-script/procedural-macro refusal
names the package using a bounded crate-name alphabet, not its registry URL.
The frozen-query message suggests checking the lockfile and preparing dependencies
outside Receipt. It does not claim to know the precise cause: missing downloaded
data, invalid project data and other Cargo failures can produce the same exit.

With `--timings`, `cargo_tools`, `cargo_metadata`, and `cargo_sources` report
tool resolution/reads/version queries, the metadata query, and the extra source
scan respectively. They are inside `fingerprint`, not additional time to add to
it. A miss normally calls each twice; a hit once. Git scanning and other work
remain in the enclosing fingerprint time. Failed stages are measured too.

Every hit still performs these checks and reads. For a tiny already-built Rust
suite, this can cost more than rerunning tests. See the Phase 7 review's scoped
measurements; no speedup or general performance guarantee is claimed.

To run the opt-in real Cargo fixture without network dependencies:

```sh
RECEIPT_TEST_CARGO=1 make check
RECEIPT_TEST_CARGO=1 make race
```

It uses an already-installed Rustup toolchain and local crates in temporary
directories. Other Cargo unit checks run normally without Rust installed.

An additional offline registry-mutation fixture is explicitly opt-in:

```sh
RECEIPT_TEST_CARGO=1 RECEIPT_TEST_CARGO_REGISTRY=/absolute/prepared/cargo-home make check
```

The prepared home must contain crates.io sparse-index data and the source/archive
for memchr 2.8.3 in Cargo's standard `index.crates.io-1949cf8c6b5b557f` layout.
The fixture copies the index and that crate into temporary storage; it never
edits the supplied home or downloads dependencies. An incomplete prepared home
fails the opted-in test, rather than silently skipping it. It saves a success,
adds a compiler error to the copied dependency without changing Cargo.lock,
and checks that Receipt executes and returns failure. Restoring the dependency
restores the previous hit. Only this fixture's generated dependency build files
are cleaned before the mutation run: Cargo otherwise assumes registry sources
are immutable. Receipt invalidation does not force Cargo itself to rebuild.

Sources: [Cargo metadata](https://doc.rust-lang.org/cargo/commands/cargo-metadata.html),
[Cargo test](https://doc.rust-lang.org/cargo/commands/cargo-test.html),
[configuration discovery](https://doc.rust-lang.org/cargo/reference/config.html),
[build scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html).
