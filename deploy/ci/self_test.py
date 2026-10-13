"""Receipt testing itself. The uncached workflow job remains independent."""
import contextlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path('/tmp/receipt-self-checkout')
HOME = Path('/tmp/receipt-self-home')
CLIENT = '/tmp/receipt-self-client'
COMMAND = ['go', 'test', '-count=1', './...']


def test_environment(go_directory, image_version):
    if not image_version:
        raise ValueError('runner image version is required for safe cache separation')
    # Opt-in database/Cargo/measurement settings and arbitrary runner variables
    # are absent. Do not ignore all environment variables in Receipt itself.
    return {'PATH': go_directory + ':/usr/bin:/bin', 'HOME': str(HOME),
            'GOCACHE': str(HOME / 'build'), 'GOPATH': str(HOME / 'go'),
            'CGO_ENABLED': '0', 'GOTOOLCHAIN': 'local', 'LANG': 'C', 'TZ': 'UTC',
            'RECEIPT_RUNNER_IMAGE': image_version}


@contextlib.contextmanager
def remote_cache():
    names = ['RECEIPT_SELF_CI_' + suffix for suffix in ('SSH_KEY', 'KNOWN_HOSTS', 'TOKEN')]
    if not all(os.environ.get(name) for name in names):
        print('Cache credentials unavailable; running local tests.', flush=True)
        yield False
        return
    with tempfile.TemporaryDirectory(prefix='receipt-self-ssh-') as directory:
        key = Path(directory) / 'key'
        key.write_text(os.environ[names[0]] + '\n')
        key.chmod(0o600)
        hosts = Path(directory) / 'hosts'
        hosts.write_text(os.environ[names[1]] + '\n')
        tunnel = subprocess.Popen(['ssh', '-N', '-i', str(key), '-o', 'BatchMode=yes',
            '-o', 'IdentitiesOnly=yes', '-o', 'StrictHostKeyChecking=yes',
            '-o', 'UserKnownHostsFile=' + str(hosts), '-o', 'ExitOnForwardFailure=yes',
            '-o', 'ConnectTimeout=10', '-o', 'ServerAliveInterval=15',
            '-o', 'ServerAliveCountMax=2', '-L', '127.0.0.1:18777:127.0.0.1:7777',
            'receipt-ci@104.248.183.112'])
        ready = False
        try:
            for _ in range(12):
                if tunnel.poll() is not None:
                    break
                try:
                    request = urllib.request.Request('http://127.0.0.1:18777/v1/identity',
                        headers={'Authorization': 'Bearer ' + os.environ[names[2]]})
                    with urllib.request.urlopen(request, timeout=2) as response:
                        identity = json.load(response)
                    ready = identity['subject'] == 'receipt-self-ci'
                    break
                except OSError:
                    time.sleep(1)
            if not ready:
                print('Private cache unavailable; running local tests.', flush=True)
            yield ready
        finally:
            if tunnel.poll() is None:
                tunnel.terminate()
                try:
                    tunnel.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    tunnel.kill()
                    tunnel.wait()


def main():
    # A clone provides fixed paths and only committed source, never operator dirt.
    if ROOT.exists() or HOME.exists():
        raise RuntimeError('refusing to reuse an existing CI workspace')
    subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '.', str(ROOT)], check=True)
    HOME.mkdir()
    env = test_environment(str(Path(shutil.which('go')).resolve().parent),
                           os.environ.get('ImageVersion', ''))
    subprocess.run(['go', 'mod', 'download'], cwd=ROOT, env=env, check=True)
    subprocess.run(['go', 'mod', 'verify'], cwd=ROOT, env=env, check=True)
    env['GOPROXY'] = 'off'
    # The current sole dependency is prepared and explicitly fingerprinted.
    # Fail visibly if go.mod grows rather than silently omitting new dependencies.
    modules = subprocess.check_output(['go', 'list', '-m', 'all'], cwd=ROOT, env=env, text=True).splitlines()
    if modules != ['receipt', 'github.com/lib/pq v1.10.9']:
        raise RuntimeError('update declared CI module inputs for changed dependencies')
    config = ROOT / '.receipt.json'
    if config.exists():
        raise RuntimeError('review repository configuration before adding CI declarations')
    config.write_text(json.dumps({'extra_inputs': [
        str(HOME / 'go/pkg/mod/github.com/lib/pq@v1.10.9'),
        '/usr/bin/git', '/usr/bin/dash', CLIENT]}))
    with remote_cache() as ready:
        if ready:
            env['RECEIPT_TOKEN'] = os.environ['RECEIPT_SELF_CI_TOKEN']
        args = [CLIENT, 'run', '--cache-dir', str(HOME / 'receipt')]
        if ready:
            args += ['--remote', 'http://127.0.0.1:18777']
        started = time.monotonic()
        result = subprocess.run(args + ['--'] + COMMAND, cwd=ROOT, env=env)
        print(f'Receipt invocation: {time.monotonic() - started:.2f}s; exit={result.returncode}', flush=True)
        return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())
