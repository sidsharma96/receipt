# Receipt's CI

A push to `main` starts two application jobs. One runs ordinary tests through
Receipt. The other always runs tests directly, including PostgreSQL checks and
race detection. A saved result must not be the only evidence that the caching
program itself works.

```text
push to main
  +-> cached job -> HTTPS cache available? -> reuse or execute ordinary tests
  |                                     no -> execute locally
  +-> independent job -> Go tests + race detector + disposable PostgreSQL

pull request -> independent job only
```

The independent database belongs to the job, not the deployed server. The cached
job excludes tests that need an external database or prepared Rust environment.
This arrangement retains duplicate work intentionally; it is not a claim that
Receipt reduces total CI compute.

## Inputs and environment

The helper gives the checkout and home directory consistent paths. It records
the runner image version and explicitly declares the PostgreSQL driver source,
Git, shell and Receipt binary as inputs. Its exact module allowlist fails visibly
when dependencies change so the declarations can be reviewed.

The selected environment excludes unrelated runner variables and opt-in test
settings. These rules are specific to this repository, not a reusable CI SDK.
See the [workflow](https://github.com/sidsharma96/receipt/blob/main/.github/workflows/receipt-ci.yml)
and [helper](https://github.com/sidsharma96/receipt/blob/main/tools/selfci/main.go).

## Secrets and renewal

Only trusted pushes to `main` receive `RECEIPT_SELF_CI_TOKEN`. Pull requests do not
receive private-cache credentials. Repository writers who can change workflows
remain trusted; a workflow condition cannot protect against such a writer.

The helper checks the HTTPS identity endpoint before enabling remote use. It
verifies certificates and refuses redirects. A missing token, failed connection
or rejected identity selects local execution. Expiry does not turn a failed test
into a success.

Inspect the actual credential with `receipt auth list` before scheduling renewal;
do not rely on a date copied into documentation. The token is not renewed
automatically. Issue a replacement, update the GitHub secret, verify access and
revoke the old credential. Normal clients need `read,publish,report` permissions.
Their token is excluded from Receipt's recorded inputs.

The existing [enrollment helper](https://github.com/sidsharma96/receipt/blob/main/tools/enroll/main.go)
runs with `go run ./tools/enroll` from the checkout. It requires an authenticated
GitHub CLI account with permission to set this repository's secrets, the operator
SSH key at `~/.ssh/receipt_do_ed25519` and a verified server host key. It issues
a 30-day token on the existing server and sends it to GitHub through standard
input; the terminal receives only the public credential ID.

It refuses any existing `RECEIPT_SELF_CI_` secret, so it does not perform the
replacement sequence above automatically. If an upload fails, revoke the reported
credential before retrying. Authentication commands also initialize storage and
may wait for the storage lock; perform administrative changes when no long
maintenance operation is active.

Tests run with trusted credentials. Removing Receipt's token from the child
environment is not a security sandbox against hostile same-user processes.

## Review a failed run

Read both jobs. If the independent job fails, investigate the test failure rather
than treating a cached success as authoritative. If the cached helper rejects
changed dependencies, update its explicit input declarations; do not disable the
guard. A red historical run remains visible after a later commit fixes it.

The documentation and release workflows have separate jobs. The release workflow
builds archives from a tag and publishes them only after its tests and smoke check
pass. It does not deploy the cache server.
