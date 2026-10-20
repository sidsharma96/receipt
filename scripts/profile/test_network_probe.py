import unittest
from network_deep_dive import packet_record


class HeaderRecords(unittest.TestCase):
    def test_header_only_projection(self):
        r=packet_record('1792525000.123456 IP 104.248.183.112.443 > 10.1.2.3.41234: Flags [P.], seq 10:1458, ack 250, win 501, length 1448')
        self.assertTrue(r['incoming'])
        self.assertEqual(r['client_port'],41234)
        self.assertEqual(r['length'],1448)
        self.assertEqual(r['seq_end'],1458)
        self.assertNotIn('104.248.183.112',str(r))
        self.assertNotIn('10.1.2.3',str(r))

    def test_outgoing_and_unexpected_lines(self):
        r=packet_record('1792525000.123456 IP 10.1.2.3.41234 > 104.248.183.112.443: Flags [S], seq 10, win 501, length 0')
        self.assertFalse(r['incoming'])
        self.assertIsNone(packet_record('tcpdump: listening on eth0'))
        self.assertIsNone(packet_record('1.0 IP 1.1.1.1.443 > 2.2.2.2.443: Flags [S], length 0'))
