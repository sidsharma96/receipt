"""One-off, bounded R2 archive transfer. No scheduling or object deletion.

Run as root on the droplet: MODE OBJECT FILE (MODE is upload or download).
Uses the four R2_* fields in /etc/receipt/backup.env. Never logs credentials.
This small drill helper supports archives up to 64 MiB, not multipart uploads.
"""
import datetime
import hashlib
import hmac
import pathlib
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

LIMIT = 64 * 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def transfer(mode, name, filename):
    if mode not in ('upload', 'download') or not re.fullmatch(r'drills/[a-zA-Z0-9_-]+\.cms', name):
        raise ValueError('Invalid operation or archive name')
    config = {}
    for line in pathlib.Path('/etc/receipt/backup.env').read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        key, sep, value = line.partition('=')
        if not sep or key in config:
            raise ValueError('Invalid configuration')
        config[key] = value
    required = {'R2_ACCESS_KEY_ID', 'R2_SECRET_ACCESS_KEY', 'R2_ENDPOINT', 'R2_BUCKET'}
    if set(config) != required or not all(config.values()) or config['R2_BUCKET'] != 'receipt-backups':
        raise ValueError('Invalid configuration')
    endpoint = urllib.parse.urlsplit(config['R2_ENDPOINT'])
    if (endpoint.scheme != 'https' or not endpoint.hostname
            or not endpoint.hostname.endswith('.r2.cloudflarestorage.com')
            or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment
            or endpoint.path not in ('', '/') or endpoint.port not in (None, 443)):
        raise ValueError('Invalid endpoint')
    file = pathlib.Path(filename)
    data = b''
    if mode == 'upload':
        with file.open('rb') as source:
            data = source.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise ValueError('Archive exceeds drill size limit')
    elif file.exists():
        raise ValueError('Download destination already exists')
    method = 'PUT' if mode == 'upload' else 'GET'
    path = '/receipt-backups/' + name
    now = datetime.datetime.now(datetime.timezone.utc)
    date, stamp = now.strftime('%Y%m%d'), now.strftime('%Y%m%dT%H%M%SZ')
    headers = {'host': endpoint.netloc, 'x-amz-date': stamp,
               'x-amz-content-sha256': hashlib.sha256(data).hexdigest()}
    if mode == 'upload':
        headers['if-none-match'] = '*'  # Never overwrite an existing backup.
    signed = ';'.join(sorted(headers))
    canonical_headers = ''.join(f'{key}:{headers[key]}\n' for key in sorted(headers))
    canonical = '\n'.join([method, path, '', canonical_headers, signed, headers['x-amz-content-sha256']])
    scope = f'{date}/auto/s3/aws4_request'
    to_sign = '\n'.join(['AWS4-HMAC-SHA256', stamp, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    signing_key = ('AWS4' + config['R2_SECRET_ACCESS_KEY']).encode()
    for component in (date, 'auto', 's3', 'aws4_request'):
        signing_key = hmac.new(signing_key, component.encode(), hashlib.sha256).digest()
    signature = hmac.new(signing_key, to_sign.encode(), hashlib.sha256).hexdigest()
    headers['Authorization'] = (f'AWS4-HMAC-SHA256 Credential={config["R2_ACCESS_KEY_ID"]}/{scope}, '
                                f'SignedHeaders={signed}, Signature={signature}')
    request = urllib.request.Request(f'https://{endpoint.netloc}{path}',
                                     data=data if mode == 'upload' else None,
                                     headers=headers, method=method)
    with urllib.request.build_opener(NoRedirect).open(request, timeout=60) as response:
        if mode == 'download':
            data = response.read(LIMIT + 1)
            if len(data) > LIMIT:
                raise ValueError('Download exceeds drill size limit')
            with file.open('xb') as target:
                target.write(data)
        print(f'{mode}: HTTP {response.status}; bytes={len(data)}; sha256={hashlib.sha256(data).hexdigest()}')


if __name__ == '__main__':
    import os
    os.umask(0o077)
    try:
        if len(sys.argv) != 4:
            raise ValueError('Expected MODE OBJECT FILE')
        transfer(*sys.argv[1:])
    except urllib.error.HTTPError as error:
        sys.exit(f'R2 request failed: HTTP {error.code}; response withheld')
    except Exception as error:
        sys.exit(f'Backup transfer failed ({type(error).__name__}); details withheld')
