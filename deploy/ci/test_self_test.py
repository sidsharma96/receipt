import os
import unittest
from unittest.mock import patch

from self_test import COMMAND, remote_cache, test_environment


class PolicyTests(unittest.TestCase):
    def test_environment_is_explicit(self):
        with patch.dict(os.environ, {'RECEIPT_TEST_DATABASE_URL': 'must-not-propagate',
                                     'RECEIPT_TEST_CARGO': '1'}):
            env = test_environment('/go/bin', 'image-v1')
        self.assertNotIn('RECEIPT_TEST_DATABASE_URL', env)
        self.assertNotIn('RECEIPT_TEST_CARGO', env)
        self.assertNotIn('RECEIPT_TOKEN', env)
        self.assertEqual(env['RECEIPT_RUNNER_IMAGE'], 'image-v1')
        self.assertEqual(COMMAND, ['go', 'test', '-count=1', './...'])

    def test_missing_image_fails(self):
        with self.assertRaises(ValueError):
            test_environment('/go/bin', '')

    def test_no_secrets_does_not_start_ssh(self):
        with patch.dict(os.environ, {}, clear=True), patch('self_test.subprocess.Popen') as ssh:
            with remote_cache() as ready:
                self.assertFalse(ready)
            ssh.assert_not_called()


if __name__ == '__main__':
    unittest.main()
