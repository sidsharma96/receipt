import unittest
from network_deep_dive import packet_record
from analyze_server_tcp import match
from analyze_dns_packets import summarize as dns_summary
from summarize_network import stages


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


class Attribution(unittest.TestCase):
    def test_body_span_excludes_decode(self):
        row={'root_load_ms':3,'trace':{'events':[{'name':name,'ns':ms*1000000,'cpu_us':ms*100} for name,ms in [('operation_start',0),('body_read_start',2),('body_read_done',12),('body_close_start',19),('operation_done',20)]]}}
        result=stages(row)
        self.assertEqual(result['body_ms'],10)
        self.assertEqual(result['decode_gap_ms'],7)
        self.assertEqual(result['including_root_ms'],23)

    def test_nat_ports_do_not_break_sequence_matching(self):
        request={'client_port':1234,'server_initial_seq':4294967295}
        kernel=[{'client_port':6543,'sock_cookie':9,'snd_nxt':0,'snd_una':0}]
        result=match(kernel,[request])
        self.assertEqual(result['matched_connections'],1)
        self.assertEqual(result['rows'][0]['kernel'][0]['client_port'],6543)
        ambiguous=kernel+[dict(kernel[0],sock_cookie=10)]
        self.assertEqual(match(ambiguous,[request])['matched_connections'],0)

    def test_dns_duplicate_capture_is_not_counted_as_retry(self):
        query={'frame.time_epoch':'1.0','ip.src':'127.0.0.1','ip.dst':'127.0.0.53','udp.srcport':'1234','udp.dstport':'53','dns.id':'0x12','dns.qry.type':'28','dns.flags.response':'False','dns.flags.rcode':'','dns.count.answers':'0','dns.resp.ttl':''}
        response=dict(query,**{'frame.time_epoch':'1.1','ip.src':'127.0.0.53','ip.dst':'127.0.0.1','udp.srcport':'53','udp.dstport':'1234','dns.flags.response':'True','dns.flags.rcode':'0'})
        result=dns_summary([query,dict(query,**{'frame.time_epoch':'1.000001'}),response])
        self.assertEqual(result['unique_rows'],2)
        self.assertEqual(result['groups']['stub/28']['n'],1)
        self.assertAlmostEqual(result['groups']['stub/28']['median_ms'],100)


class FailureEvidence(unittest.TestCase):
    def test_failed_child_numeric_record_is_retained(self):
        import json
        from pathlib import Path
        import subprocess
        import tempfile
        from unittest.mock import patch
        from network_deep_dive import record_probe
        row = {'endpoint': 'status', 'failed': True, 'trace': {'status': 503}}
        child = subprocess.CompletedProcess(['probe'], 1, json.dumps(row) + '\n', 'private diagnostic')
        with tempfile.TemporaryDirectory() as tmp, patch('network_deep_dive.subprocess.run', return_value=child):
            path = Path(tmp) / 'requests.json'
            with self.assertRaisesRegex(RuntimeError, 'numeric records retained'):
                record_probe(['probe'], {}, [], path, block=0)
            saved = path.read_text()
            self.assertNotIn('private diagnostic', saved)
            self.assertEqual(json.loads(saved)[0]['trace']['status'], 503)
            self.assertTrue(json.loads(saved)[0]['failed'])
