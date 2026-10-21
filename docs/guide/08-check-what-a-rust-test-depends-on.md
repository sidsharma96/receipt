# 8. Check what a Rust test depends on

Alice runs a Rust library's tests with [Cargo](../GLOSSARY.md#cargo), Rust's build and package tool.
She has not changed her test file. But the test calls a helper library, and that
library has changed. Replaying the old success would hide the change. Recording
only the test command or compiler version is not enough.

Receipt asks Cargo which packages the selected test needs. This relationship
is called a [dependency graph](../GLOSSARY.md#dependency-graph): an arrow from A to B means A needs B. A helper may
itself need another helper. Receipt follows these arrows before looking for a
saved result. The [dependency illustration](../FIGURES.md#figure-7-cargo-graph)
shows why the same helper can be reached more than once.

From [`cargoClosure` in cargo_graph.go](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L78-L126):

```go
if seen[id] {
	continue
}
seen[id] = true
```

`id` identifies the package being visited. `seen[id]` looks up whether it was visited before. `continue` skips to the next pass through the loop; assigning `true` marks a newly visited package. When two arrows lead to the same helper, Receipt checks it once rather
than repeating its work. Other maps let Receipt find a package's description
and outgoing arrows from its ID.

Cargo describes packages in a manifest file and pins selected dependency versions
in `Cargo.lock`. The supported `--frozen` command must use the existing resolution
without updating the lockfile or accessing the network. Receipt reads Cargo’s
metadata, its description of the selected packages and their relationships.

The walk follows libraries needed by the selected package and by its helpers.
A development dependency is needed only for a package’s own tests or examples.
Receipt includes the selected package’s development dependencies, but not a helper’s
private test dependencies: testing Alice’s package does not run the helper’s tests.

**Before Receipt reuses a Cargo result**

| Step | What happens |
|---|---|
| Select one package | Read frozen metadata using the chosen manifest and feature options. |
| Check its dependencies | Refuse build scripts, procedural macros and unsupported configuration before lookup. |
| Record the inputs | Read the lockfile, tools and supported package sources as well as repository files. |
| Look up the result | Replay only an eligible saved success; otherwise execute the original command. |

Some packages contain a [build script](../GLOSSARY.md#build-script): a program Cargo runs while compiling.
That program could read an undeclared file or invoke another tool. Receipt does
not yet track those actions. It therefore runs such a command normally without
using its cache. Procedural macros, which also execute during compilation, get
the same treatment. The [refusal illustration](../FIGURES.md#figure-7-cargo-refusal)
shows that this decision happens before reading a saved result.

In the real regex repository, the `regex-syntax` library's 147 tests can be saved
and replayed under the supported command. The main regex package still runs
uncached because its dependency graph contains build scripts. This is useful
partial support, not support for every Rust project.

The checks also cost time. In a follow-up with five direct runs and five hits,
the middle (median) regex-syntax run took 139 milliseconds directly and 434
milliseconds through a Receipt hit. But unicode-width's longer-running suite
took about 3.05 seconds directly and 0.39 seconds on a hit. The
[side-by-side measurements](../FIGURES.md#figure-7-cargo-hardening-cost)
show why a hit is not automatically a speedup: the checks must cost less than
the execution they avoid. These were local, warm-build observations, not a
remote-cache benchmark.

## What if a downloaded library changes?

Cargo.lock records which dependency versions were selected. It does not change
just because someone edits a downloaded source file. Receipt therefore also
reads the selected dependency files. Our isolated test edits a copied library
to introduce a compiler error: the old saved success must not hide that error.
The [three-state illustration](../FIGURES.md#figure-7-cargo-mutation)
shows the edit and restoration.

There are two caches in this example. Receipt remembers test results; Cargo
remembers compiled code. To demonstrate the error, the test clears only its
temporary compiled dependency files before execution. Receipt deciding to run
Cargo is not a promise that Cargo recompiles every file. No user dependency
cache is edited or cleared by this test.

## Read further

See the [command contract](../CARGO.md) and the [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### What does the Cargo adapter refuse?

**Level:** Newcomer

Alice's own crate has no build script, but a helper library it depends on has one. Will Receipt replay a previous success for `cargo test --frozen`?

<details>
<summary>Answer</summary>

No. Receipt follows the whole selected dependency graph before looking up a result. A build script is a program Cargo runs while compiling, and it could read files or settings Receipt does not record. Procedural macros also run code during compilation. When any selected package has either, Receipt refuses to reuse a result and runs Cargo normally. Receipt also refuses commands outside a short accepted grammar, any command without `--frozen`, Cargo configuration files, compiler overrides in the environment and dependencies that do not come from a registry or the repository.

**Learn more:** [8. Check what a Rust test depends on](../guide/08-check-what-a-rust-test-depends-on.md) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoBuildScript`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L30-L34)

</details>

### A helper library with a build script

**Level:** Practitioner

Alice's crate has no build script, but a helper library it depends on does. Should Receipt replay a previous success? Which dependencies does the Cargo adapter walk?

<details>
<summary>Answer</summary>

No. The helper's build script runs while this test compiles, and it could read files or environment that Receipt does not record. `cargoClosure` walks the resolved graph from `cargo metadata` starting at the selected package. `cargoPackagePolicy` refuses any package in that graph with a `custom-build` (build script) or `proc-macro` target, so Receipt runs Cargo uncached instead of looking up a result.

The walk follows normal and build dependencies of every package, but dev-dependencies only of the root package, because only the root's tests are compiled. A map of visited package ids makes each package checked once even when two arrows lead to it. The walk also refuses path dependencies outside the Git root, non-registry sources such as Git, and targets whose source lies outside their package. Passing these checks is not proof that every input is captured: a test can still read external files or services.

**Learn more:** [8. Check what a Rust test depends on](../guide/08-check-what-a-rust-test-depends-on.md#8-check-what-a-rust-test-depends-on) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoClosure`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L78-L126) · [`cargoPackagePolicy`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L152-L179)

</details>

### A build script one dependency away

**Level:** Expert

A Rust package's own dependencies are clean, but one of its dependencies has a dev-dependency that uses a build script. Does Receipt refuse to cache `cargo test --frozen` for the root package?

<details>
<summary>Answer</summary>

No. Receipt walks the resolved graph from the root with a depth-first search. It follows normal and build dependency edges everywhere, but dev-dependency edges only from the root package, because Cargo builds a dependency's dev-dependencies only when testing that dependency itself. The package with the build script is never visited, so `cargoPackagePolicy` never sees it. If the build script were on a normal or build edge anywhere in the walked graph, Receipt would refuse reuse before looking up the cache and run Cargo uncached. Passing the check still does not prove the tests read nothing else: ordinary tests can open files or call services Receipt does not record.

**Common wrong answer.** "Any build script anywhere in `cargo metadata` output forces uncached execution." Only the packages reachable through the edges Cargo would compile for this test are checked.

**Learn more:** [Cargo](../CARGO.md) · [Refusals before cache lookup](../CARGO.md#refusals-before-cache-lookup) · [`cargoDependencies`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/cargo_graph.go#L128-L150)

</details>

<!-- questions:end -->
