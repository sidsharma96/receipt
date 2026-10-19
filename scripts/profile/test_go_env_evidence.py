"""Check retained Go-env diagnosis records without installing or patching Go."""
import json
from pathlib import Path
import statistics
import unittest

DATA = Path(__file__).resolve().parents[2] / 'docs/measurements/go-env-diagnosis-2026-10-19/evidence.json'


class GoEnvEvidence(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads(DATA.read_text())

    def test_hunt_bounds_and_compiler_localization(self):
        data = self.data
        self.assertTrue(data['hunt']['completed'])
        self.assertTrue(data['hunt']['normalized_output_equal'])
        self.assertEqual(len(data['rows']), 1200)
        self.assertLess(max(r['elapsed_ms'] for r in data['rows']), 200)
        self.assertTrue(all(r['compiler_children'] == 1 for r in data['rows']))
        worst = max(data['rows'], key=lambda r: r['elapsed_ms'])
        full = data['worst_full_record']
        self.assertEqual(worst['pid'], full['pid'])
        self.assertEqual(worst['elapsed_ms'], full['elapsed_ms'])
        events = full['diagnostic']['events']
        begin = next(e for e in events if e['label'] == 'compiler_child_begin')
        end = next(e for e in events if e['label'] == 'compiler_child_done')
        self.assertEqual(worst['compiler_call_ms'], (end['unix_ns'] - begin['unix_ns']) / 1e6)
        self.assertLess(full['diagnostic']['gc_pause_ns'], 1_000_000)

    def test_repeated_cache_rejection(self):
        samples = self.data['cache_shape_samples']
        self.assertEqual(len(samples), 3)
        for sample in samples:
            labels = [e['label'] for e in sample['events']]
            self.assertEqual(labels.count('compiler_cache_odd_fields_3'), 1)
            self.assertEqual(labels.count('compiler_child_usage'), 1)
        # Illustrate the external Go writer/reader mismatch independently.
        payload = b'/compiler\0stat description\0'
        self.assertEqual(len(payload.split(b'\0')), 3)
        fields = payload.removesuffix(b'\0').split(b'\0')
        self.assertEqual(list(zip(fields[::2], fields[1::2])), [(b'/compiler', b'stat description')])

    def test_diagnostic_comparison(self):
        data = self.data['parser_comparison']
        self.assertTrue(data['completed'])
        self.assertTrue(data['all_normalized_outputs_equal'])
        self.assertEqual(len(data['rows']), 120)
        for label in ('before', 'after'):
            rows = [r for r in data['rows'] if r['label'] == label]
            self.assertEqual(len(rows), 60)
            self.assertEqual(sum(r['order'][0] == label for r in rows), 30)
            self.assertEqual({r['compiler_children'] for r in rows}, {1 if label == 'before' else 0})
            for metric in ('elapsed_ms', 'cpu_ms', 'rss_mb', 'compiler_children'):
                self.assertEqual(statistics.fmean(r[metric] for r in rows), data['summary'][label][metric]['mean'])

    def test_failed_os_capture_not_claimed_verified(self):
        capture = self.data['os_capture']
        self.assertEqual(capture['recorder_exit'], -11)
        self.assertFalse(capture['verified'])
        hunt = self.data['light_hunt']
        self.assertTrue(hunt['completed'])
        self.assertEqual(len(hunt['rows']), 600)
        self.assertLess(max(r['elapsed_ms'] for r in hunt['rows']), 200)

    def test_matching_runtime_trace_retained(self):
        proof = self.data['runtime_proof']
        self.assertTrue(proof['completed'])
        self.assertTrue(proof['normalized_output_equal'])
        self.assertEqual(proof['calls'], 600)
        self.assertLess(proof['largest_record']['elapsed_ms'], 100)
        self.assertIn('compiler_child_begin', proof['trace_compiler_begin'])
        self.assertIn('compiler_child_done', proof['trace_compiler_done'])
        self.assertIn('syscall.Wait4', proof['syscall_profile_wait4'])
        events = proof['largest_record']['diagnostic']['events']
        start = next(e['unix_ns'] for e in events if e['label'] == 'compiler_child_begin')
        end = next(e['unix_ns'] for e in events if e['label'] == 'compiler_child_done')
        self.assertLess(abs(proof['trace_compiler_interval_ms'] - (end - start) / 1e6), 0.1)
        self.assertTrue(self.data['packaged_builder_reproduces_measured_binaries'])


if __name__ == '__main__':
    unittest.main()
