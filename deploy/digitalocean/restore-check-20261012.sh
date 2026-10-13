#!/bin/sh
# One-off drill. Run as root in a private mount namespace, not the host namespace.
# The restored database must already exist; this never restores over production.
set -eu
test "$(readlink /proc/self/ns/mnt)" != "$(readlink /proc/1/ns/mnt)"
restore_dir=/var/backups/receipt-restore.yCuUL5
mount --bind "$restore_dir/extracted/var/lib/receipt" /var/lib/receipt
mount --bind "$restore_dir/extracted/usr/local/bin/receipt" /usr/local/bin/receipt
cd /var/lib/receipt
runuser -u receipt -- env 'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt_restore_20261012 sslmode=disable' \
  /usr/local/bin/receipt serve --postgres --credentials --listen 127.0.0.1:17778 --cache-dir /var/lib/receipt/cache --max-inflight 2 &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true; wait "$server_pid" || true' EXIT
sleep 2
CHECK_DATABASE=receipt_restore_20261012 CHECK_PORT=17778 python3 /home/sid/check_saved_result.py
