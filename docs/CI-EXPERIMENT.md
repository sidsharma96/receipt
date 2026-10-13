# Two CI runners sharing a result

Setup: 12 October 2026. This is a trusted, synthetic experiment, not a cache
enabled for pull requests. No arbitrary contributor code receives the secrets.

The first runner tests a function returning 42. Receipt saves the successful
output on our Droplet. The next job starts on a different runner with no local
Receipt files. It must fetch that result rather than execute the test again.
The test writes a marker each time it actually runs; replaying a printed PASS
line alone would not prove that execution was skipped.

```text
Runner A: test runs ── encrypted SSH tunnel ──▶ Droplet: save result
                                                     │
Runner B: test skipped ◀─ encrypted SSH tunnel ────────┘
    │
    ├─ change 42 to 43 → new input key → test runs and fails
    └─ restore 42, close tunnel → cache unavailable → test runs and passes
```

Each check uses a previously nonexistent local cache. Go's own test-result cache
is disabled with `-count=1`. Both jobs use the same Go version, fixture path and
small environment. A run-specific source comment separates independent runs.
This deliberately controls known differences; it does not make general Receipt
keys portable or establish that matching inputs guarantee deterministic tests.

## Access boundaries

The new `receipt-ci` Linux account cannot open shell or file-transfer sessions.
It can forward only to `127.0.0.1:7777`. It cannot forward to PostgreSQL or open
a reverse tunnel. The harness checks these denials with the actual CI key.
The existing administrator key is never sent to GitHub. The host key is pinned
from the existing verified administrator connection, not trusted on first use
inside CI.

The separate Receipt credential grants read, publish and report—not metrics or
database administration. It still accesses the entire shared cache: Receipt has
no per-project isolation. The trusted test process can read its token. Do not
reuse this workflow for untrusted pull requests.

Both the SSH key and Receipt token expire after one day. The repository secrets
are `RECEIPT_CI_SSH_KEY`, `RECEIPT_CI_KNOWN_HOSTS` and `RECEIPT_CI_TOKEN`.
The operator should revoke the token and remove the secrets after the experiment;
expiry is a fallback, not a recurring credential-rotation mechanism.

## Implementation and limits

- [Workflow](../.github/workflows/remote-cache-experiment.yml): two sequential
  GitHub-hosted Ubuntu jobs; read-only repository permission; pinned action commits.
- [Harness](../deploy/ci/experiment.py): fixture, restricted tunnel and assertions.
- [SSH restrictions](../deploy/ci/receipt-ci-ssh.conf): account-specific policy.
- [Provisioning](../deploy/ci/provision.py): refuses existing account/secrets;
  operator-only initial setup, not an idempotent deployment tool. A partial setup
  failure needs inspection and cleanup before retrying.

Only pushes to `codex/ci-tunnel-experiment` run this workflow. There is no scheduled
job, pull-request trigger, new public listener or new cloud resource. The cache
server does not execute the tests. This is a functional check, not a performance
benchmark, and does not test arbitrary network failures or runner-image drift.

SSH restrictions follow the [OpenSSH configuration reference](https://man.openbsd.org/sshd_config.5).
Secret handling follows [GitHub's Actions secret guidance](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets).

## Evidence

Local Python syntax checks and 11 existing documentation/template tests passed.
The [first GitHub run](https://github.com/sidsharma96/receipt/actions/runs/38341124737)
passed both jobs. Its original key began `f6ac282522e4` and its changed-source
key began `61586fd918db`.

| Check | Expected behavior | Observed actual test executions on that runner |
| --- | --- | ---: |
| First runner, original source | Miss, execute, upload | 1 |
| First runner, another empty local cache | Remote hit | Still 1 |
| Second runner, empty local cache | Remote hit | 0 |
| Second runner, source changed to 43 | Miss, execute, fail | 1 |
| Second runner, original source, tunnel closed | Execute locally, pass | 2 |

Both jobs also rejected shell sessions, forwarding to PostgreSQL and reverse
forwarding. Connection refusal was the tested network failure; a stalled
connection, partial response or full Droplet outage was not simulated.

GitHub warned that the first run's actions targeted a deprecated Node runtime.
The workflow now pins checkout v6 and setup-go v6. The
[second run](https://github.com/sidsharma96/receipt/actions/runs/38341291956)
passed the same checks on that revision. Its shared key began `f629bbccd989`;
the changed-source key began `88f042e959ae`. These checks did not change
Receipt's production Go code.

The cleanup command is `python3 deploy/ci/cleanup.py`, run by the operator after
jobs finish. It revokes the experiment's Receipt credentials, moves the public
SSH authorization file into root-only storage and deletes the three GitHub
secrets. It leaves the restricted Linux account, SSH policy and cached results
in place. Re-running requires deliberately provisioning new credentials; do not
put the administrator key into the workflow to bypass that step.

Cleanup was performed after the second successful run: the credential is
revoked, the authorized SSH key is disabled, and GitHub lists none of the
experiment secrets. The public key remains recoverable in root-only storage;
the temporary private key was removed when provisioning finished. The workflow
is intentionally not ready to rerun until access is provisioned again.
