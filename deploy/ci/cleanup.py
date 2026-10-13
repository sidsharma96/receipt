"""Revoke this experiment's application access and disable its SSH key."""
import json
import subprocess

from provision import REPO, remote

CLI = ("sudo -u receipt env "
       "'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' "
       "/usr/local/bin/receipt auth ")


def main():
    active_key = remote("if sudo test -f /home/receipt-ci/.ssh/authorized_keys; then "
                        "sudo cat /home/receipt-ci/.ssh/authorized_keys; fi").decode().strip()
    if active_key and (len(active_key.splitlines()) != 1 or
                       active_key.split()[-1] != "receipt-github-experiment"):
        raise RuntimeError("refusing to disable an SSH key belonging to another workflow")
    records = json.loads(remote(CLI + "list --cache-dir /var/lib/receipt/cache"))
    for record in records:
        if record["subject"] == "github-actions-experiment":
            credential_id = record["id"]
            assert len(credential_id) == 32 and all(c in "0123456789abcdef" for c in credential_id)
            remote(CLI + "revoke --cache-dir /var/lib/receipt/cache --id " + credential_id)
    remote("if sudo test -f /home/receipt-ci/.ssh/authorized_keys; then "
           "sudo test ! -e /root/receipt-ci-disabled-key-20261012 && "
           "sudo mv /home/receipt-ci/.ssh/authorized_keys /root/receipt-ci-disabled-key-20261012; fi")
    for name in ("RECEIPT_CI_SSH_KEY", "RECEIPT_CI_KNOWN_HOSTS", "RECEIPT_CI_TOKEN"):
        subprocess.run(["gh", "secret", "delete", name, "--repo", REPO], check=True)
    print("Experiment credentials revoked, SSH key disabled, GitHub secrets removed.")


if __name__ == "__main__":
    main()
