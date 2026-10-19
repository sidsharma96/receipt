"""Check estimator arithmetic without launching benchmark processes."""
import json
from pathlib import Path
import statistics
import unittest
from unittest.mock import patch

import discovery_controlled as experiment


class EstimatorTests(unittest.TestCase):
    def records(self, saving):
        return [
            {'block': block, 'order': order,
             'before': {'metrics': {'receipt_ms': 100 + i}},
             'after': {'metrics': {'receipt_ms': 100 + i - saving}}}
            for block in range(3)
            for i, order in enumerate(['before-first', 'after-first'] * 5)
        ]

    def test_constant_effect_and_zero_control(self):
        with patch.object(experiment, 'BOOTSTRAPS', 100):
            for saving in (0, 10, -10):
                result = experiment.summary(self.records(saving))['receipt_ms']
                self.assertEqual(result['paired_mean_saving'], saving)
                self.assertEqual(result['chunk_bootstrap_95'], [saving, saving])
                self.assertEqual(result['paired_bootstrap_95'], [saving, saving])
                self.assertEqual(result['block_mean_savings'], [saving] * 3)

    def test_mirrored_positions_cancel_linear_drift(self):
        for labels in ('abba', 'baab'):
            observations = {label: [] for label in 'ab'}
            for position, label in enumerate(labels):
                observations[label].append(100 + 7 * position - (10 if label == 'b' else 0))
            difference = sum(observations['a']) / 2 - sum(observations['b']) / 2
            self.assertEqual(difference, 10)

    def test_percentile_interpolates(self):
        self.assertEqual(experiment.percentile([30, 10, 20], 0.25), 15)

    def test_committed_records(self):
        directory = Path(__file__).resolve().parents[2] / 'docs/measurements/discovery-controlled-2026-10-18'
        for name, expected_hits in [('comparison', 300), ('quartets', 600)]:
            data = json.loads((directory / (name + '.json')).read_text())
            self.assertTrue(data['completed'])
            for keys in data['keys'].values():
                self.assertEqual(keys['before'], keys['after'])
            self.assertEqual(len(data['root_queries']), 60)
            hits = 0
            for case, count in data['protocol']['pairs_per_block'].items():
                pairs = [p for p in data['pairs'] if p['case'] == case]
                for block in range(3):
                    for order in ['before-first', 'after-first']:
                        self.assertEqual(sum(p['block'] == block and p['order'] == order for p in pairs), count // 2)
                self.assertEqual(experiment.summary(pairs), data['summary'][case])
                for pair in pairs:
                    runs = pair.get('runs', [pair['before'], pair['after']])
                    hits += len(runs)
                    for run in runs:
                        self.assertNotIn('execution', [s['stage'] for s in run['timing']['stages']])
                        metrics = run['metrics']
                        self.assertAlmostEqual(metrics['receipt_ms'], metrics['discovery_ms'] + metrics['outside_discovery_ms'])
                    if name == 'quartets':
                        first, second = ('before', 'after') if pair['order'] == 'before-first' else ('after', 'before')
                        self.assertEqual([r['label'] for r in runs], [first, second, second, first])
                        for label in ['before', 'after']:
                            for metric, value in pair[label]['metrics'].items():
                                self.assertEqual(value, statistics.fmean(r['metrics'][metric] for r in runs if r['label'] == label))
            self.assertEqual(hits, expected_hits)

    def test_selected_git_records_include_runtime_resolution(self):
        directory = Path(__file__).resolve().parents[2] / 'docs/measurements/git-selection-2026-10-18'
        data = json.loads((directory / 'controlled.json').read_text())
        self.assertTrue(data['completed'])
        self.assertEqual(data['protocol']['before_revision'], '7ba3cfe')
        for keys in data['keys'].values():
            self.assertEqual(keys['before'], keys['after'])
        runs = 0
        for case, expected in [('go', 60), ('generic', 30), ('go-aa', 60)]:
            pairs = [p for p in data['pairs'] if p['case'] == case]
            self.assertEqual(len(pairs), expected)
            self.assertEqual(experiment.summary(pairs), data['summary'][case])
            for block in range(3):
                for order in ['before-first', 'after-first']:
                    self.assertEqual(sum(p['block'] == block and p['order'] == order for p in pairs), expected // 6)
            for pair in pairs:
                first = 'before' if pair['order'] == 'before-first' else 'after'
                other = 'after' if first == 'before' else 'before'
                self.assertEqual([r['label'] for r in pair['runs']], [first, other, other, first])
                for run in pair['runs']:
                    runs += 1
                    self.assertFalse(run['timing']['failed'])
                    self.assertEqual(run['timing']['exit_code'], 0)
                    stages = {s['stage']:s for s in run['timing']['stages']}
                    self.assertNotIn('execution', stages)
                    if case != 'go-aa' and run['label'] == 'after':
                        self.assertEqual(stages['git_selection']['calls'], 1)
                        self.assertGreater(run['metrics']['git_selection_ms'], 0)
                        self.assertLess(stages['git_selection']['ns'], run['timing']['duration_ns'])
                    else:
                        self.assertNotIn('git_selection', stages)
        self.assertEqual(runs, 600)


if __name__ == '__main__':
    unittest.main()
