# Receipt service credentials

Implemented 2026-10-09 (Phase 5). This is the first identity/permission step, not
a browser-login system or a multi-tenant service. One token identifies a credential
with an operator-assigned subject label. Possession is the proof the server checks;
it does not prove that the caller is a particular human or their upload is honest.

## Start and administer locally

These are illustrative commands. Use your existing private database URL and a
private server-cache directory. Credential administration uses database access,
not a network admin token. It initializes the cache/schema/binding as needed, just
like shared maintenance; listing is not a promise of zero first-time setup writes.

```sh
receipt auth issue --cache-dir /path/to/private/server-cache --subject alice --scopes read,publish,report --expires-in 24h
receipt auth list --cache-dir /path/to/private/server-cache
receipt auth revoke --cache-dir /path/to/private/server-cache --id CREDENTIAL_ID
receipt serve --postgres --credentials --cache-dir /path/to/private/server-cache --listen 127.0.0.1:7777
```

All these commands require RECEIPT_DATABASE_URL. The generated JSON from issue
contains token plus public metadata. Store that token in a secret manager and
supply it to the existing client through RECEIPT_TOKEN. Do not paste it into a URL,
commit it, log it, or place a literal secret in shell history. Outside loopback,
clients require HTTPS; arrange a TLS reverse proxy and restrict access to the plain
HTTP listener. This phase does not configure certificates or the proxy.

Issue requires an explicit subject and explicit scopes. Labels are 1–128 ASCII
characters, begin with a letter/digit, and otherwise permit letters/digits and
period, underscore, colon, at-sign, slash and hyphen. Labels are not unique account
names; the public credential ID distinguishes records. Scopes are exact lowercase
names without whitespace or repeats. There is no implicit all-permissions option.
Lifetime defaults to 24h and must be between 1s and 720h (30 days).

The server creates no default credential. Credential mode requires --postgres;
RECEIPT_TOKEN does not supply a fallback server secret in that mode. Without
--credentials, the old shared-token mode remains, requiring at least 24 characters
and giving token holders the previous broad access. Startup identifies the mode.

## Permissions

| Scope | Allowed operation |
| --- | --- |
| read | GET /v1/entries/KEY and GET /v1/status/KEY |
| publish | PUT /v1/entries/KEY |
| report | POST /v1/conflicts/KEY |
| metrics | GET /v1/metrics |

GET /v1/identity needs a valid credential but no additional scope. It returns that
credential's public ID, subject, scopes and expiry, not its secret. Other paths and
methods are rejected; a future endpoint must be mapped explicitly. Authentication
failure returns 401, missing permission returns 403, and authentication saturation
or a backend failure returns 503. Wrong/missing, expired and revoked credentials
share a generic 401 message. Normal body limits, JSON validation and storage status
codes still apply after permission checks.

Scopes do not imply one another. A read-only caller can reuse results but cannot
upload or report shared conflicts. Local verification can still quarantine its
local key; a rejected shared report is not secretly accepted or durably retried.
Normal Receipt clients need read,publish,report for their full current workflow.
A publisher can trigger shared quarantine by uploading contradictory output even
without report permission; report controls the explicit report endpoint, not every
possible reason quarantine can occur. Scope holders retain access to all keys in
this one shared store. There are no per-user private keys or organizations.

Receipt still fingerprints environment values by default, including RECEIPT_TOKEN.
Different or rotated credentials therefore change input keys. Ignore that variable
in .receipt.json only if it cannot affect the tests; this phase does not silently
remove it from fingerprints or from the child process's environment. Tests run with
the caller's environment and can read the token. Only run trusted code with these
credentials. Existing absolute paths and other environment differences also still
limit cross-client reuse. Authentication does not make keys portable.

## Token storage, expiry and rotation

