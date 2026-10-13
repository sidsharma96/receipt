# Receipt tests Receipt

Added 12 October 2026. This extends the [synthetic experiment](CI-EXPERIMENT.md)
to Receipt's actual Go suite. CodeCraft is a possible next repository, not yet
configured.

Suppose you push a change. Two jobs start. One asks Receipt whether this exact
ordinary test run has succeeded before. The other runs tests directly, including
PostgreSQL checks and race detection. A cached success cannot replace that
independent check.

```text
Trusted push
    ├─ Cached job ── HTTPS ──▶ Caddy ──▶ Receipt on the same droplet
    │      ├─ saved result found → replay it
    │      └─ no result or no connection → run Go tests locally
    │
    └─ Uncached job ──▶ Go tests + race detector
                           ↕
                     disposable PostgreSQL

Pull request ──▶ uncached job only; no private-cache secrets
```

The database belongs to that GitHub job, not the Droplet. The uncached job does
not connect to production data. It always uses `-count=1`, which disables Go's
own test-result cache. Race detection looks for conflicting concurrent memory
access during this execution; passing it is not proof that all possible races
are absent.

## What is actually cached?

In [self_test.py](../deploy/ci/self_test.py), the test command is:

```python
COMMAND = ['go', 'test', '-count=1', './...']
```

The helper constructs a small environment without `RECEIPT_TEST_DATABASE_URL`,
`RECEIPT_TEST_CARGO` or measurement settings. Existing tests use these variables
to opt into external dependencies, so those tests skip in the cached job.
Rust integration remains excluded from both jobs until we add the required
toolchain and prepared registry explicitly.

The checkout, Go version and home directory have consistent paths on fresh
runners. The runner image version is an input, so an image update changes the
key. The helper additionally declares the current PostgreSQL driver source,
Git and shell binaries, and Receipt client binary as inputs. Adding another Go
module intentionally stops the helper until its declarations are reviewed.
This is a narrow setup for this repository, not a general CI SDK.

## Why keep the uncached job?

Receipt is the program under test as well as the program deciding whether to
skip tests. A bug in that decision must not be able to produce the only green
check. The independent job runs the tests anyway. For now this measures useful
reuse and checks behavior; it does not aim to reduce total CI compute because
we deliberately retain duplicate work.

The PostgreSQL suite also depends on a running database, not just files. It stays
outside Receipt's cached run. Both jobs must be reviewed; no branch-protection
rule has been changed to make either job a required merge check.

## Secrets and renewal

Only pushes to `main` and the rollout branch `codex/receipt-https-ci` receive
the `RECEIPT_SELF_CI_TOKEN` repository secret. Pull-request jobs do not receive
them. Repository writers who can change workflows remain trusted; a condition
inside a workflow is not protection against a malicious repository writer.

CI connects directly to `https://receipt.sidsharma.dev`. Caddy provides the
encrypted public connection and forwards requests to Receipt's loopback listener.
CI needs no SSH key or server shell. Receipt's token allows read, publish and report
against the shared cache; it is not restricted to one project's entries.
Trusted child tests can read the token from their environment.

The existing token expires on **11 November 2026 at about
7:28 p.m. Pacific** (12 November 03:28 UTC). There is no automatic renewal.
After expiry, the cached job reports that the cache is unavailable and runs
tests locally. It does not silently extend credentials or turn test failures
into successes.

Operator enrollment is [enroll_self_ci.py](../deploy/ci/enroll_self_ci.py).
It refuses existing keys/secrets instead of overwriting them. Before renewing,
finish active jobs, revoke credential `a985da859dd9a7440452f1a0cc4cfc47`,
and remove only `RECEIPT_SELF_CI_TOKEN`. Then enroll again. Partial enrollment failures
need operator inspection; the script is not a transactional deployment system.
Do not run the old synthetic experiment's cleanup script on these credentials.

## 13 October: moving the connection to HTTPS

The original 12 October rollout used a restricted SSH tunnel. The updated helper
contacts the HTTPS identity endpoint first. A valid response for `receipt-self-ci`
enables remote caching; a missing token, failed connection, rejected credential or
invalid response selects local execution. Certificate verification stays enabled,
and redirects are rejected so a server cannot redirect the token elsewhere.
Failure after that initial check still uses Receipt's existing execution fallback.

Before changing CI, a fresh Go fixture on the Mac uploaded through HTTPS.
A second invocation used an empty local cache and replayed the saved result;
an independent marker confirmed the test had executed only once. Changing the
source made it execute again and fail. The temporary credential was revoked.
This establishes one controlled upload/replay sequence, not a throughput claim.

The application token is unchanged: changing transport neither broadens its
permissions nor renews its expiry. Seven local helper-policy tests pass. Live
GitHub verification and removal of the retired SSH credentials are recorded
below; the older evidence remains historical.

[HTTPS run 38429263439](https://github.com/sidsharma96/receipt/actions/runs/38429263439)
tested commit `e95dcc0` twice on fresh runners. Attempt 1 missed and ran the
ordinary suite (22.30 seconds for the Receipt invocation). Attempt 2 reported a
remote hit for the same key `ff32a661f97a` (0.51 seconds). The independent
PostgreSQL/race job passed both times. These are individual observations, not
an estimate of typical latency; real-suite non-execution is identified by
Receipt's diagnostic, while the separate local fixture supplied marker evidence.

## Limits and reasons to revisit

Source files, declared dependencies and selected environment inputs do not
describe every property of a machine. These tests use temporary files, local
sockets and timing; a cached success does not prove they will always pass.
The independent job remains essential while we learn how useful this is.

No speedup claim follows from one hit. Timing printed by the helper covers one
Receipt invocation, not checkout, dependency downloads or total job time. Runner
hardware and load vary. We should measure several real pushes before changing
the uncached policy or claiming a cost saving.

Consider CodeCraft after this workflow is demonstrated. First inspect its test
configuration, dependencies and external-service use. Do not copy the Go-specific
input declarations into a Python project and assume they are sufficient.

## Verification record

Four local helper-policy tests and Python syntax checks pass, including a guard
that prevents the older experiment cleanup from disabling the new CI key.
The 11 existing documentation/template tests pass, and generated working HTML
matches its Markdown source. No production Go behavior was changed.

[Run 38341912711](https://github.com/sidsharma96/receipt/actions/runs/38341912711)
tested commit `f7a168f` twice on fresh GitHub-hosted runners:

| Attempt | Cached job | Receipt invocation | Independent job |
| --- | --- | ---: | --- |
| 1 | Miss; ordinary tests ran and published | 17.58 seconds | PostgreSQL/race suite passed |
| 2 | Remote hit for the same key `d4984cb5d7e9` | 0.51 seconds | PostgreSQL/race suite passed again |

These are two individual observations, not a benchmark or total-job speedup.
Both jobs started with new local Receipt directories. The hit was identified
by Receipt's diagnostic; unlike the synthetic fixture, this run did not add
execution-marker instrumentation to the real tests. The copied PASS output
alone is not independent evidence of execution or non-execution.

The live suite ran three helper-policy tests at that commit. The fourth
cleanup-ownership guard was added and verified locally afterwards. The ordinary
cached suite deliberately excludes opt-in PostgreSQL/Cargo/measurement tests.
The independent run supplies PostgreSQL but still does not enable Cargo or
measurement tests. No new renderer layout was introduced or visually audited.
