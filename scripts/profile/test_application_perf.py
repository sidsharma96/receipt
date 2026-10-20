import unittest
from summarize_application_perf import summarize


class Samples(unittest.TestCase):
    def test_inclusive_groups_do_not_count_recursive_frames_twice(self):
        report = summarize('receipt 1/2 1.0: cpu-clock:u: \n  123 runtime.mallocgc\n  456 receipt/internal/strictjson.Object\n  789 receipt/internal/strictjson.Object\n\nreceipt 1/2 2.0: cpu-clock:u: \n  123 encoding/json.stateInString\n  456 receipt/internal/cache.Encode\n')
        self.assertEqual(report['samples'],2)
        self.assertEqual(report['inclusive_groups']['strict_object'],1)
        self.assertEqual(report['inclusive_groups']['encode'],1)
        self.assertEqual(report['leaf_groups']['runtime.mallocgc'],1)
        self.assertEqual(report['leaf_groups']['encoding/json.'],1)

    def test_missing_frames_stay_in_denominator(self):
        report = summarize("receipt 1/2 1.0: cpu-clock:u: ")
        self.assertEqual(report["samples"],1)
        self.assertEqual(report["samples_without_frames"],1)

    def test_empty_and_wrong_events_rejected(self):
        for value in ('','receipt 1/2 1.0: cycles:\n  123 anything'):
            with self.assertRaises(ValueError):
                summarize(value)
