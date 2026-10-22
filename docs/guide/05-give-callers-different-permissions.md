# 5. Give callers different permissions

You want a laptop to download saved results, while a CI job can also upload them. Giving both the same secret gives both the same permissions. Receipt's optional credential mode lets an operator issue separate secrets with explicit permissions and expiry times.

A [credential](../GLOSSARY.md#credential) is the secret a client presents. Anyone who obtains that secret can use it, so it must be protected. The operator can give a credential a label such as `laptop-reader` to help identify its intended use. The label itself grants no permissions and does not establish who is using the secret.

## Two questions before storage

[Authentication](../GLOSSARY.md#authentication) asks whether the credential is valid. [Authorization](../GLOSSARY.md#authorization) asks whether it allows the requested operation. A valid credential can still lack permission to upload. HTTP status 401 reports a missing or invalid credential; 403 reports a valid credential without the needed permission.

**Figure 5. A read-only credential cannot authorize an upload**

| Step | What happens |
|---|---|
| Check the credential | Receipt checks whether the secret is valid, unexpired, and not revoked. Failure stops the request. |
| Check upload permission | The laptop's credential allows reading, not publishing. Receipt refuses this upload here. |
| Storage stays untouched | The denied request does not proceed to decoding and storing a result. A permitted request would continue. |

The refusal happens in [`cacheAPI.serveScoped` in internal/cache/authorization.go](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L25-L41):

```go
if known && !principal.Allows(scope) {
	a.metrics.forbidden.Add(1)
	http.Error(w, "forbidden", http.StatusForbidden)
	return
}
```

`&&` means both conditions must hold, and `!` reverses a true-or-false answer. Here the request is refused when the operation is known and its permission is not allowed. `principal` holds the checked credential’s public details and permissions. `scope` is the permission this operation requires. `Allows` checks for that permission. When the check fails, Receipt counts the refusal, sends an HTTP response, and returns before the storage handler runs. `known` means the operation has a permission mapping; other code rejects unmapped operations.

## Why does Receipt ask the database each time?

Suppose the laptop's read-only secret is leaked. Anyone with it can download shared results until it expires or is revoked. The operator [revokes](../GLOSSARY.md#revocation) it, meaning later credential checks should refuse it. Receipt consults the current database record for each request instead of trusting a permission copy remembered earlier.

The database stores a hash of the token, not the usable secret. The token is generated from randomness intended for security, not chosen as a human password. This makes guessing a token much harder than guessing a common password; knowing its hash does not grant access.

Checking each request costs a database query. A remembered copy could reduce queries, but might keep accepting a revoked credential until it refreshes. We currently favor the direct check. This is a choice about revocation behavior, not a claim about production throughput.

## Revocation does not rewind a request

If an upload passes its credential check and then pauses, revoking the credential does not cancel that upload. It may still finish. A later request is checked again and will be refused once it sees the committed revocation.

When the database cannot perform a check, the server refuses access rather than substituting the old shared secret. The client may then run tests directly; server permissions do not control what the user's local programs can execute.

## What permissions do not provide

Callers still share one store. A read credential can access shared results regardless of which client uploaded them; it is not restricted to the laptop's own results. Browser login, private organizations, and a durable record of who uploaded each result are not implemented.

Remote credentials need encrypted transport, normally [HTTPS](../GLOSSARY.md#https), so someone observing the connection cannot read the secret in transit. TLS is the protocol that protects an HTTPS connection. Receipt delegates this encrypted connection to Caddy, the web server introduced in Chapter 9.

Receipt removes `RECEIPT_TOKEN` from the environment passed to the test process
and excludes it from the input key. Rotating that credential alone therefore does
not change the key. This removal is not a sandbox: run trusted code, since local
programs may have other ways to access files or secrets on the machine.

## Read further

Use the [credential setup guide](../SERVICE-CREDENTIALS.md) for commands. Use [Design reference](../DESIGN-DEFENSE.md) for the coordination rules and their limitations.

## Check your understanding

<!-- questions:start -->

### 401 or 403?

**Level:** Newcomer

A client presents a valid read-only credential when uploading a result. Which check refuses the request, and what HTTP status should it receive?

<details>
<summary>Answer</summary>

The permission check refuses it. Authentication asks whether the credential is valid: known, unexpired and not revoked. Authorization asks whether that credential allows the requested operation. This credential is valid but carries only the `read` permission, and uploading needs `publish`. The server answers HTTP 403 Forbidden for a valid credential without the permission, and HTTP 401 Unauthorized for a missing, invalid, expired or revoked one. The four permissions are `read`, `publish`, `report` and `metrics`, and none of them implies another.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L25-L41)

</details>

### Authentication versus permission

**Level:** Practitioner

A credential permits downloads. An upload using that credential receives HTTP 403. What does this tell you, and what would HTTP 401 mean instead?

<details>
<summary>Answer</summary>

HTTP 403 means the credential is valid but does not include the `publish` scope that uploads need. `cacheAPI.serveScoped` first authenticates the credential. A missing, malformed, unknown, expired or revoked credential gets HTTP 401 with a `WWW-Authenticate: Bearer` header. Only then does it look up the scope the route requires and check it with `Principal.Allows`. A valid credential without that scope gets HTTP 403, and the request never reaches decoding or storage.

The scopes are `read`, `publish`, `report` and `metrics`, and none implies another. A credential that may publish but not read cannot download what it uploaded.

**Learn more:** [Two questions before storage](../guide/05-give-callers-different-permissions.md#two-questions-before-storage) · [Permissions](../SERVICE-CREDENTIALS.md#permissions) · [`cacheAPI.serveScoped`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/authorization.go#L25-L41)

</details>

### Revocation while an upload waits

**Level:** Expert

An upload passes its credential check, then waits for storage. The operator revokes its credential before it resumes. Must this admitted upload stop? What about the next request?

<details>
<summary>Answer</summary>

The admitted upload may finish. Revocation changes later checks; it does not cancel work already admitted. The next request checks the database again and is refused once it sees the committed revocation. Caching old permission decisions would risk extending access further.

**Learn more:** [Chapter 5](../guide/05-give-callers-different-permissions.md)

</details>

<!-- questions:end -->
