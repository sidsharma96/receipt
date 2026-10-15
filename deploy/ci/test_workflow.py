"""The cached job receives private-cache credentials, so only main may run it."""
from pathlib import Path
import re
import unittest

WORKFLOW = Path(__file__).resolve().parents[2] / '.github' / 'workflows' / 'receipt-ci.yml'


class WorkflowPolicy(unittest.TestCase):
    def test_only_main_receives_credentials(self):
        text = WORKFLOW.read_text()
        self.assertIn('branches: [main]', text)
        refs = re.findall(r"github\.ref == '([^']+)'", text)
        self.assertEqual(refs, ['refs/heads/main'])


if __name__ == '__main__':
    unittest.main()
