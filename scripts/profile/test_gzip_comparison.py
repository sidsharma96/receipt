"""Validate gzip evidence and reject incomplete or nonidentical comparisons."""
import copy
import hashlib
import json
from pathlib import Path
import unittest
from summarize_gzip import span,summarize

DATA=Path(__file__).resolve().parents[2]/'docs/measurements/gzip-2026-10-20'


class GzipComparison(unittest.TestCase):
    def test_duplicate_boundary_is_not_silently_overwritten(self):
        trace={'events':[{'name':'start','ns':0},{'name':'start','ns':1},{'name':'end','ns':2}]}
        with self.assertRaisesRegex(ValueError,'callback count'):
            span(trace,'start','end')

    def test_negative_span_fails(self):
        with self.assertRaisesRegex(ValueError,'negative span'):
            span({'events':[{'name':'start','ns':2},{'name':'end','ns':1}]},'start','end')

    def evidence(self):
        if not DATA.exists(): self.skipTest('CI evidence not yet collected')
        return json.loads((DATA/'client.json').read_text())

    def test_retained_evidence_and_recomputation(self):
        client=self.evidence()
        retained=json.loads((DATA/'summary.json').read_text())
        self.assertEqual(retained['client_sha256'],hashlib.sha256((DATA/'client.json').read_bytes()).hexdigest())
        self.assertEqual(summarize(client),{k:v for k,v in retained.items() if k!='client_sha256'})
        self.assertEqual(retained['calls'],360)
        self.assertEqual(client['go_version'],'go version go1.22.12 linux/amd64')
        for row in client['rows']:
            self.assertEqual(set(row['server']),{'type','status','gzip','encoded_bytes','decoded_bytes','decoded_sha256','handler_ns','failed'})
            self.assertEqual(set(row['http'][0]),{'type','kind','status','protocol','request_id','reused','bytes','decoded_sha256','uncompressed','events'})
            self.assertEqual(row['http'][0]['request_id'],'')

    def test_missing_call_or_changed_body_is_rejected(self):
        client=self.evidence()
        changed=copy.deepcopy(client);changed['rows'].pop()
        with self.assertRaisesRegex(ValueError,'call count'):
            summarize(changed)
        changed=copy.deepcopy(client);changed['rows'][0]['http'][0]['decoded_sha256']='0'*64
        with self.assertRaisesRegex(ValueError,'decoded result mismatch'):
            summarize(changed)

if __name__=='__main__': unittest.main()
