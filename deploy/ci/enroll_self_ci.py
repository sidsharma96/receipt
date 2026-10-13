"""Operator-only enrollment after the one-day experiment was cleaned up.

Refuses existing credentials. Renewal requires explicit revocation/removal first.
"""
import datetime
import json
from pathlib import Path
import subprocess
import tempfile

from provision import REPO, remote, secret


def main():
    existing = json.loads(subprocess.check_output(
        ['gh', 'secret', 'list', '--repo', REPO, '--json', 'name']))
    if any(item['name'].startswith('RECEIPT_SELF_CI_') for item in existing):
        raise RuntimeError('refusing to overwrite existing CI secrets')
    remote('getent passwd receipt-ci >/dev/null && '
           'sudo test ! -e /home/receipt-ci/.ssh/authorized_keys')
    expected = Path(__file__).with_name('receipt-ci-ssh.conf').read_bytes()
    actual = remote('sudo cat /etc/ssh/sshd_config.d/10-receipt-ci.conf')
    if actual != expected:
        raise RuntimeError('review changed SSH restrictions before enrollment')
    expires = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=30)
    with tempfile.TemporaryDirectory(prefix='receipt-self-enroll-') as directory:
        key = Path(directory) / 'key'
        subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-C',
                        'receipt-self-ci', '-f', str(key)], check=True)
        options = ('restrict,port-forwarding,permitopen="127.0.0.1:7777",expiry-time="'
                   + expires.strftime('%Y%m%d%H%M%SZ') + '" ')
        remote('sudo tee /home/receipt-ci/.ssh/authorized_keys >/dev/null',
               (options + key.with_suffix('.pub').read_text()).encode())
        remote('sudo chmod 644 /home/receipt-ci/.ssh/authorized_keys')
        host = remote('sudo cat /etc/ssh/ssh_host_ed25519_key.pub').decode().split()
        issued = json.loads(remote("sudo -u receipt env "
            "'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' "
            "/usr/local/bin/receipt auth issue --cache-dir /var/lib/receipt/cache "
            "--subject receipt-self-ci --scopes read,publish,report --expires-in 720h"))
        secret('RECEIPT_SELF_CI_SSH_KEY', key.read_bytes())
        secret('RECEIPT_SELF_CI_KNOWN_HOSTS', ('104.248.183.112 ' + ' '.join(host[:2]) + '\n').encode())
        secret('RECEIPT_SELF_CI_TOKEN', issued['token'].encode())
        print('Public credential metadata:', json.dumps({k: v for k, v in issued.items() if k != 'token'}))
        print('SSH authorization expires:', expires.isoformat())


if __name__ == '__main__':
    main()
