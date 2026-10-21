import json
from pathlib import Path
import tempfile
import unittest

from compare_network_runs import compare, load_run


def fixture(run_id):
    meta = dict(completed=True, run_id=run_id, source_commit='fixed', go_version='fixed',
                fixture_key='fixed', cgo_enabled='0', runner_image='fixed',
                probe_sha256={'network': 'fixed'})
    rows = []
    def row(mode, endpoint, index, block, persistent=False, h1=False):
        # The first persistent request is deliberately much slower.
        elapsed = 999 if persistent and index == 0 else 20
        events = [{'name': name, 'ns': ms * 1000000, 'cpu_us': ms * 100}
                  for name, ms in [('operation_start', 0), ('body_read_start', 2),
                                   ('body_read_done', 10), ('body_close_start', 15),
                                   ('operation_done', elapsed)]]
        return dict(mode=mode, endpoint=endpoint, index=index, block=block,
                    persistent_control=persistent, http1_control=h1, failed=False,
                    root_load_ms=0, decoded_sha256='same',
                    trace=dict(status=200 if endpoint == 'fetch' else 204,
                               request_id=str(len(rows)), body_sha256='same', body_bytes=100,
                               reused=persistent and mode == 'reuse' and index > 0, events=events))
    for block in range(12):
        for mode, endpoint, h1 in [('fresh', 'fetch', False), ('preload', 'fetch', False),
                                   ('pinned', 'fetch', False), ('fresh', 'status', False),
                                   ('fresh', 'fetch', True)]:
            rows.append(row(mode, endpoint, 0, block, h1=h1))
    for mode in ('fresh', 'resume', 'reuse'):
        for index in range(20):
            rows.append(row(mode, 'fetch', index, 12, persistent=True))
    return meta, rows


class Comparison(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.dirs = []
        for run_id in ('one', 'two'):
            directory = Path(self.temp.name) / run_id
            directory.mkdir()
            meta, rows = fixture(run_id)
            (directory / 'metadata.json').write_text(json.dumps(meta))
            (directory / 'requests.json').write_text(json.dumps(rows))
            self.dirs.append(directory)

    def mutate(self, filename, change):
        path = self.dirs[1] / filename
        value = json.loads(path.read_text())
        change(value)
        path.write_text(json.dumps(value))

    def test_scopes_exclude_only_first_reuse_request(self):
        result = compare(self.dirs)
        self.assertEqual(result['requests'], 240)
        self.assertEqual(result['fetches'], 216)
        self.assertEqual(result['runs'][0]['fresh']['operation_ms']['n'], 12)
        self.assertEqual(result['runs'][0]['reused']['operation_ms']['n'], 19)
        self.assertEqual(result['runs'][0]['reused']['operation_ms']['max'], 20)
        self.assertIn('not controlled geography', result['scope'])

    def test_binary_drift_is_rejected(self):
        self.mutate('metadata.json', lambda m: m['probe_sha256'].update(network='different'))
        with self.assertRaisesRegex(ValueError, 'binary drift'):
            compare(self.dirs)

    def test_result_drift_between_runs_is_rejected(self):
        def change(rows):
            for row in rows:
                row['decoded_sha256'] = 'different'
        self.mutate('requests.json', change)
        with self.assertRaisesRegex(ValueError, 'cross-run result drift'):
            compare(self.dirs)

    def test_failures_and_incomplete_records_are_not_filtered(self):
        self.mutate('requests.json', lambda rows: rows[0].update(failed=True))
        with self.assertRaisesRegex(ValueError, 'not dropped'):
            load_run(self.dirs[1])
        self.mutate('requests.json', lambda rows: rows.pop())
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            load_run(self.dirs[1])

    def test_reused_connection_cannot_masquerade_as_fresh(self):
        self.mutate('requests.json', lambda rows: rows[0]['trace'].update(reused=True))
        with self.assertRaisesRegex(ValueError, 'lifetime'):
            load_run(self.dirs[1])

    def test_same_run_cannot_be_counted_twice(self):
        with self.assertRaisesRegex(ValueError, 'distinct'):
            compare([self.dirs[0], self.dirs[0]])
