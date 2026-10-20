"""Validate published diagnostic records; does not run or tune the runtime."""
import json
from pathlib import Path
import unittest
import tempfile
from summarize_system_trace import extract

DATA = Path(__file__).resolve().parents[2] / '.tools/measurements/variance-diagnosis-2026-10-18'


class DiagnosticEvidence(unittest.TestCase):
    @unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
    def test_counts_and_success(self):
        for mode in ('light', 'traced'):
            data = json.loads((DATA / (mode + '.json')).read_text())
            self.assertTrue(data['completed'])
            self.assertEqual(data['source_revision'], '97b833e')
            for case, expected in [('go', 60), ('generic', 40)]:
                rows = [r for r in data['records'] if r['case'] == case]
                self.assertEqual(len(rows), expected)
                for row in rows:
                    timing = next(e for e in row['events'] if e['type'] == 'receipt_timing')
                    self.assertFalse(timing['failed'])
                    self.assertEqual(timing['exit_code'], 0)
                    self.assertNotIn('execution', [s['stage'] for s in timing['stages']])
                    gc = next(e for e in row['events'] if e['type'] == 'diagnostic_gc')
                    self.assertEqual(gc['collections'], 1 if mode == 'light' else 2)

    @unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
    def test_small_pauses_during_reproduced_spike(self):
        data = json.loads((DATA / 'light.json').read_text())
        row = next(r for r in data['records'] if r['index'] == 10)
        gc = next(e for e in row['events'] if e['type'] == 'diagnostic_gc')
        timing = next(e for e in row['events'] if e['type'] == 'receipt_timing')
        listing = next(e for e in row['events'] if e.get('operation') == 'ls-files')
        self.assertGreater(timing['duration_ns'], 500_000_000)
        self.assertLess(gc['pause_ns'], 100_000)
        self.assertGreater(listing['elapsed_ns'], 300_000_000)
        self.assertLess(listing['cpu_ns'], 25_000_000)

    def test_system_export_cross_table_references(self):
        # xctrace omits the second schema and keeps IDs across table nodes.
        xml = """<trace-query-result><node><schema name="syscall">
        <col><mnemonic>start</mnemonic></col><col><mnemonic>thread</mnemonic></col>
        <col><mnemonic>call</mnemonic></col><col><mnemonic>duration</mnemonic></col>
        <col><mnemonic>process</mnemonic></col></schema>
        <row><start-time id="1">10</start-time><thread id="2" fmt="Main (git, pid: 7)"/>
        <syscall fmt="read"/><duration>20</duration><process id="3" fmt="git (7)"/></row>
        </node><node><row><start-time ref="1"/><thread ref="2"/>
        <thread-state fmt="Runnable">Runnable</thread-state><duration>30</duration>
        <process ref="3"/></row></node></trace-query-result>"""
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'trace.xml'
            path.write_text(xml)
            rows = extract(path)
            self.assertEqual(extract(path, [7]), rows)
            self.assertEqual(extract(path, [70]), [])
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[1]['schema'], 'thread-state')
        self.assertEqual(rows[1]['fields']['process']['fmt'], 'git (7)')
        self.assertEqual(rows[1]['fields']['start']['text'], '10')
        self.assertEqual(rows[1]['fields']['state']['text'], 'Runnable')

    @unittest.skipUnless(DATA.exists(), "restore historical measurements: python3 scripts/profile/restore_measurements.py")
    def test_child_gc_detection_positive_control(self):
        entries = json.loads((DATA / 'gc-positive-control.json').read_text())
        probe = next(e for e in entries if e.get('program') == 'go')
        self.assertEqual(len(probe['gc_lines']), 4)
        for line in probe['gc_lines']:
            self.assertTrue(line.startswith('gc '))
            self.assertIn(' ms clock,', line)


if __name__ == '__main__':
    unittest.main()
