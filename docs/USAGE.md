# Usage and configuration

Run Receipt from the workload’s Git repository, with dependencies already installed.
Put Receipt options before `--` and the command to run after it.

## Useful commands

```sh
receipt run --bypass -- uv run pytest     # no lookup, fingerprint or storage
receipt run --refresh -- uv run pytest    # execute and refresh local result
receipt run --verify -- uv run pytest     # execute a hit and compare output
receipt cache list
receipt cache prune --max-bytes 104857600
```

`--verify` compares stdout, stderr and exit code byte for byte. Timing text,
randomized test ordering and output paths can cause mismatches without a test
failure. A mismatch exits nonzero and quarantines that key **on this client**.
Quarantined keys are neither replayed nor republished locally. Quarantine is
persistent; there is deliberately no automatic "trust it again" policy.
`--verify` exits 0 on a match, 1 when inputs changed during the rerun
(inconclusive), and the rerun's own nonzero code, or 1, on a mismatch.
`--refresh` quarantines a key only when the fresh run fails after a cached
success on unchanged inputs; a successful refresh replaces the local entry.
The three modes are mutually exclusive. See
[run modes](DESIGN-DEFENSE.md#execution-and-replay).

Misses stream output immediately. Hits replay stdout then stderr; original
cross-stream interleaving, terminal behavior and test side effects are not
reproduced. Commands read standard input from `/dev/null`, so a prompt gets
end of file at once. Failures and interrupted commands
are never stored. Cache/network errors fall back to execution where possible.

## The input contract

Each manifest includes:

- Git-tracked and untracked nonignored working-tree files, including permissions
  and deleted tracked-file markers. The Git commit alone is not the identity.
- Exact arguments, absolute working directory and repository root, OS and CPU
  architecture, executable bytes, and resolved toolchain metadata.
- Environment variable names with SHA-256 hashes of their values.
- Python implementation, interpreter location and installed distribution versions
  for supported pytest invocations.
- Explicit extra inputs from `.receipt.json`.

The cache defaults to `.receipt/`, which is excluded from fingerprints. Symlinks
and submodules cause an uncached run rather than an incomplete fingerprint.
Input changes between pre-run and post-run scans prevent publication. A file
changed and restored during execution is not detectable with these two scans;
run tests on a stable working tree.

**This is a declared-input cache, not a hermetic execution sandbox.** Ignored
data files, external databases, network responses, time, randomness, external
editable packages, and changes to installed package bytes without version
changes are not discovered automatically. Add files to `extra_inputs`, keep the
environment immutable, or bypass the cache for those tests.

Absolute paths deliberately prevent reuse between different checkout locations.
Portable keys and sandboxed execution are not implemented. All environment
values are hashed by default, which favors extra misses over unsafe hits. Hashes
are not encryption; low-entropy values can be guessed. Only share manifests with
trusted users.

## Configuration

Optional `.receipt.json` at the workload's repository root:

```json
{
  "extra_inputs": ["fixtures/private-data.csv"],
  "ignore_environment": [],
  "max_bytes": 1073741824,
  "remote": ""
}
```

Extra input paths are relative to the repository root; absolute files and
directories are also allowed. Missing extra inputs cause an uncached run.
Ignoring an environment variable is a correctness decision: only ignore it if
it cannot affect the test. Receipt's own `RECEIPT_TOKEN` is never fingerprinted
and is removed from the test command's environment. The config file itself is always fingerprinted,
including when Git-ignored.

Configuration is limited to 1 MiB. JSON names are exact and case-sensitive;
duplicate names, unknown fields and null scalar settings are rejected. The same
strict-shape checks apply to stored results and shared requests. See the
[JSON input contract](JSON-INPUT-CONTRACT.md) for required fields, permitted
nulls, compatibility and size limits. Existing v1 writer/key encoding is unchanged.

The local cache has a 1 GiB default entry limit. Hits touch entry modification
times; pruning evicts least recently touched entries. This disk LRU is an
approximation across processes, with a soft size bound during concurrent runs.
Each entry is written to a temporary file, synced to disk, then published with a
hard link, which creates the entry only if none exists. The first complete entry
for a key wins and is never overwritten, so a crash never leaves a partial entry.
History and quarantine metadata are outside that entry-byte budget.
