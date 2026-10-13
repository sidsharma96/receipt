"""Operator-only one-day experiment setup; secrets go to GitHub via stdin."""
import datetime
import json
from pathlib import Path
import subprocess
import tempfile

SSH = ["ssh", "-o", "BatchMode=yes", "-i",
       str(Path.home() / ".ssh/receipt_do_ed25519"), "sid@104.248.183.112"]
REPO = "sidsharma96/receipt"


def remote(command, data=None):
    return subprocess.check_output(SSH + [command], input=data)


def secret(name, data):
    subprocess.run(["gh", "secret", "set", name, "--repo", REPO], input=data, check=True)


def main():
    # Refuse to replace an existing account/configuration or repository secrets.
    names = json.loads(subprocess.check_output(
        ["gh", "secret", "list", "--repo", REPO, "--json", "name"]))
    assert not any(x["name"].startswith("RECEIPT_CI_") for x in names)
    remote("! getent passwd receipt-ci && sudo test ! -e /etc/ssh/sshd_config.d/10-receipt-ci.conf")
    expiry = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=24)
    with tempfile.TemporaryDirectory(prefix="receipt-ci-provision-") as directory:
        key = Path(directory) / "key"
        subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C",
                        "receipt-github-experiment", "-f", str(key)], check=True)
        remote("sudo useradd --create-home --shell /usr/sbin/nologin receipt-ci")
        remote("sudo install -d -o root -g root -m 755 /home/receipt-ci/.ssh")
        options = ('restrict,port-forwarding,permitopen="127.0.0.1:7777",expiry-time="'
                   + expiry.strftime("%Y%m%d%H%M%SZ") + '" ')
        remote("sudo tee /home/receipt-ci/.ssh/authorized_keys >/dev/null",
               (options + key.with_suffix(".pub").read_text()).encode())
        remote("sudo chmod 644 /home/receipt-ci/.ssh/authorized_keys")
        config = Path(__file__).with_name("receipt-ci-ssh.conf").read_bytes()
        remote("sudo tee /etc/ssh/sshd_config.d/10-receipt-ci.conf >/dev/null", config)
        remote("sudo sshd -t && sudo systemctl reload ssh")
        # Read host key through the already verified administrative connection.
        hostkey = remote("sudo cat /etc/ssh/ssh_host_ed25519_key.pub").decode().split()
        known = ("104.248.183.112 " + " ".join(hostkey[:2]) + "\n").encode()
        issued = json.loads(remote("sudo -u receipt env "
            "'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' "
            "/usr/local/bin/receipt auth issue --cache-dir /var/lib/receipt/cache "
            "--subject github-actions-experiment --scopes read,publish,report --expires-in 24h"))
        secret("RECEIPT_CI_SSH_KEY", key.read_bytes())
        secret("RECEIPT_CI_KNOWN_HOSTS", known)
        secret("RECEIPT_CI_TOKEN", issued["token"].encode())
        print("Experiment secrets configured; SSH key expires:", expiry.isoformat())
        print("Revoke the github-actions-experiment credential and remove these secrets after testing.")


if __name__ == "__main__":
    main()
