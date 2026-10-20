"""Guard attribution boundaries and the retained Go HTTP experiment."""
import hashlib
import json
from pathlib import Path
import unittest
from summarize_http_trace import phases, summarize

DATA = Path(__file__).resolve().parents[2]/'.tools/measurements/http-trace-2026-10-19'


class HTTPTrace(unittest.TestCase):
    def trace(self):
        names = ['request_start','dns_start','dns_done','connect_start','connect_done',
                 'tls_start','tls_done','request_written','first_response_byte',
                 'headers_returned','body_read_start','body_read_done','decode_start','decode_done','body_closed']
        return {'kind':'entries','events':[{'name':name,'ns':i*1000000} for i,name in enumerate(names)]}

    def test_phase_boundaries(self):
        result = phases(self.trace())
        self.assertEqual(result['http_through_close_ms'],14)
        self.assertEqual(result['body_read_ms'],1)
        self.assertEqual(result['decode_ms'],1)
        self.assertEqual(result['write_to_first_byte_ms'],1)

    def test_retries_cannot_be_silently_attributed(self):
        trace = self.trace()
        trace['events'].insert(4,dict(trace['events'][3]))
        with self.assertRaisesRegex(ValueError,'exactly one connect_start'):
            phases(trace)

    def test_reordered_events_fail(self):
        trace = self.trace()
        trace['events'][4]['ns'] = 0
        with self.assertRaisesRegex(ValueError,'nonmonotonic'):
            phases(trace)

    def test_retained_evidence(self):
        if not DATA.exists():
            self.skipTest('restore historical measurements: python3 scripts/profile/restore_measurements.py')
        client = json.loads((DATA/'client.json').read_text())
        retained = json.loads((DATA/'summary.json').read_text())
        self.assertEqual(retained['client_sha256'],hashlib.sha256((DATA/'client.json').read_bytes()).hexdigest())
        actual = summarize(client,{'events':[m['server'] for m in retained['matched']]})
        self.assertEqual(actual,{k:v for k,v in retained.items() if k not in ('server_identity','client_sha256')})
        self.assertEqual(actual['matched_calls'],60)
        for case in ('stock-checked-local','stock-remote-fetch','traced-checked-local','traced-remote-fetch'):
            for block in range(3):
                self.assertEqual(sum(r['case']==case and r['block']==block for r in client['rows']),10)
        for row in client['rows']:
            for trace in row.get('http',[]):
                self.assertEqual(set(trace),{'type','kind','status','protocol','request_id','reused','bytes','events'})
                self.assertFalse(trace['reused'])
                self.assertEqual(trace['type'],'diagnostic_http')
                self.assertIn(trace['kind'],('entries','status'))
                self.assertRegex(trace['request_id'],r'^[a-f0-9]{32}-[a-f0-9]+$')
                self.assertEqual(trace['status'],200 if trace['kind']=='entries' else 204)


if __name__ == '__main__':
    unittest.main()
