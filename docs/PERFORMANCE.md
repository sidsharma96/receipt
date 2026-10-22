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

The investigation began with `go test -count=1 ./internal/strictjson` in Receipt's
working checkout: five direct runs, one miss and five hits, with no failures.
The hits skipped test execution. Their median duration was 358.91 ms; fingerprint
and lookup medians were 267.13 ms and 5.94 ms respectively. These are separate
group medians, not an additive breakdown of a single run. The checkout included
investigation fixtures, and its exact input manifest was not retained. These
[initial command samples](https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/profiling-2026-10-18/local.json)
pointed toward input checking, without isolating which part to change.

An [allocation profile](https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/profiling-2026-10-18/tool-output.json)
then showed two file-sized allocations: reading the contents and copying them
beside a permissions prefix. The profile's roughly 17 GB total accumulated across
repeated benchmark operations; it was not peak memory. Some investigation jobs
overlapped, so their timings are exploratory. The matched comparison below was
a separate experiment.

Streaming sends the same ordered bytes into one hash calculation through a
buffer of at most 32 KiB. These measurements compare the earlier whole-file
algorithm with that implemented change:

| Operation and metric | Whole-file algorithm | Streaming algorithm |
| --- | ---: | ---: |
| 4 KiB file: allocated bytes per operation | 10,616 | 4,956 |
| 4 KiB file: elapsed time per operation | 24.04 µs | 20.92 µs |
| 8 MiB file: allocated bytes per operation | 16,795,789.5 | 33,632 |
| 8 MiB file: elapsed time per operation | 13.24 ms | 9.20 ms |

Each cell is the median of six process reports, using six alternating pairs per
file size. Each process performed 2,000 timed operations for the small file or
200 for the large one. Runs were sequential, without profiling enabled, on an
Apple M1 with macOS ARM64 and Go 1.27.2. OS file caches were warm. Fixture creation
was outside the benchmark timer and allocation count; background machine activity
was uncontrolled. Large-file timings overlapped: 11.55–15.29 ms before and
5.51–16.87 ms after. The allocation reduction is clearer than a general speedup.

A separate whole-command fixture contained 256 small files and one 8 MiB file,
plus a Go test and module file. The old binary populated a temporary cache; eight
alternating hits per version reused it. Median Receipt duration was 240.50 ms
before and 235.75 ms after, with ranges of 181–283 ms and 183–493 ms. This does
**not** establish a reliable complete-hit latency improvement. All commands
succeeded, expected hits skipped execution, and both versions calculated the
same full key. Tests also compared old and new hashes across sizes and permissions.

The [matched observations](https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/streaming-2026-10-18/comparison.json)
and [comparison helper](https://github.com/sidsharma96/receipt/blob/88b6edf3c78c1f681735cb893c355b2ab45ae562/scripts/profile/compare_hashing.py)
retain the method. The change bounds the read buffer; it still reads every byte
and cannot make a concurrently edited file stable. See
[Chapter 12](guide/12-profile-before-optimizing.md) for the reasoning from profile
to implementation. These are measurements of that change, not a fresh benchmark
of all subsequent Receipt changes.

## Shared upload accounting

An earlier implementation recounted stored files and metadata on every upload.
In a local experiment, upload medians grew from about 33 ms on an empty store
to about 132 ms at roughly 4,045 results. Running totals reduced those medians
to roughly 19–24 ms across the measured sizes.

The running-total design is implemented. The comparison used 15 sequential uploads
per store size in one run per version, with a local database and warm storage.
An earlier baseline differed materially; the result supports removing growth
from repeated recounts, not a universal percentage speedup. It does not establish
public-service throughput. Uploads still take
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
- [Earlier measurement reference](https://github.com/sidsharma96/receipt/blob/66899768d6c0f21b869fdf8f23c3a3c3232cc51a/docs/MEASUREMENTS.md): file hashing, gzip and request traces
- [Upload-accounting comparison and method](https://github.com/sidsharma96/receipt/blob/66899768d6c0f21b869fdf8f23c3a3c3232cc51a/docs/DESIGN-DEFENSE.md#d15-what-does-one-upload-cost-and-how-does-that-grow)
- [First complete runner job](https://github.com/sidsharma96/receipt/actions/runs/39534150601)
- [Second complete runner job](https://github.com/sidsharma96/receipt/actions/runs/39534228155)
- [Failed runner attempt](https://github.com/sidsharma96/receipt/actions/runs/39533940975)

Job artifacts have limited retention and may no longer be downloadable. The
committed reports preserve reported scope and outcomes, not every raw observation.
Optional archive restoration is described in the
[contributor guide](CONTRIBUTING.md#measurements-and-operations).
