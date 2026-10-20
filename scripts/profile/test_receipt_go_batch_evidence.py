"""Protect the published batch's coverage, clock relationships and scoped claims."""
import json
import math
from pathlib import Path
import unittest

DATA = Path(__file__).resolve().parents[2] / '.tools/measurements/receipt-go-batch-2026-10-19/evidence.json'


@unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
class ReceiptGoBatchEvidence(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads(DATA.read_text())

    def test_fixed_budget_success_and_identity(self):
        data = self.data
        metadata = data['metadata']
        self.assertTrue(metadata['completed'])
        self.assertFalse(metadata['plan']['stop_on_outlier'])
        self.assertEqual(len(data['rows']), 5000)
        self.assertEqual([r['i'] for r in data['rows']], list(range(5000)))
        self.assertEqual([sum(r['block'] == b for r in data['rows']) for b in range(5)], [1000] * 5)
        keys = [metadata[k] for k in ('stock_key_before', 'diagnostic_key_before', 'stock_key_after', 'diagnostic_key_after')]
        self.assertEqual(len(set(keys)), 1)
        for row in data['rows']:
            self.assertNotIn('execution', row['stages_ms'])
            self.assertEqual(len(row['git_wall_ms']), 2)
            self.assertEqual(row['go']['operation'], 'env')
        self.assertEqual(data['summary']['system_errors'], 0)

    def test_clock_and_resource_relationships(self):
        for row in self.data['rows']:
            go = row['go']
            self.assertLessEqual(row['start_ns'], go['start_ns'])
            self.assertLessEqual(go['start_ns'], go['start_return_ns'])
            self.assertLessEqual(go['start_return_ns'], go['wait_return_ns'])
            self.assertLessEqual(go['start_ns'], go['first_output_ns'])
            self.assertLessEqual(go['first_output_ns'], go['wait_return_ns'])
            self.assertLessEqual(go['wait_return_ns'], row['end_ns'])
            self.assertAlmostEqual(go['start_ms'] + go['wait_ms'], go['wall_ms'], places=5)
            self.assertLess(abs((go['wait_return_ns']-go['start_ns']) / 1e6 - go['wall_ms']), .01)
            for metric in (go['wall_ms'], go['cpu_ms'], row['elapsed_ms'], row['rss_bytes']):
                self.assertTrue(math.isfinite(metric) and metric > 0)

    def test_retained_stall_and_cluster_claims(self):
        rows = self.data['rows']
        slow = [r for r in rows if r['go']['wall_ms'] >= 200]
        self.assertEqual([r['i'] for r in slow], [666, 688, 690, 691, 694, 723, 727])
        self.assertEqual(sum(r['go']['wall_ms'] >= 300 for r in rows), 3)
        largest = max(rows, key=lambda r: r['go']['wall_ms'])
        self.assertEqual(largest['i'], 688)
        self.assertGreater(largest['go']['wall_ms'], 1600)
        self.assertLess(largest['go']['cpu_ms'], 80)
        # This measures Receipt's GC pauses, not the stock Go child's pauses.
        self.assertLess(largest['receipt_gc']['pause_ns'], 1_100_000)
        system = self.data['system_samples'][89]
        self.assertGreater((system['end_ns']-system['start_ns']) / 1e6, 1600)
        self.assertLess(system['start_ns'], largest['go']['start_ns'])
        self.assertGreater(system['end_ns'], largest['go']['start_return_ns'])
        self.assertLess(abs((system['end_ns']-system['start_ns']) / 1e6 - largest['go']['wall_ms']), 10)


if __name__ == '__main__':
    unittest.main()
