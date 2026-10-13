import os
import unittest
from unittest.mock import patch, MagicMock
import urllib.error

from self_test import COMMAND, REMOTE, NoRedirect, remote_cache, test_environment
import cleanup


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

    def test_old_experiment_cleanup_cannot_disable_new_key(self):
        with patch('cleanup.remote', return_value=b'restrict ssh-ed25519 AAAA receipt-self-ci\n') as remote:
            with self.assertRaises(RuntimeError):
                cleanup.main()
            self.assertEqual(remote.call_count, 1)

    def test_https_needs_only_token_and_checks_identity(self):
        response = MagicMock()
        response.__enter__.return_value.read.return_value = b'{"subject":"receipt-self-ci"}'
        with patch.dict(os.environ, {'RECEIPT_SELF_CI_TOKEN': 'test-token'}, clear=True), \
                patch('self_test.urllib.request.build_opener') as build, \
                patch('self_test.subprocess.Popen') as ssh:
            build.return_value.open.return_value = response
            with remote_cache() as ready:
                self.assertTrue(ready)
            request = build.return_value.open.call_args.args[0]
            self.assertEqual(request.full_url, REMOTE + '/v1/identity')
            self.assertTrue(REMOTE.startswith('https://'))
            self.assertEqual(request.get_header('Authorization'), 'Bearer test-token')
            build.assert_called_once_with(NoRedirect)
            ssh.assert_not_called()

    def test_unavailable_or_invalid_identity_falls_back(self):
        for payload in (b'{}', b'[]', b'not json', b'{"subject":"other"}'):
            response = MagicMock()
            response.__enter__.return_value.read.return_value = payload
            with patch.dict(os.environ, {'RECEIPT_SELF_CI_TOKEN': 'test-token'}, clear=True), \
                    patch('self_test.urllib.request.build_opener') as build:
                build.return_value.open.return_value = response
                with remote_cache() as ready:
                    self.assertFalse(ready)
        with patch.dict(os.environ, {'RECEIPT_SELF_CI_TOKEN': 'test-token'}, clear=True), \
                patch('self_test.urllib.request.build_opener') as build:
            build.return_value.open.side_effect = urllib.error.URLError('TLS or network failure')
            with remote_cache() as ready:
                self.assertFalse(ready)

    def test_redirect_is_not_followed(self):
        self.assertIsNone(NoRedirect().redirect_request(None, None, 302, '', {},
                                                       'https://other.example'))


if __name__ == '__main__':
    unittest.main()
