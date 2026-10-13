"""Receipt testing itself. The uncached workflow job remains independent."""
import contextlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.request

ROOT = Path('/tmp/receipt-self-checkout')
HOME = Path('/tmp/receipt-self-home')
CLIENT = '/tmp/receipt-self-client'
COMMAND = ['go', 'test', '-count=1', './...']
REMOTE = 'https://receipt.sidsharma.dev'


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
    token = os.environ.get('RECEIPT_SELF_CI_TOKEN')
    if not token:
        print('Cache credentials unavailable; running local tests.', flush=True)
        yield False
        return
    ready = False
    try:
        request = urllib.request.Request(REMOTE + '/v1/identity',
            headers={'Authorization': 'Bearer ' + token})
        # Reject redirects: never forward the bearer token to another destination.
        opener = urllib.request.build_opener(NoRedirect)
        with opener.open(request, timeout=10) as response:
            identity = json.load(response)
        ready = isinstance(identity, dict) and identity.get('subject') == 'receipt-self-ci'
    except (OSError, ValueError):
        pass
    if not ready:
        print('HTTPS cache unavailable; running local tests.', flush=True)
    yield ready


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


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
            args += ['--remote', REMOTE]
        started = time.monotonic()
        result = subprocess.run(args + ['--'] + COMMAND, cwd=ROOT, env=env)
        print(f'Receipt invocation: {time.monotonic() - started:.2f}s; exit={result.returncode}', flush=True)
        return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())
