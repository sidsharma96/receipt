"""Run as root on the server. Creates then revokes a short-lived test credential.

No token is printed or persisted. This checks authentication, not result reuse.
"""
import json
import subprocess
import urllib.error
import urllib.request

CLI = ["runuser", "-u", "receipt", "--", "env",
       "RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable",
       "/usr/local/bin/receipt", "auth"]

issued = json.loads(subprocess.check_output(
    CLI + ["issue", "--cache-dir", "/var/lib/receipt/cache", "--subject",
           "deployment-smoke", "--scopes", "read", "--expires-in", "5m"]))
token = issued["token"]
request = urllib.request.Request("http://127.0.0.1:7777/v1/identity",
                                 headers={"Authorization": "Bearer " + token})
with urllib.request.urlopen(request, timeout=5) as response:
    identity = json.load(response)
    print("Authenticated identity HTTP status:", response.status)
subprocess.run(CLI + ["revoke", "--cache-dir", "/var/lib/receipt/cache",
                     "--id", identity["id"]], check=True, stdout=subprocess.DEVNULL)
try:
    urllib.request.urlopen(request, timeout=5)
except urllib.error.HTTPError as error:
    assert error.code == 401, error.code
    print("Revoked credential HTTP status:", error.code)
else:
    raise RuntimeError("Revoked credential was accepted")
