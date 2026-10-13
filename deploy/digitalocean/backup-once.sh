#!/bin/sh
# Run as root during an operator-controlled maintenance window: no CLI writers.
set -eu
umask 077
backup_dir=$(mktemp -d /var/backups/receipt-manual.XXXXXX)
trap 'systemctl start receipt' EXIT
systemctl stop receipt
runuser -u postgres -- pg_dump -Fc receipt > "$backup_dir/receipt.dump"
runuser -u postgres -- pg_dumpall --roles-only --no-role-passwords > "$backup_dir/roles.sql"
tar -C / -czf "$backup_dir/server.tar.gz" var/lib/receipt usr/local/bin/receipt etc/systemd/system/receipt.service etc/ssh/sshd_config.d/00-receipt-access.conf etc/sudoers.d/receipt-sid
sha256sum "$backup_dir/receipt.dump" "$backup_dir/server.tar.gz" "$backup_dir/roles.sql" > "$backup_dir/SHA256SUMS"
systemctl start receipt
trap - EXIT
printf '%s\n' "$backup_dir"
