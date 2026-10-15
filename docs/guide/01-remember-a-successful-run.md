# 1. Remember a successful run

Alice runs her tests. They pass. Ten minutes later she runs the same command without changing her code. Could Receipt avoid doing the same work twice?

It first needs a description of the run. The command alone is not enough: `pytest` can behave differently after a source file, environment setting, or Python package changes.

Receipt records the command arguments, discovered files, environment-value hashes, and supported runtime information. It also records the working directory and platform. A [hash](../GLOSSARY.md#hash) is a short, fixed-length value calculated from some bytes. It helps compare content without keeping another full copy in the description. Hashing is not encryption.

Receipt calls the whole description a [manifest](../GLOSSARY.md#manifest). It calculates another hash from that manifest and uses it as the [input key](../GLOSSARY.md#input-key), the name under which it looks for a saved run.

**Figure 1. A local run, in order**

| Step | What happens |
|---|---|
| Describe the inputs | Receipt reads the files and settings it knows to record. |
| Calculate the key | Receipt gives this description a content-derived name. |
| Look for a saved result | If a usable result exists, Receipt prints it and does not start pytest. |
| Run pytest on a miss | If no usable result exists, Receipt starts the test command and shows its output. |
| Check the inputs again | Receipt saves a successful run only if its other storage conditions also hold. |

Finding a usable saved result is a [cache hit](../GLOSSARY.md#cache-hit). Having to run the command is a [cache miss](../GLOSSARY.md#cache-miss). A cache is storage for work you hope to reuse.

## Why use a struct and slices?

The manifest groups different pieces of information in named fields. This is the actual type, [`Manifest` in internal/fingerprint/manifest.go](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L53-L60):

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

## Read further

For the exact local storage rules and alternatives, see [Phase 1's review](../PHASE-REVIEWS.md#phase-1-review-remembering-a-local-run).

## Check your understanding

<!-- questions:start -->

### What problem does Receipt solve?

**Level:** Newcomer

Alice runs `uv run pytest`, the tests pass, and ten minutes later she runs the same command without changing anything. What does Receipt do the second time, and what does it need to know to do it safely?

<details>
<summary>Answer</summary>

Receipt wraps the test command. Before running it, Receipt describes the run: the command arguments, the repository files, hashes of the environment values, the working directory, the platform and runtime information such as the Python packages. If a successful run with exactly the same description was saved earlier, Receipt prints that run's saved output and exit code instead of starting pytest. Finding such a result is a cache hit; having to run the command is a cache miss. To do this safely Receipt must name every input that could change the result, which is why it records so much more than the command line.

**Learn more:** [1. Remember a successful run](../guide/01-remember-a-successful-run.md) · [`run`](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L231-L333)

</details>

### Why sort the inputs before hashing

**Level:** Practitioner

Receipt builds a manifest and hashes it into the input key. What does the manifest contain, and what does Receipt do so that the same inputs always produce the same key?

<details>
<summary>Answer</summary>

The manifest records a format version, the absolute Git root, the absolute working directory, the platform as `GOOS/GOARCH`, the command arguments in order, and a list of inputs. Each input has a kind, a name and a SHA-256 hash. There are four kinds: `file` for discovered repository files and declared extra inputs, `environment` for each environment variable except Receipt's own `RECEIPT_TOKEN`, `executable` for the bytes of the resolved command binary, and `toolchain` for a runner probe such as the Python package list or `go env -json`.

The key is the SHA-256 of the manifest encoded as JSON. Inputs are discovered by walking a Go map, whose iteration order is random, so `Manifest.Key` sorts a copy of the inputs by kind and then by name before encoding. Go's JSON encoder writes struct fields in declaration order, so the rest of the encoding is fixed. The command keeps its original order because reordering arguments can change what the command does. The copy matters: sorting the caller's slice in place would rearrange inputs the caller still holds.

**Learn more:** [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [D2. How does the manifest become a key every time the same way?](../DESIGN-DEFENSE.md#d2-how-does-the-manifest-become-a-key-every-time-the-same-way) · [`Manifest.Key`](https://github.com/sidsharma96/receipt/blob/main/internal/fingerprint/manifest.go#L65-L76)

</details>

### Two machines, one commit

**Level:** Expert

Alice and a CI runner check out the same commit and run `receipt run -- go test ./...`. Will they compute the same key? What decides it?

<details>
<summary>Answer</summary>

Usually not. The manifest records the absolute Git root and the absolute working directory, and each file input is named by its absolute path. The executable input records the absolute path of the resolved `go` binary and the SHA-256 of its bytes. Every environment variable is hashed by value, including `HOME`, `PATH`, `USER` and shell session variables such as `SHLVL`. The `go env -json` probe adds the Go version and settings. The keys match only if the checkout path, tool paths and bytes, environment values and platform are all identical.

Receipt's own CI gets cross-runner hits by building that sameness on purpose: a fixed checkout path, a fixed `HOME`, a small explicit environment that includes the runner image version, and declared extra inputs for its one Go module, `git`, `dash` and the client binary.

**Common wrong answer.** "Yes, because the key is derived from the repository's file contents." The file contents are only part of the manifest. Paths and the environment are in it too, so even two terminals on one laptop can disagree.

**Learn more:** [D1. What exactly goes into the cache key?](../DESIGN-DEFENSE.md#d1-what-exactly-goes-into-the-cache-key) · [What is actually cached?](../RECEIPT-CI.md#what-is-actually-cached) · [`test_environment`](https://github.com/sidsharma96/receipt/blob/main/deploy/ci/self_test.py#L18-L28)

</details>

<!-- questions:end -->
