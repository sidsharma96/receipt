"""Run a fresh Go fixture through public HTTPS; leave exercise files untouched."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from check_https import AUTH, HOST, SSH


def main():
    with tempfile.TemporaryDirectory(prefix="receipt-https-experiment-") as directory:
        base = Path(directory)
        client = base / "receipt"
        subprocess.run(["go", "build", "-o", str(client), "./cmd/receipt"],
                       env={**os.environ, "CGO_ENABLED": "0"}, check=True)
        root = base / "fixture"
        root.mkdir()
        subprocess.run(["git", "init", "-q", str(root)], check=True)
        (root / "go.mod").write_text("module example.com/https-check\n\ngo 1.22\n")
        source = root / "value.go"
        source.write_text("package fixture\nfunc Value() int { return 42 }\n")
        marker = base / "executions"
        (root / "value_test.go").write_text('''package fixture
import ("os"; "testing")
func TestValue(t *testing.T) {
    f, err := os.OpenFile(''' + json.dumps(str(marker)) + ''', os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
    if err != nil { t.Fatal(err) }
    if _, err = f.WriteString("ran\\n"); err != nil { t.Fatal(err) }
    if err = f.Close(); err != nil { t.Fatal(err) }
    if Value() != 42 { t.Fatal("expected 42") }
}
''')
        issued = json.loads(subprocess.check_output(SSH + [AUTH +
            "issue --cache-dir /var/lib/receipt/cache --subject https-upload-smoke "
            "--scopes read,publish,report --expires-in 15m"]))
        credential_id = issued["id"]
        assert len(credential_id) == 32 and all(c in "0123456789abcdef" for c in credential_id)
        try:
            env = {"PATH": str(Path(shutil.which("go")).resolve().parent) + ":/usr/bin:/bin",
                   "HOME": str(base), "GOCACHE": str(base / "build"),
                   "GOPATH": str(base / "go"), "CGO_ENABLED": "0", "GOTOOLCHAIN": "local",
                   "LANG": "C", "TZ": "UTC", "RECEIPT_TOKEN": issued["token"]}
            for label, status, message, count in [
                    ("publish", 0, "receipt: miss", 1),
                    ("replay", 0, "receipt: hit", 1),
                    ("changed", 1, "receipt: miss", 2)]:
                if label == "changed":
                    source.write_text("package fixture\nfunc Value() int { return 43 }\n")
                result = subprocess.run([str(client), "run", "--cache-dir", str(base / label),
                    "--remote", "https://" + HOST, "--", "go", "test", "-count=1", "-v", "."],
                    cwd=root, env=env, capture_output=True, text=True, timeout=180)
                assert result.returncode == status, (label, result.returncode)
                assert message in result.stderr, (label, result.stderr.replace(issued["token"], "[redacted]"))
                assert len(marker.read_text().splitlines()) == count
                print(label, "status", result.returncode, "actual executions", count)
                print(result.stderr.replace(issued["token"], "[redacted]"), end="")
        finally:
            subprocess.run(SSH + [AUTH + "revoke --cache-dir /var/lib/receipt/cache --id " +
                                  credential_id], check=True, stdout=subprocess.DEVNULL)
            print("Temporary experiment credential revoked")


if __name__ == "__main__":
    main()
