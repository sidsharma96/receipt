# Private Droplet setup — 12 October 2026

## Current state, 15 October 2026

This file is a dated log. Later amendments supersede earlier sections, so read
this summary first. The sections below keep their original wording.

- Public access is `https://receipt.sidsharma.dev`. Caddy listens on TCP 443 and
  80 (80 only redirects), terminates TLS and forwards to Receipt on
  `127.0.0.1:7777`. Cloudflare provides DNS only. UFW allows SSH, 80 and 443.
  Receipt, PostgreSQL (5432) and Caddy's admin port (2019) are loopback only.
  This supersedes "No Caddy", "only SSH open" and the SSH-tunnel instructions
  in the initial setup sections.
- Receipt runs as `receipt serve --postgres --credentials --max-inflight 2
  --request-log` under [receipt.service](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service).
  It has two storage slots and, separately, two authentication slots. A
  request that arrives while both slots of either kind are busy gets HTTP 503,
  and its client runs tests locally.
- CI connects over HTTPS. The CI SSH key is retired and its GitHub secrets are
  deleted. The CI application token expires on 12 November 2026 at 03:28 UTC.
  Renewal is manual ([RECEIPT-CI.md](RECEIPT-CI.md#secrets-and-renewal)).
- Backups: one encrypted backup was restored in a drill on 13 October. There is
  no schedule, no second copy of the private recovery key, and no monitoring.
  This supersedes "Backups are not configured" only in that one drill exists.
- Still open: root key login is still allowed (`PermitRootLogin
  prohibit-password`), `sid` has passwordless sudo, the retired `receipt-ci`
  account still exists, and Caddy has not yet been observed renewing its
  certificate or restarting after a reboot.

## Initial private setup, 12 October 2026

Receipt is installed on the user's single Ubuntu 24.04 Droplet. This is an
initial private deployment, not a completed production or CI rollout.
The earlier Fly files remain an undeployed alternative.

## How to connect

Use the same private SSH key as before, but log in as `sid`:

```sh
ssh -i ~/.ssh/receipt_do_ed25519 sid@104.248.183.112
```

The account can administer the machine with passwordless `sudo`. Protect its
SSH private key: possessing it grants administrator access. Password SSH login
is disabled. Root key login remains available as recovery until the owner has
confirmed their own `sid` session works.

There is no public Receipt port. To connect from the Mac, leave this command
running in a terminal:

```sh
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:7777:127.0.0.1:7777 \
  -i ~/.ssh/receipt_do_ed25519 sid@104.248.183.112
```

Receipt clients then use `--remote http://127.0.0.1:7777`. Although that local
URL uses HTTP, SSH encrypts its trip over the internet. Receipt still requires
a valid service credential; SSH access does not replace application permission
checks. No lasting client credential has been issued yet.

Connection sequence: client → local tunnel port → encrypted SSH connection →
server's local Receipt listener → local PostgreSQL socket and output files.

## What is configured

- `sid`: SSH administrator; separate `receipt` account runs the service.
- UFW: incoming traffic denied except SSH, for IPv4 and IPv6. SSH is currently
  allowed from any source to avoid locking out a changing client address. A
  narrower source rule can follow once access requirements are established.
- Receipt: systemd service, listening only on `127.0.0.1:7777`, enabled at boot.
  Output files live in `/var/lib/receipt/cache`, accessible to the service user.
- PostgreSQL 16: local listeners only. Receipt uses a Unix socket and peer
  authentication: PostgreSQL checks the Linux username rather than a stored
  database password. The `receipt` database role owns its database but is not a
  superuser and cannot create roles or databases.
- DigitalOcean's existing metrics agent is active. Custom alerts have not been
  configured. The OS unattended-upgrades status is checked separately.
- No Caddy, public HTTP/HTTPS listener or certificate yet. There is no domain.

The systemd unit is [receipt.service](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service).
For example, `User=receipt` separates the service from the administrator, and
`ProtectSystem=strict` makes most system paths read-only to the service. It can
still write its designated data directory. These restrictions do not turn it
into an isolated host or protect against an administrator.

## Checks and limits

Separate SSH login and `sudo` as `sid` passed before SSH settings and firewall
rules were applied. A new connection passed afterwards. SSH configuration and
sudoers syntax checks passed. The systemd unit validated and started; an
unauthenticated identity request returned 401. Listening sockets show PostgreSQL
and Receipt on loopback only, with SSH as the public listener.

Linux amd64 cross-compilation succeeded. Native Mac `go test ./...` encountered
a loader failure (`missing LC_UUID`) in three packages; `CGO_ENABLED=0 go test
./...` passed. This is not a full live PostgreSQL integration/race-suite result.

The checked-in identity smoke script creates a five-minute read credential,
checks its identity, and revokes it without printing or saving its secret. It
does not test cache publication or replay. Its retained credential record counts
toward the credential budget, so it should not be run as a periodic health check.
On this deployment it returned 200 before revocation and 401 afterwards. Receipt
also restarted successfully and reported an empty cache through its database.
Unattended upgrades are enabled; this does not mean all pending updates have
already been applied. All 11 local documentation/template tests passed.

The Droplet exposes `/dev/kvm`; no Firecracker guest was booted. No reboot,
backup/restore exercise, real workload remote hit, CI integration or performance
measurement has been completed. Backups are not configured. Do not yet rely on
this server as the only copy of important records. OS updates remain to be
reviewed/applied; package installation alone does not fully patch the host.

## Next checks

Confirm the owner's `sid` login, issue a client credential through a private
terminal, and demonstrate a real miss followed by remote replay. Then establish
an encrypted off-machine backup and restore it onto disposable storage. Choose
a domain before adding Caddy and public HTTPS. A CI job should not receive this
administrator SSH key as a substitute for a properly scoped application setup.

No additional cloud resources were purchased. No Git commit or push was made.

## 12 October amendment: first remote-cache experiment passed

A temporary Go project on the Mac contains a function returning 42 and a test
that expects 42. Each actual test execution appends a line to a separate marker
file. That file is an observation instrument outside the checkout, not an input
used to decide whether the test passes. Its contents are deliberately not cached.

All three invocations used `go test -count=1 -v .`, disabling Go's own result
cache, through a newly built Receipt client. Each invocation started with a
different, previously nonexistent local Receipt cache directory. They used the
same checkout path and a deliberately small, unchanged environment. The server
was reached through a temporary SSH tunnel on local port 18777.

| Step | Receipt outcome | Actual executions so far | Exit status |
| --- | --- | ---: | ---: |
| Original code, first empty local cache | Miss; execute and upload | 1 | 0 |
| Same code, second empty local cache | Remote hit; replay saved output | 1 | 0 |
| Change function to return 43, third empty local cache | Miss; execute and fail | 2 | 1 |

The successful key began `1ceddd490f97`; authenticated HTTP fetches confirmed it
existed on the server after both successful invocations. The changed-source key
began `0872399a8e42`. Receipt reported that failed results are not cached, and
the third local cache contained no result entry. Replayed output still shows
the original test's PASS lines: the unchanged execution counter is the separate
evidence that the test did not run again.

The fifteen-minute experiment credential had read/publish/report permissions.
Its secret stayed in process memory and the trusted test process's environment;
it was not printed or written to a credential file. It was revoked afterwards,
and the experiment's SSH tunnel was closed. The small successful result remains
in the shared cache; temporary local fixtures remain under
`/tmp/receipt-remote-experiment.axstw7` for inspection. No existing project files
or existing cache directories were cleared.

This establishes remote reuse from one controlled Mac environment, not reuse
between CI jobs or machines with differing checkout paths and toolchains. It
is not a speed benchmark, backup test, reboot test or proof of deterministic
tests. Earlier outstanding-work notes are historical: real remote reuse is now
checked for this fixture; backups, public HTTPS and CI remain outstanding.

## 12 October amendment: encrypted backup and isolated restore

Before OS maintenance, Receipt was stopped briefly to pause HTTP writes. No CLI
maintenance writers were running. The backup contains a PostgreSQL custom dump,
role definitions without passwords, Receipt's complete data directory including
its binding marker, the executable and selected service/SSH configuration files.
Receipt was restarted after capture. This is not a complete machine image:
rebuilding a host still requires packages, users, ownership, SSH and firewall setup.

The encrypted copy on the Mac is
`/Users/siddharthsharma/receipt-backup-2026-10-12.sa69ap/receipt-backup.cms`.
OpenSSL CMS uses AES-256-GCM and an RSA recovery certificate. The adjacent
`recovery-key.pem` is required to decrypt it. The directory is mode 0700 and
the archive and private key are mode 0600. The key has no passphrase and is
beside the archive: this protects an archive copied alone, not a compromised
Mac account. Keep a copy of the key in secure separate custody before relying
on disaster recovery. Neither archive nor key belongs in Git. Losing the Mac
and its only key copy would lose this backup.

The drill decrypted the actual Mac copy and streamed it over SSH into separate
root-only storage at `/var/backups/receipt-restore.yCuUL5`. The database dump
and file archive matched their source copies byte for byte. PostgreSQL restored
into `receipt_restore_20261012`, not the live database. A temporary Receipt
server used the restored executable and files on local port 17778.

Receipt binds its database to an absolute directory path. A private Linux mount
namespace let the drill see restored files at `/var/lib/receipt` without changing
the live service's view of that path. Existing PostgreSQL and Linux/database
roles were reused: this was not a brand-new-host or role-recreation test.

Sequence: pause writes → capture database and files → resume service → encrypt
on Mac → decrypt into separate storage → restore database → fetch saved result.

Both original and restored fetches returned HTTP 200, 2,076 bytes and SHA-256
`71d261af226f705a24f34fc7e2c316d7717aade5c068e3b85260588f9bb8222a`.
Temporary read credentials were revoked. An initial drill failed because the
service inherited an inaccessible working directory; explicitly selecting
`/var/lib/receipt` fixed it. The live systemd service already had that setting.
The temporary server was stopped afterwards.

The root-only raw server backup remains at `/var/backups/receipt-manual.twcex6`.
Restore files remain for inspection. Scripts in `deploy/digitalocean` record
the procedure; the dated restore script is specific to these paths, not a general
restore tool. There is no scheduled backup, retention policy or external backup
service yet. This test establishes recovery of the one saved result, not every
failure mode or full disaster recovery.

After the drill, the temporary `receipt_restore_20261012` database was dropped;
it can be recreated from the retained dump. The live database was not replaced.

### OS update and reboot result

Installed 29 package upgrades and six new kernel-related packages, with no
package removals. The Droplet rebooted from kernel `6.8.0-142-generic` into
`6.8.0-146-generic`. SSH as `sid`, Receipt, PostgreSQL and the monitoring agent
recovered. UFW remained enabled with only SSH allowed; Receipt and PostgreSQL
still listen locally. The saved-result fetch returned the same hash shown above.
The subsequent upgrade preview reported zero pending upgrades. SSH password
authentication remains disabled. Backup/restore and reboot are now tested within
this scope; earlier outstanding-work statements remain historical records.

### Remote editor setup (2026-10-12)

Ghostty on the Mac needs a matching terminal description on Ubuntu so shell
programs know how to clear and redraw the screen. The Droplet lacked
`xterm-ghostty`. Installed the description bundled with the Mac's Ghostty into
`/usr/share/terminfo`; `tput clear` now emits the expected escape sequence.
Bash already binds Ctrl+L to clear-screen. Reconnect SSH and try the key again;
the actual interactive keypress has not been verified by the user yet.

Installed Neovim 0.11.5 for `sid` at `~/.local/bin/nvim`. Copied the Mac's
`init.lua`, `lazy-lock.json` and Rust filetype settings without changing the
local originals. All 21 plugin checkouts match the lockfile. Headless checks
loaded Oxocarbon and Telescope's native search extension, and confirmed Go's
literal tabs with display width four. Syntax parsers are installed for the
15 configured languages. No existing remote Neovim config needed replacing.

Language tools are installed through Mason, rather than copied from macOS:
Linux needs its own executables. These tool versions are not promised to match
the Mac's versions. Mason's headless installer aborted the Go language-server
installation; installed `gopls` v0.23.0 directly with `go install` into
`~/.local/bin` instead. Rust/Zig compilers and the Claude CLI are not installed;
their dependent features are therefore incomplete. No login sessions or private
AI credentials were copied. Fonts still come from Ghostty on the Mac. Clipboard
integration and a full interactive editor session have not been verified.

This setup belongs to `sid`, not root. Reconnecting also refreshes the login
PATH so `nvim` resolves to the installed executable. Receipt and PostgreSQL
remained active during the editor checks.

## 13 October amendment: public HTTPS with Caddy

A client can now fetch a saved result without an SSH tunnel. Cloudflare DNS
points `receipt.sidsharma.dev` directly to `104.248.183.112` (DNS only).
The client connects to Caddy, which checks and decrypts the HTTPS connection,
then forwards the request to Receipt on the same machine:

```text
Mac or CI client
    | HTTPS, port 443: encrypted across the internet
    v
Caddy on receipt-1
    | HTTP, 127.0.0.1:7777: stays inside this machine
    v
Receipt checks the credential and its permissions
    | only an allowed request reaches storage
    v
Local PostgreSQL + cached output files
```

Caddy is a reverse proxy: it accepts the public connection and passes requests
to another server. It does not replace Receipt's permission checks. The actual
[Caddyfile](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/Caddyfile) contains:

```caddyfile
receipt.sidsharma.dev {
    reverse_proxy 127.0.0.1:7777
}
```

The hostname lets Caddy obtain a certificate for this site. The loopback address
keeps Receipt's own listener inaccessible directly from another machine. Caddy
2.11.7 was installed from its official stable APT repository and enabled at boot.
It obtained a Let's Encrypt certificate and schedules renewal automatically.
The first issuance passed; a future renewal has not yet been observed.

UFW now allows TCP ports 80 and 443 as well as SSH. Port 80 redirects ordinary
HTTP requests to HTTPS and supports certificate validation. Clients must start
with an HTTPS URL: a redirect cannot protect a secret already sent over HTTP.
Receipt port 7777, PostgreSQL port 5432 and Caddy's administration port 2019
remain loopback-only. UDP 443 is not allowed; this setup uses HTTPS over TCP.
Cloudflare is providing DNS, not proxying or filtering these requests. Anyone
can reach the public HTTPS listener, but needs a valid credential for protected
operations. This adds internet exposure; it is not a denial-of-service guarantee.

### What was checked

From the Mac, normal DNS lookup and certificate verification succeeded without
disabling TLS checks. HTTP returned a 308 redirect to the same HTTPS path.
The [HTTPS smoke script](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/check_https.py) checked:

- Missing and invalid credentials: HTTP 401.
- A temporary read credential: identity returned HTTP 200 with the expected ID.
- The saved experiment result: HTTP 200, 2,076 bytes and SHA-256
  `71d261af226f705a24f34fc7e2c316d7717aade5c068e3b85260588f9bb8222a`,
  matching the earlier recovery check.
- After revocation, that temporary credential returned HTTP 401.

The script pins the known IP while retaining hostname and certificate checks;
a separate curl request verified normal DNS resolution. The secret was kept in
memory and passed to curl through stdin, not its command-line arguments. The
revoked credential's metadata remains within the existing retained-row budget.
This is a read/authentication smoke test, not a fresh HTTPS upload/replay test,
load test or full permission-matrix test. No application binary changed.

CI workflows and their SSH access have not changed. Next, test a fresh upload
and replay over HTTPS before moving CI away from its tunnel. Existing tunnels
still work. No new permanent client credential was issued.

The installed file is `/etc/caddy/Caddyfile`; the package's original default was
preserved at `/etc/caddy/Caddyfile.pre-receipt`. To return to private-only access,
stop Caddy and remove the two added UFW rules (`80/tcp` and `443/tcp`), leaving
SSH and Receipt untouched. Do not restore the package's default website and
leave it publicly listening as a substitute for closing access.

No Git commit or push was made. Automatic renewal, a reboot with Caddy installed,
external monitoring and hostile-traffic capacity remain untested.

### 13 October follow-up: fresh HTTPS upload and replay

The [fresh-fixture check](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/experiment_https.py) built the
local Receipt client and used a new temporary Go project. It used normal DNS,
verified HTTPS and a fifteen-minute read/publish/report credential.

| Step | Result | Actual test executions |
| --- | --- | ---: |
| First empty local cache | Miss `e0187889417a`; run and upload | 1 |
| Second empty local cache | Hit for the same key; replay | 1 |
| Change function from 42 to 43 | Miss `3a5d9184d681`; test fails | 2 |

Each actual test appended to a separate marker outside the checkout. The count,
not replayed PASS text, establishes non-execution in step two. Go's own test
result cache was disabled with `-count=1`. The temporary credential was revoked
and local temporary files were removed; the uploaded successful result remains
in the server cache. No exercise implementation was touched.

Receipt's GitHub rollout then demonstrated a miss and same-commit remote hit on
fresh runners in [run 38429263439](https://github.com/sidsharma96/receipt/actions/runs/38429263439).
The independent PostgreSQL/race suite passed both attempts. See
[migration PR #2](https://github.com/sidsharma96/receipt/pull/2) for the workflow
and helper changes. They were prepared in `/tmp/receipt-https-ci-20261013` to
preserve this checkout's pre-existing changes.

Main run [38429805089](https://github.com/sidsharma96/receipt/actions/runs/38429805089)
passed both jobs. The CI-only authorized key was moved to
`/home/receipt-ci/.ssh/authorized_keys.retired-20261013` (recoverable but no longer
accepted), and the two GitHub SSH secrets were deleted. Only the application
token remains. Administrator SSH access was unchanged. A second main run after
cleanup passed both jobs and replayed key `3fc8bbae451b` over HTTPS in a 0.49-second
Receipt invocation. This is one observed invocation, not a general speed claim.

### 13 October: one-off R2 backup and restore

The user chose one recovery exercise instead of nightly backups. No schedule,
retention deletion policy or monitoring workflow was installed.

The [snapshot script](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/backup-r2-drill.py) stopped Receipt,
checked that no other Receipt process was running, dumped PostgreSQL, and copied
the output files, binary, service definition, Caddy configuration and selected
SSH/sudo configuration. It restarted Receipt before encryption and upload.
The observed stop/start interval was **1.306 seconds**. This is not continuous
measurement of the public endpoint's outage or a future downtime bound.
No independent CLI writers were started during the snapshot.

OpenSSL CMS encrypted the archive with AES-256-GCM using the existing public
recovery certificate. Only the certificate went to the droplet; its matching
private key stayed on the Mac. R2 credentials were excluded from the archive.
The roles dump excludes PostgreSQL role passwords. Caddy certificate state was
not backed up; a replacement host would need certificate issuance again.

The [one-off transfer helper](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/r2_backup_object.py) uploaded
and then downloaded the object over verified HTTPS. It refuses redirects and
uses a conditional upload to avoid replacing an existing object. Its credentials
come from `/etc/receipt/backup.env`, owned by root with mode 600. It has no delete
operation, multipart support, schedule or automatic retry. It reads at most
64 MiB into memory; this is a small-drill helper, not a general backup service.

Retained recovery material:

- Bucket/object: `receipt-backups/drills/receipt-20261013-3fr7fj87.cms`.
- Encrypted local copy: `/Users/siddharthsharma/receipt-r2-recovery-20261013.y0PxWL/from-r2.cms`.
- Private key: `/Users/siddharthsharma/receipt-backup-2026-10-12.sa69ap/recovery-key.pem`.
- Public certificate: the adjacent `recovery-cert.pem`.
- Object size: 5,796,416 bytes.
- SHA-256: `d5db44f21d608fd701c2d0dfc33c92fb22c5b5fd0ab130bda8c1381205931815`.

To decrypt a retrieved copy, use OpenSSL CMS with `-decrypt -binary -inform DER`,
the private key via `-inkey`, and public certificate via `-recip`. Write its output
to a new file inside a mode-700 directory. The decrypted tar contains
`receipt.dump`, `roles.sql`, `server.tar.gz` and `checksums.json`. Verify the
checksums before restoring. Never restore directly over live data for a drill.
The private key is essential: R2 access alone cannot decrypt the archive.

The [restore script](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-r2-drill.py) records this
specific drill's paths and database name; it is not a generic recovery installer.
The downloaded archive was decrypted on the Mac, then sent through SSH for the
isolated test. A private Linux mount namespace gave the test server its own view
of the restored paths while the live server kept its original files. The test
server listened only on loopback port 17779, with a separate PostgreSQL database.

Evidence:

- Upload and download returned HTTP 200; ciphertext matched byte for byte.
- Decryption succeeded and all three inner-file checksums matched.
- Restored table contents matched live contents before issuing a test token:
  10 entries, 10 evidence rows, one storage-policy row and nine credential rows.
- All 10 nonquarantined entries returned HTTP 200 from the restored server.
- The known result had 2,076 bytes and SHA-256
  `71d261af226f705a24f34fc7e2c316d7717aade5c068e3b85260588f9bb8222a`.
- The test credential was revoked, the test server stopped, and temporary
  database `receipt_restore_r2_20261013` was dropped. This drill's plaintext
  staging files and restored directory were removed, including the Mac's
  decrypted tar. Earlier manual backup directories were not removed.
- Live Receipt remained active after cleanup; unauthenticated public HTTPS
  identity returned the expected 401. No listener remained on port 17779.

Limits: existing Linux/PostgreSQL roles were reused, rather than rebuilt from
`roles.sql`. This does not establish new-host recovery, all credential behaviours
after recovery, or recovery from a corrupted archive or lost key. Ordinary file
removal is not secure erasure from SSDs or provider snapshots. Keep a separately
protected recovery-key copy; the current Mac is its only verified location.
The uploader's write-capable credentials are not protection against deletion
by a compromised administrator. No immutable retention policy was configured.
