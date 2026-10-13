"""Operator smoke check from the Mac; never prints or persists bearer tokens.

Uses the known server IP while retaining TLS hostname/certificate verification,
so stale local DNS does not prevent testing. Requires administrator SSH access.
Creates one five-minute read credential and revokes it in a finally block.
"""
import hashlib
import json
import subprocess
from pathlib import Path

HOST = "receipt.sidsharma.dev"
SSH = ["ssh", "-o", "BatchMode=yes", "-i",
       str(Path.home() / ".ssh/receipt_do_ed25519"), "sid@104.248.183.112"]
AUTH = ("sudo -u receipt env "
        "RECEIPT_DATABASE_URL='host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' "
        "/usr/local/bin/receipt auth ")


def request(path, token=None):
    config = ""
    if token:
        # JSON quoting safely escapes curl config strings. Token is stdin, not argv.
        config = "header = " + json.dumps("Authorization: Bearer " + token) + "\n"
    result = subprocess.run(
        ["curl", "--silent", "--show-error", "--connect-timeout", "10",
         "--max-time", "30", "--resolve", HOST + ":443:104.248.183.112",
         "--config", "-", "--write-out", "\n%{http_code}",
         "https://" + HOST + path], input=config.encode(),
        stdout=subprocess.PIPE, check=True)
    body, status = result.stdout.rsplit(b"\n", 1)
    return int(status), body


def main():
    assert request("/v1/identity")[0] == 401
    assert request("/v1/identity", "invalid-smoke-token")[0] == 401
    print("HTTPS: missing and invalid credentials rejected (401)")
    issued = json.loads(subprocess.check_output(SSH + [AUTH +
        "issue --cache-dir /var/lib/receipt/cache --subject https-deployment-smoke "
        "--scopes read --expires-in 5m"]))
    try:
        status, body = request("/v1/identity", issued["token"])
        assert status == 200
        assert json.loads(body)["id"] == issued["id"]
        print("HTTPS: valid credential identity accepted (200)")
        key = "1ceddd490f9799adbc8950b5a901fa6e1306b9b549d876c91395f787a6bf1a88"
        status, body = request("/v1/entries/" + key, issued["token"])
        assert status == 200
        digest = hashlib.sha256(body).hexdigest()
        assert digest == "71d261af226f705a24f34fc7e2c316d7717aade5c068e3b85260588f9bb8222a"
        print("HTTPS: saved result matches recovery check:", len(body), "bytes, sha256", digest)
    finally:
        # Only a validated public credential ID is interpolated into the SSH command.
        credential_id = issued["id"]
        assert len(credential_id) == 32 and all(c in "0123456789abcdef" for c in credential_id)
        subprocess.run(SSH + [AUTH + "revoke --cache-dir /var/lib/receipt/cache --id " +
                             credential_id], check=True, stdout=subprocess.DEVNULL)
    assert request("/v1/identity", issued["token"])[0] == 401
    print("HTTPS: temporary credential revoked and rejected (401)")


if __name__ == "__main__":
    main()
