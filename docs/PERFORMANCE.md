# Selected measurements

A cached result can be slower than a short test. In one local Rust comparison,
replaying a result took about 434 ms while the tests themselves took 139 ms.
Receipt still had to inspect inputs. For a longer suite, that same kind of
checking was worthwhile: a hit took about 392 ms instead of 3,049 ms.

These examples explain costs, not a performance promise for a new project.
Use [the measurement guide](MEASUREMENTS.md) to test your own workload.

## Direct execution and reuse

| Workload | Direct run | Receipt hit | Scope |
| --- | ---: | ---: | --- |
| Python selection, 15 tests | 253 ms | 127 ms | Five runs per repeated group; one Mac; warm caches |
| Rust regex-syntax, 147 tests | 139 ms | 434 ms | Five direct runs and five hits; warm build cache |
| Rust unicode-width | 3,049 ms | 392 ms | Five direct runs and five hits; warm build cache |

Numbers are medians, the middle observation after sorting each group. The first
Receipt run also executes the tests and checks inputs twice, so it costs more
than the later hits. Runs were sequential rather than randomized. These small
samples do not describe worst-case latency or all projects.

The Rust cases used Go 1.22.1 on macOS ARM64 and an installed Rust toolchain.
The Python case also used Go 1.22.1 on macOS ARM64. The linked records retain the
commands and machine details. Different compiler caches and input sizes can
change the comparison substantially.

## File hashing

Reading a whole file and copying it beside a permissions prefix used temporary
memory proportional to the file size. Streaming the same ordered bytes into one
hash calculation reduces that buffer to at most 32 KiB. The compatibility tests
compare against the earlier calculation, including permission bits and chunk
boundaries.

This change is implemented. It reduces temporary allocation; it does not avoid
reading every byte or make a concurrently edited file stable. Related changes
stream executable hashing and reuse initial Git discovery within a command while
keeping file observations and toolchain probes fresh. See the
[performance chapter](guide/12-profile-before-optimizing.md).

## Shared upload accounting

An earlier implementation recounted stored files and metadata on every upload.
In a local experiment, upload medians grew from about 33 ms on an empty store
to about 132 ms at roughly 4,045 results. Running totals reduced those medians
to roughly 19–24 ms across the measured sizes.

The running-total design is implemented. The test used a local database and warm
storage; it does not establish public-service throughput. Uploads still take
turns, process their output bytes and commit database changes. Startup and
collection still recount storage. Their cost has not disappeared.

## Network and compression

Two complete hosted-runner jobs fetched the same saved result with the same
probe binary. Fresh fetch medians were about 56 ms and 129 ms. TCP connection
setup was about 2 ms and 17 ms respectively, while client CPU stayed near 23 ms.
Each primary group had twelve fresh-process observations. The server did not move.

A separate warmed-connection batch had lower medians, but excluded its initial
setup request and ran later. That demonstrates a possible benefit for repeated
requests in one process; it is not a measured speedup for the ordinary CLI.

A third job attempt failed after three server-recorded fetches. Its client
records were lost by the original driver, so the exact cause cannot be recovered.
The driver now saves reported numeric records before raising a child failure.
The failed attempt remains part of the evidence.

These jobs used Go 1.22.12 on Linux amd64 and the same 65,789-byte response.
They measured fetching a known key, excluding repository input checks and normal
CLI startup. Runner region, hardware and routing were not controlled. The
comparison supports investigating the network path when remote fetches are slow;
it does not identify a winning region.

A separate gzip comparison reduced response bytes but did not establish a
consistent latency improvement through the actual proxy. Compression adds CPU
work and interacts with connection startup. The deployed configuration remains
uncompressed. Raw traces are useful for reviewing that experiment, but are not
required to understand normal Receipt usage.

## Measurement sources

The records below contain commands, observations and environment details.

- [Python and shared-request samples](https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/phase-6-2026-10-09.json)
- [Sequential Rust comparisons](https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/cargo-hardening-2026-10-09.json)
- [Earlier measurement reference](https://github.com/sidsharma96/receipt/blob/66899768d6c0f21b869fdf8f23c3a3c3232cc51a/docs/MEASUREMENTS.md): file hashing, upload accounting, gzip and request traces
- [First complete runner job](https://github.com/sidsharma96/receipt/actions/runs/39534150601)
- [Second complete runner job](https://github.com/sidsharma96/receipt/actions/runs/39534228155)
- [Failed runner attempt](https://github.com/sidsharma96/receipt/actions/runs/39533940975)

Job artifacts have limited retention and may no longer be downloadable. The
committed reports preserve reported scope and outcomes, not every raw observation.
Optional archive restoration is described in the
[contributor guide](CONTRIBUTING.md#optional-investigation-tools).