A token contains a public random 128-bit ID and a random 256-bit secret, encoded
as rcpt_ID_SECRET in lowercase hexadecimal. crypto/rand supplies those random bytes.
The database stores a SHA-256 digest of the full token, not the bearer token.
The same digest also appears outside the database whenever the client has
RECEIPT_TOKEN set and does not ignore it: environment values are hashed into
every manifest, so each uploaded result carries SHA-256 of the caller's token.
The digest cannot be turned back into the token, but it links results to one
credential. See [the design defense](DESIGN-DEFENSE.md#d4-why-is-the-hash-of-receipt_token-in-the-key-a-problem).
Verification looks up the ID and compares fixed-length digests using Go's
constant-time comparison. This does not make the entire SQL/network request
constant-time. This hashing choice is for generated high-entropy tokens, not for
human-chosen passwords. A database writer can alter permissions/expiry or install
credentials; the database and operator remain trusted.

Expiry is calculated and checked with the database clock. Every request looks up
the row again: there is no local permissions cache. A revocation committed before
a later lookup takes its snapshot is observed by that lookup. An operation that
already passed authentication may still finish after revocation or expiry. It is
not rolled back or cancelled by revocation. This also does not revoke a user's
existing local cached files or stop them executing tests directly.

Rotate by issuing a replacement, installing it in the client and revoking the
previous ID. Scopes cannot be edited through the CLI; use a replacement credential.
Revocation is idempotent and preserves its original timestamp. Unknown IDs return
an error. Tokens cannot be recovered through list or identity.

If issue commits successfully but writing its JSON output fails, the credential
still exists. Find its public ID in list, revoke it and issue a replacement. An
uncertain database commit acknowledgement has a similar recovery concern; there
is no idempotent issuance key or secret recovery operation.

## Bounds and operational costs

All retained credential rows, including revoked/expired rows, count against a
1024-row issuance limit. A SQL advisory lock serializes issuance and migrations
so cooperating issuers cannot both claim the last slot. It uses key
72656365697076, separate from the cache's storage lifecycle lock since
15 October 2026; before that, issuance reused the storage key and paused cache
reads and uploads until it committed. Every `receipt auth` command still takes
the storage lock exclusively once while it opens the store, so plan renewals
for a quiet moment. List reads at
most 1025 rows and errors on over-capacity data. There is no purge command yet;
the limit makes this a bounded small deployment, not indefinite rotation at scale.
Credential rows are outside the cache's Phase 3 metadata/evidence budgets.

Each matching token request performs an indexed ID lookup before cache work.
Authentication has max-inflight slots and a two-second context. Cache work retains
its own max-inflight slots and 25-second context. Authentication releases its slot
before cache admission, so these are two separate bounds, not a single global
request/connection cap. Identity/metrics responses need authentication but bypass
cache-work slots, allowing inspection while storage is busy. Both kinds of work
share the server's 16-connection database pool. Pool contention can also produce
an authentication timeout; 503 does not uniquely identify a crashed database.

Counters add unauthorized, forbidden, auth_failures and auth_busy. They contain
no token, subject or ID labels and reset on restart. There is no durable per-upload
actor history or full audit log. Authentication admission is not a per-caller/IP
rate limit or protection against every denial-of-service attack.

## Evidence and remaining work

Tests cover the complete four-scope/five-operation matrix, unknown routes, invalid
or ambiguous headers, identity/metrics output, bounded authentication admission,
cancellation release, issued/forged/expired/revoked credentials, database outage,
CLI issue/list/revoke and lost issuance output. Real PostgreSQL tests also cover
eight concurrent issuers competing for one remaining row and a paused upload
finishing after revocation while the next request is rejected.

Browser sessions, OAuth/OIDC, organizations, tenant isolation, per-upload actor
attribution, credential purging, automatic renewal, database-role provisioning,
TLS deployment and production load testing remain future work. Multiple-server
clock/failover behavior, commit-acknowledgement loss and hostile infrastructure
were not established by this phase's tests. See the Phase 5 review for scoped
verification and reasons to revisit the choices.

The use of HTTPS outside loopback, per-operation access checks and avoiding tokens
in URLs follows the concerns described in [OWASP's REST Security guidance](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html).
