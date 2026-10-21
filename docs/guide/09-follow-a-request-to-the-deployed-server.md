# 9. Follow a request to the deployed server

Suppose GitHub starts a fresh computer to run Receipt's tests. That computer has
no local saved result. It can contact `https://receipt.sidsharma.dev` directly. Follow the arrows: finding
the address and fetching the result are different conversations.

![DNS finds the droplet; HTTPS carries the request to Caddy.](../assets/reviews/deployment-request.svg)

The worker first asks DNS for the address of `receipt.sidsharma.dev`. [DNS](../GLOSSARY.md#dns) maps
names to addresses. Cloudflare answers with our server’s IP address. DigitalOcean calls this rented server a droplet. In our DNS-only
setup, Cloudflare does not carry the subsequent cache request.

Next the worker opens an HTTPS connection to Caddy on the droplet. HTTPS encrypts
the public connection and checks a certificate for the hostname. Caddy passes
the request to Receipt inside the same machine. This forwarding role is called
a [reverse proxy](../GLOSSARY.md#reverse-proxy). It lets Receipt concentrate on cache behavior while Caddy
handles certificates and their renewal.

Actual Caddy configuration, from [`reverse_proxy` in deploy/digitalocean/Caddyfile](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/Caddyfile#L1-L3):

```text
receipt.sidsharma.dev {
	reverse_proxy 127.0.0.1:7777
}
```

The hostname identifies the public site. `127.0.0.1:7777` identifies Receipt
on this same machine; `7777` is its port, the number used to select the service.
In this deployment, only Caddy’s web ports and SSH, the administrator’s remote-login service, are public. Receipt's own port and
PostgreSQL stay private. Local HTTP avoids another certificate between processes
on the same host; it also means Caddy and the administrator can see requests.

## Encryption is not permission

Anyone can know the hostname and make an HTTPS connection. Receipt still asks
for a token and checks whether it permits the requested operation. A token is
a secret the client presents, not something supplied by DNS or by the site's
certificate. A rejected token means the connection may be working but access
was refused. We must not disable HTTPS checks to fix that kind of error.

CI needs the Receipt token to access the cache. That simplifies each job, but exposes
the API to internet traffic. It does not make the server resistant to every
kind of overload. Monitoring is still a next step.

## Watch the test, not the old PASS message

![A second empty local cache can reuse the first client's saved result.](../assets/reviews/deployment-replay.svg)

First the client misses, runs its tests and uploads success. A second invocation
starts with another empty local cache, fetches that success and prints the saved
output. Both display PASS. To tell whether the test actually ran, our small
fixture appended to a separate execution marker. The marker stayed at one after
replay. Changing the source made the test execute again and fail.

Receipt’s own CI also keeps a separate job that always executes its tests with
a disposable PostgreSQL database. It enables Go’s race detector, which reports
unsynchronized access to shared memory in the executions it observes. A cached answer from the
program under test must not be the only evidence that the program works.
If the HTTPS readiness check fails, the cached job chooses local execution;
it does not declare the tests passed just because the cache is unavailable.

## One server is simple, but can still fail halfway

One droplet keeps the proxy, application, database and files close together.
We can learn to operate that arrangement before adding communication between
several storage machines. It also means a droplet failure affects all four.

![A failed database save does not undo a file already written.](../assets/reviews/deployment-partial-save.svg)

Suppose output file X was saved, then its database record failed. X remains,
but there is no usable result pointing to it. Cleanup may later remove it if
still unreferenced. A database rollback cannot undo a filesystem write.
HTTPS protects the connection; it does not make these two saves one operation.

This is also why recovery must include database records and output files.
We tested an encrypted backup by uploading it to [R2](../GLOSSARY.md#r2), downloading that actual
object, and restoring it into a separate test database and directory. R2 holds
the backup, not Receipt's live output files.

Imagine the droplet disappears. A backup kept only on that droplet disappears
with it. The R2 copy gives us the data elsewhere; the private recovery key on
the Mac lets us decrypt it. Losing that key would make the copy unusable.

```text
Droplet: pause → copy database + files → restart → encrypt → R2
                                                           │
Mac:                     private key + downloaded copy ←────┘
                                      │ decrypt
Test on droplet: separate database + copied files → fetch saved results
Live Receipt:   original database + original files stay in place
```

The pause kept the database and files from changing while we copied them.
The service resumes before encryption and transfer. The restored server was
checked by fetching its saved results. We then removed the temporary restored data.

This exercise reused the existing machine and its PostgreSQL setup. It did not
test rebuilding a new machine. For this small learning deployment, we chose one
recovery exercise rather than a nightly schedule. Monitoring is also deferred.
See the [recovery record](../DIGITALOCEAN-SETUP.md#backup-and-recovery)
for the archive location, recovery steps and remaining limits.

## Read further

Credential renewal is manual. See the [CI guide](../RECEIPT-CI.md#secrets-and-renewal)
for the procedure and the [operator guide](../DIGITALOCEAN-SETUP.md) for recovery.

## Check your understanding

<!-- questions:start -->

### What do DNS, HTTPS and Caddy each do?

**Level:** Newcomer

A CI job contacts `https://receipt.sidsharma.dev`. Which part finds the server, which part protects the connection, and which part checks the CI job's token?

<details>
<summary>Answer</summary>

DNS turns the name `receipt.sidsharma.dev` into the droplet's IP address; Cloudflare answers that lookup but does not carry the cache request. HTTPS encrypts the connection and lets the client check the certificate for that hostname. Caddy, a web server on the droplet, ends the HTTPS connection and forwards the request to Receipt at `127.0.0.1:7777` on the same machine; this forwarding role is called a reverse proxy. Receipt itself checks the token and its permissions. A rejected token means the connection worked and access was refused, so the fix is a valid credential, not a new certificate.

**Learn more:** [9. Follow a request to the deployed server](../guide/09-follow-a-request-to-the-deployed-server.md) · [Encryption is not permission](../guide/09-follow-a-request-to-the-deployed-server.md#encryption-is-not-permission) · [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions)

</details>

### What the client enforces about the connection

**Level:** Practitioner

Receipt itself speaks only plain HTTP. Where is TLS terminated in the DigitalOcean deployment, and what does the client refuse in order to protect its token?

<details>
<summary>Answer</summary>

On the droplet, Receipt listens on `127.0.0.1:7777`. Caddy terminates HTTPS for `receipt.sidsharma.dev`, obtains and renews its certificate, and forwards requests to that loopback port. Receipt's own port and PostgreSQL are not reachable from the internet.

`Remote.requestPath` refuses any remote URL that is not `https`, except `http` to `localhost`, `127.0.0.1` or `::1`. It also refuses URLs with a user name, a query string or a fragment. Its HTTP client never follows redirects, so the bearer token is not sent on to another host, and it has a 10-second timeout. An expired or revoked token is an application refusal (HTTP 401) on a working HTTPS connection, so the fix is a new credential, not a different certificate or an open database port.

**Learn more:** [Encryption is not permission](../guide/09-follow-a-request-to-the-deployed-server.md#encryption-is-not-permission) · [Requests and permissions](../DESIGN-DEFENSE.md#requests-and-permissions) · [`Remote.requestPath`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L38-L61)

</details>

### A backup beside the live data

**Level:** Expert

The server stores a backup on its own disk, then the entire server is lost. What does an external encrypted copy change, and what must the operator still have?

<details>
<summary>Answer</summary>

The external copy can survive losing the server. The operator still needs the private recovery key to decrypt it, and must restore both database records and output files. Having the archive does not by itself establish that a fresh machine can run the restored service.

**Learn more:** [Chapter 9](../guide/09-follow-a-request-to-the-deployed-server.md)

</details>

<!-- questions:end -->
