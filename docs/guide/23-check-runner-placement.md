# 23. Check whether we can control runner placement

A new GitHub runner asks the existing Receipt server for the same saved result.
On one completed job, a fresh fetch takes about **56 ms**, including roughly
**2 ms to open TCP**. Chapter 22 observed much longer connection waits. We have
not moved the server or shipped a faster client. The runner and its network
path can change between jobs.

That distinction matters before spending money on a server move. We wanted to
compare chosen runner locations. The current repository uses standard hosted
Ubuntu runners and has no registered self-hosted runners. Its workflow chooses
an operating-system image, not a geographic region. We therefore performed a
bounded feasibility check with three job attempts, rather than describing the
assignments as a controlled regional experiment.

## Choosing an image is not choosing a location

The existing workflow contains:

```yaml
runs-on: ubuntu-24.04
```

This is from the `client` job in
[profile-network.yml](https://github.com/sidsharma96/receipt/blob/main/.github/workflows/profile-network.yml).
It asks GitHub for that runner image. It does not mean “run beside our cache.”
GitHub documents runner selection through labels and groups in its
[workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idruns-on).
The repository runner inventory returned zero self-hosted runners. This is a
personal repository; GitHub's managed larger-runner offering is documented for
[organizations and enterprises](https://docs.github.com/en/actions/how-tos/manage-runners/larger-runners/manage-larger-runners).
We did not provision infrastructure or change account settings.

```text
Current job
  choose Ubuntu image
        |
  GitHub assigns a machine
        |
  fresh connection -> existing server

Controlled location comparison would need
  chosen location A -> same server
  chosen location B -> same server
  repeat on matched clients and workloads
```

This is a diagram of experimental choices, not a timing chart. A short TCP wait
is evidence about the observed path. It does not tell us which city the runner
is in, or let us request that city next time. We did not infer locations from IP
addresses. Choosing different OS versions would also change software and would
not isolate location.

## Keep the request the same

Each job used the same committed source, pinned Go version and existing fixture.
The diagnostic invokes the real cache client. In
[`Remote.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L63-L83):

```go
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxEntryBytes+1))
	if e != nil {
		return v, e
	}
	v, e = Decode(b)
```

The client collects bounded response bytes into `b`; a read error stops the
operation. `Decode` checks the result after reading finishes. The diagnostic
measures around this path and checks hashes of both received bytes and the
result encoded again after decoding. Matching these hashes across jobs means
we compared the same result, not a smaller response on the faster job.

Each completed job contains twelve primary fresh-process fetches interleaved
with separate diagnostic controls. Another batch keeps a process and connection
alive; its first request is cold and is excluded only from the warmed-control
summary. We do not pool that batch with fresh-process requests. It happens later,
so server warming and time variation can also affect the comparison.

These are **fetch durations**, not complete `receipt run` durations. Repository
fingerprinting, normal CLI startup, test execution and local publication are
outside the measured operation. CPU counters include application and operating
system work charged to the process; they are not hardware-cycle measurements.

## What the completed jobs measured

Both completed jobs used the same network-probe binary and returned the same
65,789-byte result. All 240 request IDs matched server logs. The service process
identities, executable hashes and Caddy configuration hash matched before and
after the three attempts.

| Median, primary fresh fetches | Job 2 | Job 3 |
| --- | ---: | ---: |
| Open TCP | 2.04 ms | 16.53 ms |
| TLS setup | 14.02 ms | 28.45 ms |
| Wait for first response byte | 24.81 ms | 41.87 ms |
| Collect body | 3.55 ms | 32.62 ms |
| Decode interval | 6.88 ms | 6.93 ms |
| Complete fetch operation | 56.12 ms | 129.35 ms |
| Client CPU during operation | 23.08 ms | 23.19 ms |

Each column contains twelve fresh-process observations. Each row is its own
median, so do not add rows to reconstruct a request. Client CPU was similar,
while several network-facing intervals grew. That supports the importance of
the observed path. It does not prove distance caused the difference: runner
hardware, routing and time were not independently controlled.

The later established-connection batches had medians of **32.32 ms** and
**43.71 ms**, with nineteen warmed requests each. Their CPU medians were about
8.61 and 8.71 ms. These controls show the opportunity when one process performs
repeated operations. They exclude the first setup request, do not include a
helper's lifecycle costs and were not randomly interleaved with fresh processes.

**Decision: keep the current server placement.** There is no measured regional
winner to migrate toward. First establish control over client location if a
migration remains the goal. The earlier 300–383 ms observations remain valid
history, but subtracting today's times from them would invent a product speedup.

## Keep a failed attempt visible

The first job fetched the result three times according to the server log, then
its status probe exited unsuccessfully. The existing harness raised an exception
before writing its request records. We cannot recover that probe's HTTP status
or timing from the retained artifact, and do not assign a cause from the exception.
Those three successful server entries are not a substitute for complete client
measurements. The failed attempt remains part of the experiment's record.

A local harness repair now saves numeric records before reporting a child error.
It does not export the child's stderr or credentials, retry failed requests, or
turn a failed run into a successful sample. This repair was tested locally after
the jobs were dispatched; it was not present in these measured jobs.

The new comparison helper rejects incomplete runs, changed binaries, differing
fixture results and unexpected connection reuse. Rejecting a run prevents an
invalid timing comparison; it does not erase the run's failure from the report.
Raw downloads stay under ignored `.tools/`, and CI artifacts expire after seven
days. The reference records the attempts and their limits.

## What would justify a placement change?

A regional comparison needs clients whose locations we can deliberately choose,
with comparable hardware, operating system and trust-store setup. Repeat fresh
fetches across locations and time blocks while holding the endpoint, fixture
and client binary fixed. Include failed requests and job setup costs, not only
the successful fetch median. Do not move production based on one short path or
one slow runner.

For the current CI job, there is one ordinary cached test command. A background
helper would have little opportunity to reuse its connection within that job.
Batching becomes more relevant if a job makes several cache operations, but its
first request still pays connection setup. JSON validation remains a separate
CPU target, especially when the network is already short. These alternatives
are current comparisons, not implemented optimizations.

The [measurement record](../MEASUREMENTS.md#runner-placement-feasibility-20-october-2026)
and [nine-question review](../PHASE-REVIEWS.md#runner-placement-feasibility-20-october-2026)
retain the exact results, attempts, source boundaries and remaining questions.

## Check your understanding

<!-- questions:start -->

### Does choosing Ubuntu choose a nearby runner?

**Level:** Newcomer

A workflow uses `runs-on: ubuntu-24.04`. Does that select a runner beside the cache server?

<details>
<summary>Answer</summary>

No. It selects a runner image. The current standard hosted setup has no region selection in that label. A short observed TCP interval describes one path, not a location we can request next time.

**Learn more:** [23. Check whether we can control runner placement](../guide/23-check-runner-placement.md)

</details>

### Can the faster job prove a placement improvement?

**Level:** Practitioner

Two jobs use the same client binary and result bytes. Their fresh fetch medians are 56 and 129 ms. Did moving the server improve performance?

<details>
<summary>Answer</summary>

No server move occurred. Different runner assignments and times can expose different paths. Matching binaries and results removes two confounders, but does not control geography, hardware or routing.

**Learn more:** [23. Check whether we can control runner placement](../guide/23-check-runner-placement.md)

</details>

### What can we infer from a failed job with missing request records?

**Level:** Expert

A server log contains three successful fetches, then the client job fails on status checking before saving its request records. Can those successes replace the missing timing sample?

<details>
<summary>Answer</summary>

No. Server entries do not recover the client intervals or the failed status outcome. Keep the failed attempt visible, reject it from comparisons requiring complete records, and repair evidence retention without claiming to reconstruct the lost data.

**Learn more:** [23. Check whether we can control runner placement](../guide/23-check-runner-placement.md)

</details>

<!-- questions:end -->
