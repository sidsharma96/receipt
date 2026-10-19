"""Audit captured boundaries and the claims made from the paired data."""
import json
from pathlib import Path
import unittest

DATA = Path(__file__).resolve().parents[2] / 'docs/measurements/git-outliers-2026-10-18'


class OutlierEvidence(unittest.TestCase):
    def test_correlated_startup_cases(self):
        cases = json.loads((DATA / 'startup-cases.json').read_text())['cases']
        self.assertEqual(len(cases), 2)
        for case in cases:
            call = case['call']
            end = next(e for e in reversed(case['git_events']) if e['event'] == 'atexit')
            self.assertEqual(int(end['sid'].rsplit('-P', 1)[1], 16), call['pid'])
            self.assertEqual(end['code'], 0)
            self.assertEqual(case['git_reported_ms'], end['t_abs'] * 1000)
            self.assertGreater(case['before_git_ms'], 200)
            total = case['before_git_ms'] + case['git_reported_ms'] + case['after_git_ms']
            wall_stamps = (call['wait_return_ns'] - call['start_ns']) / 1e6
            self.assertAlmostEqual(total, wall_stamps, places=6)
            self.assertLess(abs(total - call['wall_ms']), 0.02)

    def test_paired_record_integrity(self):
        data = json.loads((DATA / 'paired-replays.json').read_text())
        self.assertTrue(data['completed'])
        self.assertEqual(data['keys']['launcher'], data['keys']['direct'])
        self.assertEqual(len(data['runs']), 1000)
        for label in ('launcher', 'direct'):
            rows = [r for r in data['runs'] if r['label'] == label]
            self.assertEqual(len(rows), 500)
            self.assertEqual(sum(r['order'][0] == label for r in rows), 250)
            calls = [e for r in rows for e in r['events'] if e.get('program') == 'git']
            self.assertEqual(len(calls), 1500)
            self.assertEqual(max(c['wall_ms'] for c in calls), data['summary'][label]['git_max_ms'])
            for row in rows:
                timing = next(e for e in row['events'] if e['type'] == 'receipt_timing')
                self.assertFalse(timing['failed'])
                self.assertNotIn('execution', [s['stage'] for s in timing['stages']])
                for child in [e for e in row['events'] if e['type'] == 'diagnostic_child']:
                    self.assertGreater(child['first_output_ns'], 0)
                    self.assertGreaterEqual(child['first_output_ns'], child['start_ns'])
                    self.assertLessEqual(child['first_output_ns'], child['wait_return_ns'])
                    self.assertAlmostEqual(child['start_ms'] + child['wait_ms'], child['wall_ms'], places=6)

    def test_largest_hits_are_not_git_dominated(self):
        data = json.loads((DATA / 'paired-replays.json').read_text())
        def timing(row):
            return next(e for e in row['events'] if e['type'] == 'receipt_timing')
        for label in ('launcher', 'direct'):
            rows = [r for r in data['runs'] if r['label'] == label]
            row = max(rows, key=lambda r: timing(r)['duration_ns'])
            self.assertGreater(timing(row)['duration_ns'], 400_000_000)
            git = [e for e in row['events'] if e.get('program') == 'git']
            self.assertLess(max(c['wall_ms'] for c in git), 30 if label == 'launcher' else 10)
            stages = {s['stage']: s['ns'] / 1e6 for s in timing(row)['stages']}
            if label == 'launcher':
                probe = next(e for e in row['events'] if e.get('program') == 'go')
                self.assertGreater(probe['wall_ms'], 370)
                self.assertLess(probe['cpu_ms'], 54)
            else:
                self.assertGreater(stages['input_files'], 100)
                self.assertGreater(stages['executable'], 100)
            gc = next(e for e in row['events'] if e['type'] == 'diagnostic_gc')
            self.assertLess(gc['pause_ns'], 1_000_000)


if __name__ == '__main__':
    unittest.main()
