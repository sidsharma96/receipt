"""Manual snapshot for the October 2026 R2 drill. Run as root; no CLI writers.

No scheduling. Production is stopped only for snapshot creation. On ordinary
errors, finally restarts it. Power loss/SIGKILL cannot run that cleanup.
"""
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile
import time


def run(*args, **kwargs):
    return subprocess.run(args, check=True, timeout=120, **kwargs)


os.umask(0o077)
run('systemctl', 'is-active', '--quiet', 'receipt')
root = pathlib.Path(tempfile.mkdtemp(prefix='receipt-r2-drill-', dir='/var/backups'))
print(root, flush=True)
start = time.monotonic()
try:
    run('systemctl', 'stop', 'receipt')
    # Refuse a snapshot if a separate Receipt CLI/server is still running.
    if subprocess.run(['pgrep', '-x', 'receipt'], stdout=subprocess.DEVNULL).returncode != 1:
        raise RuntimeError('Other Receipt process detected')
    with (root / 'receipt.dump').open('xb') as output:
        run('runuser', '-u', 'postgres', '--', 'pg_dump', '-Fc', 'receipt', stdout=output)
    with (root / 'roles.sql').open('xb') as output:
        run('runuser', '-u', 'postgres', '--', 'pg_dumpall', '--roles-only', '--no-role-passwords', stdout=output)
    run('tar', '-C', '/', '-czf', str(root / 'server.tar.gz'),
        'var/lib/receipt', 'usr/local/bin/receipt', 'etc/systemd/system/receipt.service',
        'etc/caddy/Caddyfile', 'etc/ssh/sshd_config.d/00-receipt-access.conf', 'etc/sudoers.d/receipt-sid')
finally:
    run('systemctl', 'start', 'receipt')
    run('systemctl', 'is-active', '--quiet', 'receipt')
    print(f'stop/start interval: {time.monotonic() - start:.3f} seconds', flush=True)
hashes = {name: hashlib.sha256((root / name).read_bytes()).hexdigest()
          for name in ('receipt.dump', 'roles.sql', 'server.tar.gz')}
(root / 'checksums.json').write_text(json.dumps(hashes, indent=2) + '\n')
run('tar', '-C', str(root), '-cf', str(root / 'backup.tar'),
    'receipt.dump', 'roles.sql', 'server.tar.gz', 'checksums.json')
run('openssl', 'cms', '-encrypt', '-binary', '-aes-256-gcm', '-outform', 'DER',
    '-in', str(root / 'backup.tar'), '-out', str(root / 'backup.cms'),
    '/home/sid/receipt-recovery-cert.pem')
print('Encrypted snapshot ready; plaintext remains root-only pending successful restore.', flush=True)
