# 5. Give callers different permissions

Alice should be able to download results. CI should also be able to upload them. Giving both the same secret gives both the same broad access. Receipt's optional credential mode lets an operator issue separate secrets with explicit permissions and expiry times.

A [credential](../GLOSSARY.md#credential) is the secret a client presents. Anyone who obtains that secret can use it, so it must be protected. The operator can give a credential a label such as `alice`, but that label is not a verified human account.

## Two questions before storage

[Authentication](../GLOSSARY.md#authentication) asks whether the credential is valid. [Authorization](../GLOSSARY.md#authorization) asks whether it allows the requested operation. A valid credential can still lack permission to upload.

**Figure 5. Alice tries to upload with a read-only credential**

| Step | What happens |
|---|---|
| Check the credential | Receipt checks whether the secret is valid, unexpired, and not revoked. Failure stops the request. |
| Check upload permission | Alice's credential allows reading, not publishing. Receipt refuses this upload here. |
| Storage stays untouched | The denied request does not proceed to decoding and storing a result. A permitted request would continue. |

The refusal happens in [`cacheAPI.serveScoped` in internal/cache/authorization.go](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L24-L40):

```go
if known && !principal.Allows(scope) {
	a.metrics.forbidden.Add(1)
	http.Error(w, "forbidden", http.StatusForbidden)
	return
}
```

`principal` holds the checked credential's public details and permissions. `scope` is the permission this operation requires. `Allows` checks for that permission. When the check fails, Receipt counts the refusal, sends an HTTP response, and returns before the storage handler runs. `known` means the operation has a permission mapping; other code rejects unmapped operations.

## Why does Receipt ask the database each time?

Suppose Alice's secret is stolen. The operator [revokes](../GLOSSARY.md#revocation) it, meaning later credential checks should refuse it. Receipt consults the current database record for each request instead of trusting a permission copy remembered earlier.

The database stores a hash of the token, not the usable secret. The token is generated from randomness intended for security, not chosen as a human password. Exact byte counts and comparison mechanics are implementation details in the review reference.

Checking each request costs a database query. A remembered copy could reduce queries, but might keep accepting a revoked credential until it refreshes. We currently favor the direct check. This is a choice about revocation behavior, not a claim about production throughput.

## Revocation does not rewind a request

If an upload passes its credential check and then pauses, revoking the credential does not cancel that upload. It may still finish. A later request is checked again and will be refused once it sees the committed revocation.

When the database cannot perform a check, the server refuses access rather than substituting the old shared secret. The client may then run tests directly; server permissions do not control what the user's local programs can execute.

## What this phase does not provide

Callers still share one store. Read permission does not mean access to only Alice's results. Browser login, private organizations, and a durable record of who uploaded each result are not implemented.

Remote credentials need encrypted transport, normally [HTTPS](../GLOSSARY.md#https), so someone observing the connection cannot read the secret in transit. Receipt itself does not terminate TLS. The deployment in chapter 9 puts Caddy in front of it for that job. The secret is also currently passed to the child test process through the environment. Run trusted code, and do not automatically exclude that setting from input recording if it can affect behavior.

## Read further

Use the [credential setup guide](../SERVICE-CREDENTIALS.md) for commands. Use [Phase 5's review](../PHASE-REVIEWS.md#phase-5-review-separate-caller-credentials-and-operation-permissions) for exact lifetimes, row limits, concurrency mechanisms, and verification records.

## Check your understanding

<!-- questions:start -->

### 401 or 403?

**Level:** Newcomer

Alice's credential lets her download results. She tries to upload one and the server refuses. Did authentication fail?

<details>
<summary>Answer</summary>

No. Authentication asks whether the credential is valid: known, unexpired and not revoked. Authorization asks whether that credential allows the requested operation. Alice's credential is valid but carries only the `read` permission, and uploading needs `publish`. The server answers HTTP 403 Forbidden for a valid credential without the permission, and HTTP 401 Unauthorized for a missing, invalid, expired or revoked one. The four permissions are `read`, `publish`, `report` and `metrics`, and none of them implies another.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L24-L40)

</details>

### Authentication versus permission

**Level:** Practitioner

Alice can download results but her upload is refused. Did authentication fail? How would the HTTP status tell you?

<details>
<summary>Answer</summary>

No. Her credential is valid; it does not include the `publish` scope that uploads need. `cacheAPI.serveScoped` first authenticates the credential. A missing, malformed, unknown, expired or revoked credential gets HTTP 401 with a `WWW-Authenticate: Bearer` header. Only then does it look up the scope the route requires and check it with `Principal.Allows`. A valid credential without that scope gets HTTP 403, and the request never reaches decoding or storage.

The scopes are `read`, `publish`, `report` and `metrics`, and none implies another. A credential that may publish but not read cannot download what it uploaded.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L24-L40)

</details>

### Is credential administration independent of storage?

**Level:** Expert

An operator issues a new credential while CI is uploading results. Does issuance interact with cache traffic at the lock level?

<details>
<summary>Answer</summary>

Yes. `Postgres.Issue` and the credential-table migration run `pg_advisory_xact_lock(72656365697075)`, which is the same key as the storage `lifecycleLock`, taken exclusively. Issuance waits for every in-flight read, upload and collection to finish, and while it waits and holds the lock PostgreSQL queues new reads and uploads behind it. The hold itself is short, one `count(*)` and one insert, so a renewal pauses cache traffic for the slowest in-flight storage operation plus a few milliseconds. A long `shared gc --apply` can therefore stall `receipt auth issue`. Every `receipt auth` command also runs the storage migration through `openShared`, which takes the lifecycle lock and its `flock` once more. Authentication of existing tokens takes no lock and continues.

The lock exists so that two concurrent issuers cannot both count 1,023 rows and insert past the 1,024 cap. The fix is a distinct key, such as `72656365697076`, and a test that all keys differ.

**Common wrong answer.** "Credentials have their own dedicated lock." Earlier documents claimed this; the code reuses the storage key.

**Learn more:** [D13. Which advisory lock keys exist, and what does each guard?](../DESIGN-DEFENSE.md#d13-which-advisory-lock-keys-exist-and-what-does-each-guard) · [G1](../DESIGN-DEFENSE.md#g1-credential-issuance-takes-the-storage-lock) · [`Postgres.Issue`](https://github.com/sidsharma96/receipt/blob/main/internal/auth/postgres.go#L46-L69)

</details>

<!-- questions:end -->
