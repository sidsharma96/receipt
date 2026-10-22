# 10. Final walkthrough: miss, hit, conflict and recovery

Follow one command through four situations:

```sh
receipt run -- uv run pytest
```

Assume the Python environment already works and Receipt is configured to use
your remote cache. The question is: do we have a saved successful result for
these recorded inputs, and are we still allowed to reuse it? The `--` ends
Receipt's options; `uv run pytest` is the command Receipt may execute. The
local cache is on your machine; the remote cache is the shared server.

## A miss: run the tests

You run the command for the first time. Receipt builds a manifest: a description
of the inputs it knows about. This includes project files, command arguments,
environment-value hashes and Python environment information. It hashes that
description into a cache key, which is the name used to look up the result.
Call our example key `K`.

Watch where pytest runs in the diagram: on the client, not the cache server.

![A miss runs pytest on the client, then checks whether its result can be saved.](../assets/reviews/walkthrough-miss.svg)

Text equivalent: recorded inputs produce K. Neither the local nor remote cache
has a reusable result, so the client runs pytest. A successful run with stable
recorded inputs, bounded output and no local quarantine can be saved locally
and uploaded. Otherwise Receipt does not save a reusable result. Storage or
network failures can still prevent a save.

Receipt [records the inputs again](../GLOSSARY.md#stability-check) after execution. Actual excerpt from
[`fingerprint.Build` in app.go](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L323-L324):

```go
after, e := fingerprint.Build(ctx, dir, command, cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
stable := e == nil && after.Key() == key
```

`Build` returns two values: the new description in `after` and an error in `e`.
`:=` declares variables and assigns their values. `ctx` carries cancellation,
`dir` is the workload directory and `command` is the argument list. The remaining
arguments supply extra files, ignored environment names and whether generic
commands are allowed.

`nil` means no error. `&&` requires both conditions to hold and checks the second
only if the first holds. So a failed scan makes `stable` false without asking
the incomplete description for a key. `==` compares values: `key` describes the
inputs before execution and `after.Key()` describes them afterwards.
If you edit a source file while tests run, Receipt should not save
the result under the old description. Two matching scans cannot detect a file
changed and changed back between scans, or discover every external dependency.

The saved result contains the command description, stdout, stderr and exit code.
[Stdout](../GLOSSARY.md#stdout-and-stderr) is normal command output; stderr is its separate diagnostic output.
An [exit code](../GLOSSARY.md#exit-code) of zero means the command reported success. Editing a tracked
test file before the next invocation normally produces a different key, rather
than reusing K.

## A hit: replay without execution

Run the same command with unchanged recorded inputs. Receipt calculates K again
and finds the local result. When a remote cache is configured, it also checks
the server's quarantine status before trusting that local copy. Quarantine is
a warning attached to a key: different results have been reported, so stop
reusing it. The conflict section below follows how that warning is created.

![A local hit checks the remote quarantine status before replaying saved output.](../assets/reviews/walkthrough-hit.svg)

Text equivalent: Receipt loads K locally, asks the server about quarantine,
and replays the output and exit code if the check allows it. Pytest does not
start. If status is unavailable or the key is quarantined, Receipt executes
instead. With no local copy, a remote hit can supply the saved result.

Actual decision from [`s.replay` in app.go](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L308-L310):

```go
if hit && !o.verify {
    return s.replay(prior)
}
```

`hit` says a usable result was found; `!o.verify` means verification was not
requested. With both true, `prior`, the saved result, is passed to `replay`.
This returns before reaching the code that starts the tests. The replay function writes the
saved bytes to the terminal. Actual excerpt from
[`runSession.replay` in run_session.go](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L142-L162):

```go
if _, e := out.Write(prior.Stdout); e != nil {
    return 0, e
}
if _, e := errOut.Write(prior.Stderr); e != nil {
    return 0, e
}
```

`Write` returns a byte count and an error. `_` discards the count; `e` keeps the
error. The semicolon separates that assignment from the condition `e != nil`,
meaning an error occurred. The function returns two values: an exit code and an
error. `return 0, e` passes the error to its caller; the zero does **not** make a
failed write a successful command. If stdout cannot be written, replay stops
before writing stderr. Some output may already have reached the terminal.

Why contact the server for a local hit? Another client may have discovered a
conflict. An old local copy should not bypass that warning. This adds a network
check, and server trouble can force execution even with a local copy available.

A hit reproduces output, not execution. It does not recreate files the tests
would write, repeat their network requests or preserve the original mixing of
stdout and stderr. In our experiments, a separate execution marker established
whether tests really ran; seeing PASS twice would not establish that by itself.

## A conflict: the same key, different observations

Suppose a test prints elapsed time. These outputs are illustrative, not measured:

```text
Saved run: 48 passed in 0.42s
Fresh run: 48 passed in 0.51s
```

Both runs passed, but their output bytes differ. An ordinary hit cannot discover
this because it does not execute the tests. Ask Receipt to run a hit again:

```sh
receipt run --verify -- uv run pytest
```

![Different observed output quarantines K locally; reporting to the server is attempted separately.](../assets/reviews/walkthrough-conflict.svg)

Text equivalent: verification compares a fresh execution with the saved exit
code, stdout and stderr. For this mismatch, Receipt marks K quarantined locally,
removes the local saved result and attempts a shared conflict report. Future
local runs execute instead of replaying K. A failed report does not undo the
local marker, but it can leave other clients unaware of the conflict.

Quarantine means do not reuse this key. It does not mean Receipt has determined
which run was correct. Actual marker creation from [`quarantine` in app.go](https://github.com/sidsharma96/receipt/blob/main/internal/app/app.go#L222-L230):

```go
func quarantine(dir, key string) error {
	path := filepath.Join(dir, "quarantine", key)
	if err := atomically(path, []byte("conflicting observation; do not reuse\n")); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
```

`func` declares a function. Here `dir` and `key` are strings, and the return value
is an `error`. This function's `dir` is the **cache directory**, not the workload
directory passed to `Build`. With the default cache and our symbolic key `K`,
`filepath.Join` builds `.receipt/quarantine/K`. `[]byte(...)` converts the message
to bytes for writing; `\n` ends it with a newline. If `atomically` fails, the
function returns that error immediately.

The marker is a file, so it remains after this command ends.
`filepath.Dir(path)` selects its containing directory, `.receipt/quarantine` in
this example. `syncDir` asks the
operating system to persist the directory change after the marker's bytes have
been written and synchronized.
That is the durability step intended to preserve the name after a crash; it
still depends on successful synchronization and the filesystem/storage honoring
that request. A verification
mismatch returns a nonzero exit code even when the fresh tests passed. If
recorded inputs change during verification, Receipt instead reports that the
comparison is inconclusive. Exceeding the output capture limit also prevents
successful byte-for-byte verification.

Why not replace the old answer? That would hide disagreement. Timing text may
be harmless, but differing output may also expose randomness, an undeclared
input or an unreliable test. Two clients publishing different observations
under the same key are another way the shared store can detect a conflict.
There is no automatic rule that makes a quarantined key trusted again.

Consider a successful local marker write and entry removal, followed by a failed
network report:

| Location after this partial failure | What remains | What the next run can do |
| --- | --- | --- |
| This client | K's quarantine marker; the reusable local entry has been removed | Execute instead of replaying K. An ordinary rerun does not clear the marker. |
| Shared server | Its earlier result, without this client's conflict report | Other clients can still reuse it unless the server learns of a conflict by another successful report or publication. |

The local success does not make the two machines agree. Retrying a report, if
performed, must still reach the server successfully; an ordinary rerun is not
a promise that the failed report is automatically repaired.

## Recovery: restore stored state, not trust

Now suppose stored server data is lost. This is not conflict resolution:
restoring a backup recovers earlier state, not proof that a result is correct.

A shared result spans database records and output files. Copying only the
database can leave records pointing at missing files. Our backup includes both,
plus the binary and selected configuration. R2 is the external object-storage
service used to hold the encrypted backup archive. Follow the copy out and back:

![The downloaded R2 archive is decrypted and restored separately; live data stays in place.](../assets/reviews/walkthrough-recovery.svg)

Text equivalent: pause Receipt, copy database and files, restart, encrypt and
upload. Download the R2 object and decrypt it on the Mac with the private recovery
key. Verify checksums, restore a separate database and files, then fetch results
from a test server. The live data is never replaced during this drill.

Pausing the service keeps database records and output files from changing during
the copy. Restarting before encryption and upload keeps network transfer out of
that pause. Fetching results from the restored copy checks that its records and
files work together; a successful download alone would not establish that.

This drill used the existing machine. It did not establish recovery onto a new
host or without the private recovery key. Changes after the backup are absent:
a credential revoked later could appear unrevoked after restoring the old state.
The [operator guide](../DIGITALOCEAN-SETUP.md#backup-and-recovery) links the procedure
and recorded evidence. No nightly backup schedule is enabled.

## Check your understanding

<!-- questions:start -->

### What does a replay not reproduce?

**Level:** Newcomer

The original test run wrote a coverage report and printed warnings mixed in with normal output. What will a replay give you?

<details>
<summary>Answer</summary>

A replay gives back only the captured output and the exit code. It does not recreate files the tests wrote, such as a coverage report or a JUnit XML file, and it does not repeat network requests. The mixing of standard output and standard error is lost too: Receipt writes all of the saved standard output first, then all of the saved standard error. Timings printed in the output are the original run's timings. If you need a test's side effects, run it with `--bypass`.

**Learn more:** [A hit: replay without execution](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution) · [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay)

</details>

### What a hit gives back

**Level:** Practitioner

Which results can Receipt store, and on a hit, what exactly does the user get back?

<details>
<summary>Answer</summary>

Only successful runs are stored. `Validate` rejects any result whose exit code is not 0, so a failing run always executes again. A run is also not stored when its output exceeded the capture limit of 8 MiB per stream, when the inputs changed during execution, or when the key is quarantined.

A stored result holds the manifest, stdout bytes, stderr bytes, the exit code, the run's duration and its creation time. On a hit, `runSession.replay` prints a `receipt: hit` line with the saved duration to stderr, writes the saved stdout in full, then the saved stderr in full, touches the entry's modification time, and returns the saved exit code, which is 0. The tests do not run, so any files they would have written, network requests they would have made, and the original interleaving of stdout and stderr are not reproduced.

**Learn more:** [A hit: replay without execution](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md#a-hit-replay-without-execution) · [Execution and replay](../DESIGN-DEFENSE.md#execution-and-replay) · [`runSession.replay`](https://github.com/sidsharma96/receipt/blob/main/internal/app/run_session.go#L142-L162) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L74)

</details>

### A conflict report never reaches the server

**Level:** Expert

Verification writes the local quarantine marker and removes the saved entry, but its network report fails. Predict what this client and another client can reuse.

<details>
<summary>Answer</summary>

This client keeps its quarantine marker and executes instead of replaying that key. The shared server may still hold the earlier result without knowing about the conflict, so another client can still reuse it. A successful local write does not establish agreement between machines, and an ordinary rerun does not promise to repair the failed report.

**Learn more:** [Chapter 10](../guide/10-final-walkthrough-miss-hit-conflict-and-recovery.md)

</details>

<!-- questions:end -->
