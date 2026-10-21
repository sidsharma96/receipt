# Measure a slow run

Add `--timings` to the command you already use:

```sh
receipt run --timings -- go test -count=1 ./...
```

Receipt prints a timing summary to standard error. Start by checking whether
time is spent inspecting inputs, looking up a saved result or running the tests.
A cache hit skips execution but still has to inspect inputs and read the result.

## Read the timings

Timings measure elapsed time: both computation and waiting. A long lookup may
mean slow storage or a network wait; it does not necessarily mean the CPU is busy.
Some stages contain others. Do not add every number to calculate a total.

| Stage | What to investigate |
| --- | --- |
| `fingerprint` | The full input check, including files and toolchain information |
| `discovery` | Git file listing and declared extra inputs |
| `input_files` | Reading and hashing ordinary input files |
| `executable` | Reading the command executable |
| `toolchain` | Starting and reading toolchain probes |
| `lookup` | Finding a saved result, including remote work when configured |
| `execution` | Running the test command and collecting its output |

A missing stage was not measured; it is not evidence of zero cost. The
[performance chapter](guide/12-profile-before-optimizing.md) explains how to
choose a more detailed measurement once you know which stage matters.

## Compare reuse with running the tests

A useful comparison has three cases: run the command directly, run it through
an empty Receipt cache, then reuse the saved result. Keep the files and environment
unchanged. The repository includes a helper that creates its own temporary cache:

```sh
make build
python3 scripts/measure_local.py --repo "$PWD" --receipt ./receipt \
  --repeats 5 -- go test -count=1 ./internal/strictjson
```

It runs five direct commands, one initial Receipt run and five expected hits.
It fails if a command fails or an expected hit executes instead. It removes its
temporary cache afterward; your command can still have its normal side effects.

Go's `-count=1` disables its test-result cache, not its compiled-package cache.
Earlier runs may warm filesystem or compiler caches. The helper uses a fixed
order, so small differences need a more careful experiment before drawing a
conclusion. Try a representative workload, repeat the comparison and keep failed
attempts in the report.

## Find one server request

An operator can add `--request-log` to the server's existing startup command.
Each response includes `X-Receipt-Request-ID`. Search the server's standard-error
log for that ID to find the matching timing summary. The server creates the ID;
it ignores caller-supplied request IDs.

| Server stage | Work included |
| --- | --- |
| `authentication` | Checking a scoped credential in the database |
| `db_pool` | Waiting for a storage database connection |
| `db_lock` | The lock request, including database travel and waiting |
| `fs_lock` | Opening and acquiring the filesystem lock |
| `sql` | Selected database operations, with validation and commit where applicable |
| `files` | Reading, writing, checking or removing stored output |
| `inventory` | Counting records and files during maintenance |

These stages do not cover the entire client journey. The client also pays for
connection setup, sending requests, reading responses and validating the result.
A short server duration can therefore accompany a slow client fetch.

The request summaries omit credentials and test output. Other process output
can still contain sensitive data. Logging is synchronous: a slow log destination
can delay completion, and the reported handler duration excludes that final write.
Use these summaries for diagnosis, not as a durable audit log.

## Keep a useful result

Report the command, source version, machine, toolchain, sample count, failures
and whether caches were warm. Put the main finding first. Link raw records only
when they help someone reproduce or challenge it; do not paste entire hashes
or traces into the explanation.

Save raw output under ignored `.tools/` or as a CI artifact. See
[selected measurements](PERFORMANCE.md) for examples of what earlier experiments
established, and the [contributor guide](CONTRIBUTING.md#optional-investigation-tools)
for restoring optional raw evidence.
