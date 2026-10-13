"""Trusted synthetic workload only. Never run PR-supplied code with these secrets."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request


def main():
    mode = sys.argv[1]
    assert mode in ("publish", "replay")
    # Fixed paths and a deliberately small environment allow equivalent runners
    # to share a key. This is not a general portable-key implementation.
    root = Path("/tmp/receipt-ci-fixture")
    root.mkdir()  # Refuse to reuse any pre-existing checkout.
    home = Path("/tmp/receipt-ci-home")
    home.mkdir()
    marker = home / "executions"
    run_id = os.environ["GITHUB_RUN_ID"] + "-" + os.environ["GITHUB_RUN_ATTEMPT"]
    assert all(c in "0123456789-" for c in run_id)
    (root / "go.mod").write_text("module example.com/receipt-ci\n\ngo 1.22\n")
    source = root / "value.go"
    source.write_text('package fixture\n// CI run ' + run_id + '\nfunc Value() int { return 42 }\n')
    (root / "value_test.go").write_text('''package fixture
import ("os"; "testing")
func TestValue(t *testing.T) {
    f, err := os.OpenFile("/tmp/receipt-ci-home/executions", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
    if err != nil { t.Fatal(err) }
    if _, err = f.WriteString("ran\\n"); err != nil { t.Fatal(err) }
    if err = f.Close(); err != nil { t.Fatal(err) }
    if Value() != 42 { t.Fatal("expected 42") }
}
''')
    subprocess.run(["git", "init", "-q", str(root)], check=True)
    go_bin = str(Path(shutil.which("go")).resolve().parent)
    env = {"PATH": go_bin + ":/usr/bin:/bin", "HOME": str(home),
           "GOCACHE": str(home / "go-cache"), "GOPATH": str(home / "go"),
           "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "LANG": "C", "TZ": "UTC",
           "RECEIPT_TOKEN": os.environ["RECEIPT_TOKEN"]}

    def execute(label, expected_status, expected_text, expected_runs):
        cache = home / label
        assert not cache.exists()
        result = subprocess.run(["/tmp/receipt-ci-client", "run", "--cache-dir", str(cache),
                                 "--remote", "http://127.0.0.1:18777", "--",
                                 "go", "test", "-count=1", "-v", "."],
                                cwd=root, env=env, capture_output=True, text=True, timeout=180)
        # This fixture prints no secrets. Do not generalize this to arbitrary tests.
        print(result.stdout, end="")
        print(result.stderr, end="")
        assert result.returncode == expected_status, (label, result.returncode)
        assert expected_text in result.stderr, label
        count = len(marker.read_text().splitlines()) if marker.exists() else 0
        assert count == expected_runs, (label, count)
        print(f"CHECK PASSED: {label}; actual executions={count}")

    with tempfile.TemporaryDirectory(prefix="receipt-ci-ssh-") as temporary:
        key = Path(temporary) / "key"
        key.write_text(os.environ["RECEIPT_CI_SSH_KEY"] + "\n")
        key.chmod(0o600)
        hosts = Path(temporary) / "known_hosts"
        hosts.write_text(os.environ["RECEIPT_CI_KNOWN_HOSTS"] + "\n")
        ssh = ["ssh", "-i", str(key),
            "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
            "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + str(hosts),
            "-o", "ExitOnForwardFailure=yes", "-o", "ConnectTimeout=10",
            "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2"]
        destination = "receipt-ci@104.248.183.112"
        # Prove restrictions using the CI key itself, not just configuration text.
        for args in ([destination, "true"], ["-W", "127.0.0.1:5432", destination],
                     ["-N", "-R", "127.0.0.1:18779:127.0.0.1:7777", destination]):
            denied = subprocess.run(ssh + list(args), capture_output=True, timeout=15)
            assert denied.returncode != 0, "unexpected SSH capability allowed"
        print("CHECK PASSED: shell, database forwarding and reverse forwarding denied")
        tunnel = subprocess.Popen(ssh + ["-N", "-L", "127.0.0.1:18777:127.0.0.1:7777", destination])
        try:
            for attempt in range(20):
                if tunnel.poll() is not None:
                    raise RuntimeError("SSH tunnel exited")
                try:
                    req = urllib.request.Request("http://127.0.0.1:18777/v1/identity",
                        headers={"Authorization": "Bearer " + env["RECEIPT_TOKEN"]})
                    with urllib.request.urlopen(req, timeout=2) as response:
                        assert json.load(response)["subject"] == "github-actions-experiment"
                    break
                except OSError:
                    time.sleep(1)
            else:
                raise RuntimeError("authenticated tunnel readiness timed out")
            if mode == "publish":
                execute("first", 0, "receipt: miss", 1)
                # Empty local cache proves publication before the second job starts.
                execute("upload-check", 0, "receipt: hit", 1)
            else:
                execute("fresh-runner", 0, "receipt: hit", 0)
                original = source.read_text()
                source.write_text(original.replace("return 42", "return 43"))
                execute("changed-source", 1, "receipt: miss", 1)
                source.write_text(original)
                tunnel.terminate()
                tunnel.wait(timeout=10)
                execute("offline", 0, "falling back to execution", 2)
        finally:
            if tunnel.poll() is None:
                tunnel.terminate()
                tunnel.wait(timeout=10)


if __name__ == "__main__":
    main()
