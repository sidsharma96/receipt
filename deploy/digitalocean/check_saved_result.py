"""Run as root. Fetch the known experiment result without printing secrets/data."""
import hashlib
import json
import os
import subprocess
import urllib.request

database = os.environ.get("CHECK_DATABASE", "receipt")
port = os.environ.get("CHECK_PORT", "7777")
assert database in ("receipt", "receipt_restore_20261012")
assert port in ("7777", "17778")
cli = ["runuser", "-u", "receipt", "--", "env",
       "RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=" + database + " sslmode=disable",
       "/usr/local/bin/receipt", "auth"]
issued = json.loads(subprocess.check_output(cli + ["issue", "--cache-dir",
    "/var/lib/receipt/cache", "--subject", "recovery-check", "--scopes", "read",
    "--expires-in", "5m"]))
try:
    key = "1ceddd490f9799adbc8950b5a901fa6e1306b9b549d876c91395f787a6bf1a88"
    req = urllib.request.Request("http://127.0.0.1:" + port + "/v1/entries/" + key,
        headers={"Authorization": "Bearer " + issued["token"]})
    with urllib.request.urlopen(req, timeout=15) as response:
        data = response.read()
        print("HTTP", response.status, "bytes", len(data), "sha256", hashlib.sha256(data).hexdigest())
finally:
    subprocess.run(cli + ["revoke", "--cache-dir", "/var/lib/receipt/cache",
        "--id", issued["id"]], check=True, stdout=subprocess.DEVNULL)
