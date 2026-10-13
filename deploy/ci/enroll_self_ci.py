"""Operator-only enrollment after the one-day experiment was cleaned up.

Refuses existing credentials. Renewal requires explicit revocation/removal first.
"""
import json
import subprocess

from provision import REPO, remote, secret


def main():
    existing = json.loads(subprocess.check_output(
        ['gh', 'secret', 'list', '--repo', REPO, '--json', 'name']))
    if any(item['name'].startswith('RECEIPT_SELF_CI_') for item in existing):
        raise RuntimeError('refusing to overwrite existing CI secrets')
    issued = json.loads(remote("sudo -u receipt env "
        "'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' "
        "/usr/local/bin/receipt auth issue --cache-dir /var/lib/receipt/cache "
        "--subject receipt-self-ci --scopes read,publish,report --expires-in 720h"))
    # Print the public ID first so a failed secret upload can be revoked.
    print('Public credential metadata:', json.dumps({k: v for k, v in issued.items() if k != 'token'}), flush=True)
    secret('RECEIPT_SELF_CI_TOKEN', issued['token'].encode())


if __name__ == '__main__':
    main()
