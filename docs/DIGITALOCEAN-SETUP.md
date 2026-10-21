# Operate the shared server

This guide describes the checked-in deployment configuration. Verify the running
machine before changing it; a document is not a live health check. Application
installation does not grant access to this privately administered cache.

## Request path

Clients connect to `https://receipt.sidsharma.dev`. Cloudflare supplies DNS;
Caddy handles HTTPS and forwards to Receipt on `127.0.0.1:7777`. PostgreSQL and
Receipt's own listener stay private to the host.

The [service unit](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/receipt.service)
runs Receipt as a separate `receipt` user with PostgreSQL storage, scoped
credentials, request logs and two admitted storage operations. Authentication has
its own admission slots. The [Caddyfile](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/Caddyfile)
contains the public hostname and local forwarding address.

Administrative SSH access uses the existing operator key and `sid` account.
Application clients use HTTPS and a Receipt credential, not that SSH key.
Protect administrative access separately from application permissions.

## Inspect before changing

On the server, check the service, recent errors and listening sockets:

```sh
sudo systemctl status receipt caddy postgresql
sudo journalctl -u receipt -n 50 --no-pager
sudo ss -lntp
```

Logs may include output outside the redacted request summaries. Review them
before sharing. Do not print environment files or tokens into a diagnostic report.

The database and output files must remain a matched pair. Shared storage binds
the database to the directory through store identities. Never delete `.store-id`
or `.lifecycle.lock` to make a failed check disappear.

## Storage maintenance

Supply the service's database settings securely and select its actual cache
directory. These examples inspect work before applying it:

```sh
receipt shared stats --cache-dir /var/lib/receipt/cache
receipt shared limits --cache-dir /var/lib/receipt/cache
receipt shared prune --cache-dir /var/lib/receipt/cache --older-than 720h
receipt shared gc --cache-dir /var/lib/receipt/cache
```

Add `--apply` only after reviewing the planned change and backup coverage.
Initialization may still create schema or identity records on first use even
when a maintenance operation defaults to a dry run.

Pruning removes eligible old, nonquarantined database records. Collection removes
unreferenced output afterward. It retains quarantine evidence and refuses unknown
paths, symlinks or malformed records. Once a file has been deleted, retrying does
not restore it. The [storage reference](DESIGN-DEFENSE.md#locks-limits-and-cleanup)
explains the locks and accounting.

## Backup and recovery

Back up database records and output files together. A database-only backup can
point to output that is missing; a files-only backup cannot reconstruct which
results refer to it. Pause writers for the copy, then resume service before doing
slower encryption and off-machine transfer work.

An encrypted R2 backup has been restored into an isolated database and directory.
That exercise used the existing machine; rebuilding a new host and losing the
recovery key were not tested. Backup scheduling, monitoring and a second protected
key copy still need operator attention.

The [retained recovery inventory](https://github.com/sidsharma96/receipt/blob/66899768d6c0f21b869fdf8f23c3a3c3232cc51a/docs/DIGITALOCEAN-SETUP.md#13-october-one-off-r2-backup-and-restore)
identifies the existing encrypted object and private-key location. Those are
specific recovery materials, not sample paths to copy into a new deployment.
The [restore helper](https://github.com/sidsharma96/receipt/blob/main/deploy/digitalocean/restore-r2-drill.py)
also contains drill-specific paths; inspect it rather than running it blindly.

To decrypt a downloaded copy, use OpenSSL CMS with `-decrypt -binary -inform DER`,
the recovery key through `-inkey` and its certificate through `-recip`. Write
plaintext only into a new private directory. Verify the archive checksums before
restoring. R2 access alone cannot decrypt the archive; the private key is required.

Restore into isolated storage first. Preserve the configured directory identity
and check saved results through the restored server. Do not overwrite live files
as a way to test recovery. Restore database roles and service configuration as
well when rebuilding a host.

## Credentials and service limits

Use [credential administration](SERVICE-CREDENTIALS.md) to inspect expiry,
issue replacements and revoke old IDs. Follow the [CI renewal procedure](RECEIPT-CI.md#secrets-and-renewal)
for the workflow's secret. There is no automatic renewal.

Receipt's admission and storage budgets do not replace host disk limits,
connection controls or monitoring. A single-host failure affects the proxy,
application, database and output files together. Test recovery procedures before
relying on this setup for an important workload.
