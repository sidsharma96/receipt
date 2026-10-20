import json
from pathlib import Path
import unittest

DATA=Path(__file__).resolve().parents[2]/'.tools/measurements/network-deep-dive-2026-10-20'


@unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
class RetainedEvidence(unittest.TestCase):
    def test_request_counts_and_compatibility_scope(self):
        for name in ('run-1-summary.json','run-2-summary.json'):
            report=json.loads((DATA/name).read_text())
            self.assertEqual(report['total_requests'],120)
            self.assertEqual(report['matched_server_requests'],120)
            self.assertEqual(len({r['request_id'] for r in report['rows']}),120)
            self.assertEqual(sum(r['endpoint']=='fetch' for r in report['rows']),108)
            self.assertEqual(report['groups']['True/reuse/fetch/False']['n'],19)
            self.assertEqual(report['groups']['True/resume/fetch/False']['resumed'],19)
        compat=json.loads((DATA/'compatibility.json').read_text())
        self.assertEqual(compat['fetches'],216)
        self.assertEqual(len(compat['body_sha256']),1)
        self.assertEqual(compat['body_sha256'],compat['decoded_sha256'])

    def test_native_and_dns_matches_preserve_limits(self):
        native=json.loads((DATA/'server-tcp.json').read_text())
        self.assertEqual(native['matched_connections'],101)
        self.assertEqual(len(native['selected_connections']),4)
        dns=json.loads((DATA/'dns-user-packet-match.json').read_text())
        self.assertEqual(dns['matched_reads'],72)
        self.assertTrue(dns['over50_reads'])
        self.assertTrue(all(r['qtype']==28 for r in dns['over50_reads']))
        self.assertLess(dns['max_read_done_after_kernel_packet_ms'],1)
