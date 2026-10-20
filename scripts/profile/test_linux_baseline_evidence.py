"""Check the retained Linux workload, resource scope and planner evidence."""
import hashlib
import json
from pathlib import Path
import unittest

from linux_ci_baseline import distributions
from summarize_linux_baseline import request_summary

DATA = Path(__file__).resolve().parents[2] / '.tools/measurements/linux-baseline-2026-10-19'


@unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
class LinuxBaselineEvidence(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.summary = json.loads((DATA / 'summary.json').read_text())
        cls.clients = [json.loads((DATA / f'client-{i}.json').read_text()) for i in (1, 2)]

    def test_all_planned_hits_and_identity(self):
        total = 0
        for client, reference in zip(self.clients, self.summary['clients']):
            self.assertTrue(client['completed'])
            self.assertEqual(client['key_before'], client['key_after'])
            self.assertEqual(client['go_version'], 'go version go1.22.12 linux/amd64')
            self.assertEqual(client['summary'], distributions(client['rows']))
            self.assertEqual(client['summary'], reference['summary'])
            self.assertEqual(hashlib.sha256((DATA / reference['file']).read_bytes()).hexdigest(), reference['sha256'])
            for case, calls in (('local', 100), ('checked-local', 20), ('remote-fetch', 10)):
                for block in range(3):
                    self.assertEqual(sum(r['case'] == case and r['block'] == block for r in client['rows']), calls)
            for row in client['rows']:
                total += 1
                self.assertTrue(row['hit'])
                self.assertEqual(row['returncode'], 0)
                self.assertFalse(row['timing']['failed'])
                self.assertNotIn('execution', [s['stage'] for s in row['timing']['stages']])
                self.assertGreater(row['end_ns'], row['start_ns'])
                self.assertGreater(row['rss_mb'], 0)
        self.assertEqual(total, 780)
        self.assertEqual(self.clients[0]['client_sha256'], self.clients[1]['client_sha256'])
        # Tracked diagnostic files changed between sessions, so different keys are expected.
        self.assertNotEqual(self.clients[0]['key_before'], self.clients[1]['key_before'])

    def test_separate_transport_samples_are_ordered(self):
        self.assertNotIn('transport', self.clients[0])
        rows = self.clients[1]['transport']['rows']
        self.assertEqual(len(rows), 20)
        for row in rows:
            self.assertEqual(row['status'], 204 if row['case'] == 'status' else 200)
            clocks = [row[k] for k in ('dns_s', 'connected_s', 'tls_ready_s', 'first_byte_s', 'total_s')]
            self.assertEqual(clocks, sorted(clocks))
            self.assertEqual(row['download_bytes'], 0 if row['case'] == 'status' else 57821)

    def test_actual_point_queries_used_existing_indexes(self):
        plans = self.summary['server']['plans']['plans']
        for name, values in plans.items():
            node = values[0]['Plan']
            self.assertEqual(node['Node Type'], 'Index Scan')
            self.assertEqual(node['Actual Rows'], 1)
            self.assertEqual(node['Shared Read Blocks'], 0)
            self.assertEqual(node['Index Name'], 'receipt_credentials_v5_pkey' if name == 'authentication' else 'receipt_entries_v2_pkey')
        self.assertTrue(self.summary['server']['build']['modified'])

    def test_server_counter_windows_keep_their_scope(self):
        for window in self.summary['server']['windows']:
            before, after = window['before'], window['after']
            self.assertEqual(before['pid'], after['pid'])
            self.assertEqual(before['binary_sha256'], after['binary_sha256'])
            self.assertGreaterEqual(after['cpu_seconds'], before['cpu_seconds'])
            self.assertEqual(window['request_summary'], request_summary(window['logs']['events']))
            for event in window['logs']['events']:
                self.assertEqual(set(event), {'journal_unix_us', 'version', 'request_id', 'operation', 'status', 'duration_ns', 'stages'})
        self.assertIn('service_cpu_seconds_delta', self.summary['server']['windows'][1])


if __name__ == '__main__':
    unittest.main()
