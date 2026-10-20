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

    def test_manual_profile_stays_on_trusted_main(self):
        text = (WORKFLOW.parent / 'profile-linux.yml').read_text()
        self.check_manual(text)
        self.check_manual((WORKFLOW.parent / 'profile-http.yml').read_text())

    def check_manual(self, text):
        self.assertIn('workflow_dispatch:', text)
        self.assertNotIn('pull_request', text)
        refs = re.findall(r"github\.ref == '([^']+)'", text)
        self.assertEqual(refs, ['refs/heads/main'])
        self.assertIn('contents: read', text)
        self.assertIn('RECEIPT_SELF_CI_TOKEN:', text)


if __name__ == '__main__':
    unittest.main()
