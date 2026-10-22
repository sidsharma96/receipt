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
unchanged. For the example below, start in the **Receipt source checkout's root**,
the directory containing `Makefile` and `tools/`. A downloaded Receipt binary
alone does not include this helper. You need Go for this experiment.
The helper creates its own temporary cache:

```sh
make build
go run ./tools/measure --repo "$PWD" --receipt ./receipt \
  --repeats 5 -- go test -count=1 ./internal/strictjson
```

`make build` writes the Receipt executable to `./receipt`. The shell expands
`$PWD` to the current directory's absolute path; here it selects Receipt itself
as the workload. `--receipt ./receipt` selects the executable to measure.
`go run ./tools/measure` builds and starts the Go helper in this checkout.
That build happens before measurement begins.
Everything after `--` is the workload command: `./internal/strictjson` selects one
Go package under the workload directory. The trailing backslash continues the
shell command on the next line.

To measure another project, stay in the Receipt checkout and change `--repo` to
that project's absolute path and the command after `--` to its test command.
The helper runs tests from the chosen workload directory; it still uses the
Receipt executable you selected.

It runs five direct commands, one initial Receipt run and five expected hits.
It fails if a command fails or an expected hit executes instead. It removes its
temporary cache afterward; your command can still have its normal side effects.

Go's `-count=1` disables its test-result cache, not its compiled-package cache.
Earlier runs may warm filesystem or compiler caches. The helper uses a fixed
order, so small differences need a more careful experiment before drawing a
conclusion. Try a representative workload, repeat the comparison and keep failed
attempts in the report.

The helper prints JSON observations, not a table of medians. Under `samples`,
`kind` identifies `direct`, `miss` or `hit`. `client_ns` measures the helper's
elapsed wait for the child process, including startup. Wrapped runs also include
`receipt_ns`, Receipt's own invocation duration, and a `stages` list. Each duration
is in nanoseconds: divide by 1,000,000 for milliseconds.

For example, an illustrative `client_ns` of `360000000` means 360 ms. A
`fingerprint` stage of `260000000` means 260 ms inside that invocation. It is
already included in the invocation duration; adding the two would count it twice.
For five hits, sort their five `client_ns` values and take the middle one for
the median. Compare that with the direct-run median using the same field.

## Profile one operation

Suppose input checks dominate your hit, and you want to investigate file-reading
memory. From the **Receipt source checkout root**, build its fingerprint benchmark
and run only the 8 MiB file case:

```sh
mkdir -p .tools/profiles
go test -c -o .tools/profiles/fingerprint.test ./internal/fingerprint
.tools/profiles/fingerprint.test -test.run '^$' \
  -test.bench '^BenchmarkProfileFileInput/8388608$' -test.benchtime 200x \
  -test.benchmem -test.memprofile .tools/profiles/file.alloc.pprof
go tool pprof -top -alloc_space .tools/profiles/fingerprint.test \
  .tools/profiles/file.alloc.pprof
```

`go test -c` builds a test executable without running it. `-o` chooses where it
is written; `.tools/` is ignored local output, not source to commit.
`-test.run '^$'` selects no ordinary tests. `-test.bench` selects the named benchmark and
its `8388608`-byte case (8 MiB); the quotes keep the selection pattern intact.
`200x` requests 200 measured repetitions. The benchmark creates its own temporary
file before timing repeated reads, which normally use warm OS file caches.

`-test.benchmem` adds memory columns to the timing output. `ns/op` means elapsed
nanoseconds per operation, `B/op` means bytes allocated per operation, and
`allocs/op` counts allocations per operation. None is a complete CLI hit time.
`-test.memprofile` writes a sampled memory profile. `go tool pprof` reads that
file with the matching test executable; `-top` prints a function table and
`-alloc_space` selects total allocated bytes, including memory later reclaimed.
Profiling may affect timings; omit the profile flag in a separate timing comparison.

The benchmark resets its timing and allocation counters after creating the file.
The profile still covers setup and the test harness—the code that runs benchmarks.
For example, a large `bytes.Repeat` row comes from creating the fixture contents,
not from each timed file read. This is why a profile's totals need not equal
`B/op` multiplied by the repetition count. Follow the function and source, not
just the largest number.

Read `flat` as bytes attributed directly to a function and `cum` as bytes there
plus its callees—the functions it calls. The `%` columns express those as shares
of the total. Follow a large cumulative row to the functions doing the actual
allocating rather than adding overlapping totals. To inspect the current helper's
source beside its attributed samples:

```sh
go tool pprof -alloc_space -list 'hashFile' \
  .tools/profiles/fingerprint.test .tools/profiles/file.alloc.pprof
```

For processor work instead, replace the memory-profile option with
`-test.cpuprofile .tools/profiles/file.cpu.pprof` in a separate benchmark run.
Pass that CPU profile and the same executable to `go tool pprof -top`, omitting
`-alloc_space`. CPU sample times do not account for
all waiting or work in child processes. See Go's
[profiling walkthrough](https://go.dev/blog/pprof) and
[benchmark documentation](https://pkg.go.dev/testing#hdr-Benchmarks) for tool details.

These commands measure today's implementation. They should not reproduce the
earlier whole-file allocation split in
[Chapter 12](guide/12-profile-before-optimizing.md#follow-the-allocation-evidence-to-the-code).
That chapter explains what the earlier evidence justified changing. After a
change, compare identical workloads and confirm key compatibility, then repeat
the whole-command comparison to see whether users benefit.

## Find one server request

An operator can add `--request-log` to the server's existing startup command.
With logging enabled, each response includes `X-Receipt-Request-ID`. The ordinary
Receipt CLI does not expose this header, and `--timings` does not add it. Use an
HTTP diagnostic client that captures response headers for the request you are
investigating. For a simple demonstration on the server host, assuming Receipt
listens on its usual loopback port:

```sh
curl --silent --show-error --dump-header - --output /dev/null \
  http://127.0.0.1:7777/healthz
```

`--dump-header -` prints response headers to the terminal; `--output /dev/null`
discards the body. Copy the returned request ID. This identifies only this health
request; it cannot identify a previous upload. Diagnosing an authenticated lookup
or upload requires recording that operation's own response headers and timing,
with its normal credentials and payload.

Search the server's standard-error
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
established, and the [contributor guide](CONTRIBUTING.md#measurements-and-operations)
for restoring optional raw evidence.
