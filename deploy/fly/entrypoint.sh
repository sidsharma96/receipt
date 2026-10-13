#!/bin/sh
set -eu
# Refuse to store durable data on the disposable root filesystem.
mountpoint -q /data || { echo 'receipt: /data must be a mounted persistent volume' >&2; exit 1; }
# A fresh Fly volume is root-owned. Change only our dedicated directory, never
# recursively change ownership of arbitrary existing volume contents.
if [ -L /data/cache ]; then
    echo 'receipt: cache directory must not be a symlink' >&2
    exit 1
fi
mkdir -p /data/cache
chown 10001:10001 /data/cache
chmod 700 /data/cache
# Replace the shell so Receipt receives shutdown signals directly as PID 1.
exec gosu receipt:receipt /usr/local/bin/receipt "$@"
