"""Actual-Caddy evidence, privacy configuration and restoration guards."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import caddy_capture
from summarize_caddy import summarize

ROOT=Path(__file__).resolve().parents[2]
DATA=ROOT/'docs/measurements/caddy-gzip-2026-10-20'


class CaddyComparison(unittest.TestCase):
    def test_profile_removes_sensitive_objects_before_logging(self):
        source=(ROOT/'deploy/digitalocean/Caddyfile.gzip-profile').read_text()
        for text in ('request delete','resp_headers delete','method GET','path /v1/entries/*','status 200','gzip'):
            self.assertIn(text,source)
        self.assertNotIn('log_credentials',source)

    def test_restore_refuses_other_edits(self):
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary);config=state/'Caddyfile'
            original=b'original';profile=b'profile'
            (state/'metadata.json').write_text(json.dumps({'original_sha256':caddy_capture.digest(original),'profile_sha256':caddy_capture.digest(profile)}))
            (state/'original.Caddyfile').write_bytes(original)
            config.write_bytes(b'concurrent change')
            with mock.patch.object(caddy_capture,'STATE',state),mock.patch.object(caddy_capture,'CONFIG',config):
                with self.assertRaisesRegex(RuntimeError,'concurrent'):
                    caddy_capture.restore()
            self.assertEqual(config.read_bytes(),b'concurrent change')

    def evidence(self):
        if not DATA.exists(): self.skipTest('CI evidence not yet collected')
        return json.loads((DATA/'client.json').read_text()),json.loads((DATA/'server.json').read_text())

    def test_full_evidence_recomputes(self):
        client,server=self.evidence();retained=json.loads((DATA/'summary.json').read_text())
        self.assertEqual(retained['client_sha256'],hashlib.sha256((DATA/'client.json').read_bytes()).hexdigest())
        self.assertEqual(retained['server_sha256'],hashlib.sha256((DATA/'server.json').read_bytes()).hexdigest())
        self.assertEqual(summarize(client,server),{k:v for k,v in retained.items() if k not in ('client_sha256','server_sha256')})
        for event in server['caddy']:
            self.assertEqual(set(event),{'ts','duration','status','size','request_id','encoding'})
        self.assertEqual(retained['calls'],180)

    def test_wrong_bytes_or_missing_request_fails(self):
        client,server=self.evidence()
        changed=copy.deepcopy(server)
        request=client['rows'][0]['http'][0]['request_id']
        edge=next(r for r in changed['caddy'] if r['request_id']==request)
        edge['size']=0
        with self.assertRaisesRegex(ValueError,'byte accounting'):
            summarize(client,changed)
        changed=copy.deepcopy(server);changed['caddy']=[r for r in changed['caddy'] if r['request_id']!=request]
        with self.assertRaises(KeyError): summarize(client,changed)

if __name__=='__main__': unittest.main()
