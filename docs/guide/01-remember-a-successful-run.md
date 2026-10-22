# 1. Remember a successful run

Alice runs her tests. They pass. Ten minutes later she runs the same command without changing her code. Could Receipt avoid doing the same work twice?

It first needs a description of the run. The command alone is not enough: `pytest` can behave differently after a source file, environment setting, or Python package changes.

Receipt calls the process of collecting and describing these inputs
[fingerprinting](../GLOSSARY.md#fingerprinting). It records command arguments,
files, environment settings and supported runtime information, such as installed
Python package versions. It also records the working directory and platform.
A [hash](../GLOSSARY.md#hash) is a fixed-length value calculated from some bytes.
Receipt uses hashes to summarize file contents and environment values without
copying them into the description. Hashing is not encryption.

Receipt calls the whole description a [manifest](../GLOSSARY.md#manifest). It calculates another hash from that manifest and uses it as the [input key](../GLOSSARY.md#input-key), the name under which it looks for a saved run.

Suppose Alice runs `pytest` on a price calculation. Receipt records the bytes of
`prices.py`, a currency setting and the Python package versions, along with the
other inputs. Call the resulting key `K`. The letters here are illustrative names,
not actual hash values:

```text
prices.py + currency setting
  + package versions + other inputs
  -> manifest -> hash -> K
  -> look up saved successful output

edit prices.py
  -> different file hash
  -> different manifest -> new key
```

Without that input description, a cache indexed only by `pytest` could return
the old success after Alice breaks the calculation. Fingerprinting lets Receipt
distinguish those runs. It does not prove that it found every input: if the test
also reads a changing website that the description omits, the key can stay `K`.

**Figure 1. A local run, in order**

| Step | What happens |
|---|---|
| Describe the inputs | Receipt reads the files and settings it knows to record. |
| Calculate the key | Receipt gives this description a content-derived name. |
| Look for a saved result | If a usable result exists, Receipt prints it and does not start pytest. |
| Run pytest on a miss | If no usable result exists, Receipt starts the test command and shows its output. |
| Check the inputs again | If the description changed while tests ran, Receipt does not save that run. |

Finding a usable saved result is a [cache hit](../GLOSSARY.md#cache-hit). Having to run the command is a [cache miss](../GLOSSARY.md#cache-miss). A cache is storage for work you hope to reuse.

## Which files count, and why?

Receipt asks Git for tracked files and untracked files that are not ignored. It
reads their contents and permissions; a deleted tracked file is recorded as
deleted. Ignored data files need an explicit [extra input](../USAGE.md#configuration).
Receipt's own cache is excluded, so saving a result does not invalidate itself.

This broad selection can cause an unnecessary miss: editing a README changes the
key even if the tests do not read it. Recording only files a test actually uses
could avoid that work, but would require reliable discovery of those dependencies.
A test may open a data file dynamically, so source imports alone are not enough.
Receipt accepts extra misses to reduce the risk of silently omitting an input.

A Git commit alone would miss Alice's uncommitted edit. File modification times
are also insufficient: copying different bytes while preserving the old timestamp
can hide a change. Reading and hashing content costs more, but detects that case.
Absolute directory paths count too. A test can print its current directory or
read a file from its parent, so moving unchanged source files can change behavior.
That protection also limits sharing between checkouts at different paths.

## Why use a struct and slices?

The manifest groups different pieces of information in named fields. Source paths
in this book start at the Receipt source checkout's root. For example,
`internal/fingerprint/manifest.go` names a Go file inside two directories.
This is its actual [`Manifest` type](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L54-L61):

```go
type Manifest struct {
	Version   int      `json:"version"`
	Root      string   `json:"root"`
	Directory string   `json:"directory"`
	Platform  string   `json:"platform"`
	Command   []string `json:"command"`
	Inputs    []Input  `json:"inputs"`
}
```

In Go, [`struct`](../GLOSSARY.md#struct) groups named fields. `string` holds text and `int` holds a whole number. `[]string` means a [slice](../GLOSSARY.md#slice) of strings, a sequence whose length can vary. `Command` needs a sequence because arguments have an order. `Inputs` needs one because projects contain different numbers of files and settings.

Each `Input` describes one recorded item, such as a file and its content hash.
Before hashing the manifest, Receipt sorts input records by kind and name. Finding
the same files in another order should not change the key. Command arguments keep
their order because swapping them can change the command.

The text in backticks tells Go which field names to write in JSON. JSON is a text format that can represent objects, lists, strings, and numbers. `Version` lets Receipt distinguish this format from a later one.

A fixed-size array would require an input count in advance. A map, a lookup table, would make sense for finding one field by name, but these fields already have distinct names and types. Receipt does use a map during file discovery to avoid recording the same path twice.

## What happens when pytest runs?

Receipt starts pytest as another process. A [process](../GLOSSARY.md#process) is a running program with its own execution and memory. Receipt passes arguments directly rather than asking a shell to interpret a command string.

It shows output while capturing a copy. The capture has a size limit: a test can keep printing to the user, but an oversized capture will not be saved. Failed runs are not saved for replay either.

After execution, Receipt describes the inputs again. If that description changed, it does not publish the result. For example, uv might have installed packages during the run. The second scan helps notice that change.

## Matching inputs do not prove matching behavior

Suppose a test asks a public website for today's price. The recorded source files can stay unchanged while the website's answer changes. Receipt does not automatically discover that dependency.

The same issue applies to time, randomness, and some files outside the discovered set. Declare extra files where appropriate, or bypass the cache for a command whose dependencies you cannot safely describe. The two scans also cannot detect a file that changes and changes back during the run.

With `--verify`, Receipt runs a saved command again and compares the exit code and both output streams byte for byte. A difference makes it stop trusting that key. Even timing text can cause a difference; a mismatch is not automatically a failed test.

File inputs are read in chunks to keep temporary memory bounded. The
[performance chapter](12-profile-before-optimizing.md#read-large-files-in-chunks)
explains how that preserves the hash calculation.

## Read further

For the exact local storage rules and alternatives, see [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### What problem does Receipt solve?

**Level:** Newcomer

Alice runs `uv run pytest`, the tests pass, and ten minutes later she runs the same command without changing anything. What does Receipt do the second time, and what does it need to know to do it safely?

<details>
<summary>Answer</summary>

Receipt wraps the test command. Before running it, Receipt describes the run: the command arguments, the repository files, hashes of the environment values, the working directory, the platform and runtime information such as the Python packages. If a successful run with exactly the same description was saved earlier, Receipt prints that run's saved output and exit code instead of starting pytest. Finding such a result is a cache hit; having to run the command is a cache miss. To do this safely Receipt must name every input that could change the result, which is why it records so much more than the command line.

**Learn more:** [1. Remember a successful run](../guide/01-remember-a-successful-run.md) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L232-L335)

</details>

### An input Receipt never recorded

**Level:** Newcomer

Alice's tests read an ignored data file that she has not declared as an extra
input. Only that file changes. Does fingerprinting necessarily produce a new key?
How does this differ from editing a tracked README?

<details>
<summary>Answer</summary>

The omitted file's change does not affect the recorded description, so the key
can stay the same and an old success can be reused. Alice should declare the
file as an extra input or bypass caching for that command. A tracked README is
included even if the tests never read it, so its edit changes the key and may
cause an unnecessary miss. Fingerprinting compares recorded inputs; it does not
discover every dependency automatically.

**Learn more:** [Which files count, and why?](../guide/01-remember-a-successful-run.md#which-files-count-and-why)

</details>

### Why sort the inputs before hashing

**Level:** Practitioner

Receipt builds a manifest and hashes it into the input key. What does the manifest contain, and what does Receipt do so that the same inputs always produce the same key?

<details>
<summary>Answer</summary>

The manifest records a format version, the absolute Git root, the absolute working directory, the platform as `GOOS/GOARCH`, the command arguments in order, and a list of inputs. Each input has a kind, a name and a SHA-256 hash. There are four kinds: `file` for discovered repository files and declared extra inputs, `environment` for environment values except Receipt's own `RECEIPT_TOKEN` and names explicitly excluded by `ignore_environment`, `executable` for the bytes of the resolved command binary, and `toolchain` for a runner probe such as the Python package list or `go env -json`. Other environment values are included by default.

The key is the SHA-256 of the manifest encoded as JSON. Inputs are discovered by walking a Go map, whose iteration order is random, so `Manifest.Key` sorts a copy of the inputs by kind and then by name before encoding. Go's JSON encoder writes struct fields in declaration order, so the rest of the encoding is fixed. The command keeps its original order because reordering arguments can change what the command does. The copy matters: sorting the caller's slice in place would rearrange inputs the caller still holds.

**Learn more:** [Inputs and keys](../DESIGN-DEFENSE.md#inputs-and-keys) · [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L66-L77)

</details>

### The same files in another directory

**Level:** Expert

Alice copies unchanged source files to a different working directory and runs the same command. Must Receipt reuse the original result?

<details>
<summary>Answer</summary>

No. The manifest records the working directory as well as files and command
arguments. Changing that description changes the key, even when file contents
match. A test can print its directory or read a file from its parent. Omitting
paths would allow more sharing but could reuse output from a different context.

**Learn more:** [Chapter 1](../guide/01-remember-a-successful-run.md)

</details>

<!-- questions:end -->
