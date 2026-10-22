# Set up a shared cache with credentials

A CI job needs to download and publish test results; a dashboard needs only
aggregate metrics. Give them separate credentials so one can be revoked without
replacing the other. Each credential has an operator-chosen label and permissions.
Possession of its token grants access; the label does not prove a human identity.

## Start and administer locally

Prepare a dedicated PostgreSQL database and private server-cache directory.
Supply `RECEIPT_DATABASE_URL` securely. The administration commands use direct
database access and can initialize schema and storage bindings:

```sh
receipt auth issue --cache-dir /path/to/private/server-cache --subject ci-tests --scopes read,publish,report --expires-in 24h
receipt auth list --cache-dir /path/to/private/server-cache
receipt auth revoke --cache-dir /path/to/private/server-cache --id CREDENTIAL_ID
receipt serve --postgres --credentials --cache-dir /path/to/private/server-cache --listen 127.0.0.1:7777
```

Issuance prints the token once. Store it in a secret manager and provide it to
clients through `RECEIPT_TOKEN`. Do not put it in command arguments, URLs or
committed configuration. Remote clients require HTTPS; arrange a TLS proxy in
front of the private HTTP listener. See the [operator guide](DIGITALOCEAN-SETUP.md).

Credential mode requires PostgreSQL and creates no default user. It never falls
back to a shared server token. Without `--credentials`, legacy shared-token mode
remains available and grants its token holders broad access to one cache.

## Permissions

| Scope | Allowed operation |
| --- | --- |
| `read` | Fetch entries and check shared status |
| `publish` | Upload successful results |
| `report` | Submit conflict reports |
| `metrics` | Read aggregate server counters |

The identity endpoint requires a valid credential but no additional scope.
Permissions do not imply one another. Normal clients need `read,publish,report`.
A read-only client can quarantine its own local result after verification, but
cannot submit that conflict to the shared server.

A publisher can also trigger quarantine by uploading output that contradicts an
existing result. The `report` permission controls the explicit report endpoint,
not every way a conflict can arise. All callers share one store; permissions do
not separate customers' data into private namespaces.

Invalid, expired or revoked credentials receive HTTP 401. A valid credential
without permission receives 403. Authentication overload or a database failure
can produce 503. A working HTTPS connection does not imply permission to use it.

## Expiry and rotation

Each request looks up current credential state in PostgreSQL. A revocation that
commits before a later lookup is observed by that lookup. Work that already
passed authentication may finish, and revocation cannot erase a caller's cached
files. Expiry uses the database clock.

Rotate by issuing a replacement, installing it in clients and revoking the old
ID. Listing returns public metadata, never the original token. If issuance commits
but writing the token to the terminal fails, revoke that new credential and issue
another. There is no operation to recover its secret.

`RECEIPT_TOKEN` is excluded from input keys and removed from the test command's
environment. A renewal therefore does not invalidate otherwise matching results.
This is not a sandbox for untrusted test code running as the same operating-system
user. Older stored manifests may retain a digest of an earlier token.

## Storage and limits

Tokens contain a random public ID and a random secret generated with Go's secure
random source. PostgreSQL stores the token digest, not the bearer token. This
scheme is for generated secrets, not passwords people choose. An administrator
who can write the database remains trusted.

The label is 1–128 ASCII characters with a restricted punctuation set. Scopes
must be named explicitly. Lifetimes default to 24 hours and range from one second
to 30 days. Expired and revoked rows count toward the 1,024-row issuance limit;
there is no credential-purge command.

Issuance takes a separate lock from cache writes. Opening shared storage still
takes the storage lock once, so an administration command can wait behind long
maintenance work. Authentication and cache work have separate admission limits
but share the database connection pool. These limits do not provide per-client
rate limiting or complete protection against overload.

## What this setup does not provide

There is no browser login, organization model, tenant isolation, automatic renewal
or durable per-upload actor log. Reports are assertions by trusted clients, not
proof that tests ran honestly. Use the [design reference](DESIGN-DEFENSE.md#requests-and-permissions)
for the tradeoff between fresh permission checks and caching them.
