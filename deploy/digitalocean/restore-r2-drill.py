"""Isolated same-host restore drill; never overwrites the production database.

Run as root with unshare --mount --propagation private. Uses only the archive
downloaded from R2 and decrypted on the Mac. Existing OS/PG roles are reused.
"""
import hashlib
import json
import os
import pathlib
import signal
import subprocess
import tarfile
import time
import urllib.request

DATABASE = 'receipt_restore_r2_20261013'
ROOT = pathlib.Path('/var/backups/receipt-r2-drill-3fr7fj87/restore')
TABLES = ('receipt_entries_v2', 'receipt_evidence_v2',
          'receipt_storage_policy_v3', 'receipt_credentials_v5')


def run(*args, **kwargs):
    return subprocess.run(args, check=True, timeout=60, **kwargs)


def sql(database, query):
    return subprocess.check_output(['runuser', '-u', 'postgres', '--', 'psql',
        '-X', '-v', 'ON_ERROR_STOP=1', '-d', database, '-Atc', query], timeout=30).decode().strip()


def extract(source, destination):
    # Only archives produced in this drill; reject links and escaping paths.
    with tarfile.open(source) as archive:
        for member in archive.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                raise RuntimeError('Unsafe archive member')
    destination.mkdir(mode=0o700)
    run('tar', '-xf', str(source), '-C', str(destination))


os.umask(0o077)
if os.readlink('/proc/self/ns/mnt') == os.readlink('/proc/1/ns/mnt'):
    raise SystemExit('Requires private mount namespace')
extract(ROOT.parent / 'downloaded-decrypted.tar', ROOT)
checksums = json.loads((ROOT / 'checksums.json').read_text())
assert set(checksums) == {'receipt.dump', 'server.tar.gz', 'roles.sql'}
for name, expected in checksums.items():
    assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == expected
print('All archive checksums verified', flush=True)
extract(ROOT / 'server.tar.gz', ROOT / 'extracted')
# createdb refuses to replace a database if this name already exists.
run('runuser', '-u', 'postgres', '--', 'createdb', '-O', 'receipt', DATABASE)
with (ROOT / 'receipt.dump').open('rb') as source:
    run('runuser', '-u', 'postgres', '--', 'pg_restore', '--exit-on-error',
        '--single-transaction', '-d', DATABASE, stdin=source)
for table in TABLES:
    query = (f"SELECT count(*) || ':' || md5(COALESCE(string_agg(row_to_json(t)::text, '' "
             f"ORDER BY row_to_json(t)::text), '')) FROM {table} t")
    assert sql(DATABASE, query) == sql('receipt', query), f'{table}: live data changed or restore differs'
    print(f'{table}: restored rows match live snapshot; count={sql(DATABASE, "SELECT count(*) FROM " + table)}', flush=True)
run('mount', '--bind', str(ROOT / 'extracted/var/lib/receipt'), '/var/lib/receipt')
run('mount', '--bind', str(ROOT / 'extracted/usr/local/bin/receipt'), '/usr/local/bin/receipt')
os.chdir('/var/lib/receipt')
cli = ['runuser', '-u', 'receipt', '--', 'env',
       f'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname={DATABASE} sslmode=disable',
       '/usr/local/bin/receipt']
process = None
issued = None
with (ROOT / 'server.log').open('xb') as log:
    try:
        process = subprocess.Popen(cli + ['serve', '--postgres', '--credentials', '--listen',
            '127.0.0.1:17779', '--cache-dir', '/var/lib/receipt/cache', '--max-inflight', '2'],
            stdout=log, stderr=log, start_new_session=True)
        for _ in range(50):
            if process.poll() is not None:
                raise RuntimeError('Restored server exited')
            try:
                urllib.request.urlopen('http://127.0.0.1:17779/v1/identity', timeout=1).close()
            except urllib.error.HTTPError as error:
                if error.code == 401:
                    break
            except urllib.error.URLError:
                pass
            time.sleep(0.1)
        else:
            raise RuntimeError('Restored server not ready')
        issued = json.loads(subprocess.check_output(cli + ['auth', 'issue', '--cache-dir',
            '/var/lib/receipt/cache', '--subject', 'r2-restore-drill', '--scopes', 'read',
            '--expires-in', '5m'], timeout=30))
        keys = sql(DATABASE, 'SELECT key FROM receipt_entries_v2 WHERE NOT quarantined ORDER BY key').splitlines()
        known = '1ceddd490f9799adbc8950b5a901fa6e1306b9b549d876c91395f787a6bf1a88'
        assert known in keys
        for key in keys:
            request = urllib.request.Request('http://127.0.0.1:17779/v1/entries/' + key,
                headers={'Authorization': 'Bearer ' + issued['token']})
            with urllib.request.urlopen(request, timeout=15) as response:
                data = response.read()
                assert response.status == 200
                if key == known:
                    assert hashlib.sha256(data).hexdigest() == '71d261af226f705a24f34fc7e2c316d7717aade5c068e3b85260588f9bb8222a'
                    print(f'Known result: {len(data)} bytes; expected SHA-256 matched', flush=True)
        print(f'All {len(keys)} nonquarantined restored results fetched successfully', flush=True)
    finally:
        try:
            if issued:
                run(*(cli + ['auth', 'revoke', '--cache-dir', '/var/lib/receipt/cache',
                             '--id', issued['id']]), stdout=subprocess.DEVNULL)
        finally:
            if process:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
print('Restore drill passed; test server stopped. Production paths were not replaced.', flush=True)
