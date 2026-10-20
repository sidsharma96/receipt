"""Exercise Git's ignored-output boundary and the forced-add escape hatch."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from check_source_artifacts import MAX_FILE_BYTES, forbidden_paths, violations

REPOSITORY = Path(__file__).resolve().parents[2]


class ArtifactPolicy(unittest.TestCase):
    def test_current_repository_has_no_raw_json_source(self):
        self.assertEqual(forbidden_paths(REPOSITORY), [])

    def test_ignored_output_is_allowed_but_forced_add_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            (root / '.gitignore').write_text((REPOSITORY / '.gitignore').read_text())
            (root / '.tools').mkdir()
            (root / '.tools/run.json').write_text('{}\n')
            for extension in ('json', 'jsonl', 'ndjson'):
                (root / ('run.' + extension)).write_text('{}\n')
            self.assertEqual(forbidden_paths(root), [])
            subprocess.run(['git', 'add', '-f', 'run.json', 'run.jsonl', 'run.ndjson'], cwd=root, check=True)
            self.assertEqual(forbidden_paths(root), ['run.json', 'run.jsonl', 'run.ndjson'])
            # A tracked file deleted in the working tree must not block cleanup checks.
            (root / 'run.json').unlink()
            self.assertEqual(forbidden_paths(root), ['run.jsonl', 'run.ndjson'])


class StagedPolicy(unittest.TestCase):
    def test_staged_json_cannot_hide_behind_working_tree_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            path = root / 'measurement.JSON'
            path.write_text('{}\n')
            subprocess.run(['git', 'add', path.name], cwd=root, check=True)
            path.unlink()
            self.assertEqual(forbidden_paths(root), [])
            self.assertEqual(violations(root, staged=True), ['measurement.JSON'])
            subprocess.run(['git', 'rm', '--cached', '--', path.name], cwd=root, check=True, capture_output=True)
            self.assertEqual(violations(root, staged=True), [])

    def test_staged_size_cannot_hide_behind_smaller_working_copy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            path = root / 'renamed-archive.txt'
            path.write_bytes(b'x' * (MAX_FILE_BYTES + 1))
            subprocess.run(['git', 'add', path.name], cwd=root, check=True)
            path.write_text('small now\n')
            self.assertEqual(violations(root), [])
            self.assertEqual(violations(root, staged=True), [path.name])
            path.write_bytes(b'x' * MAX_FILE_BYTES)
            subprocess.run(['git', 'add', path.name], cwd=root, check=True)
            self.assertEqual(violations(root, staged=True), [])
