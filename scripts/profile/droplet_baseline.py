#!/usr/bin/env python3
"""Read-only droplet observation. Run through administrator SSH as root.
Modes: snapshot, plans, logs SINCE_UNIX_SECONDS. Never emit rows or credentials.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


def run(args):
    return subprocess.check_output(args, text=True, timeout=20).strip()


def sql(statement):
    prefix = "BEGIN READ ONLY; SET LOCAL statement_timeout='3s'; "
    value = run(['runuser', '-u', 'postgres', '--', 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-d', 'receipt', '-c', prefix + statement + '; COMMIT'])
    return value


def snapshot():
    pid = int(run(['systemctl', 'show', 'receipt', '-p', 'MainPID', '--value']))
    stat = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
    usage = dict(line.split(':', 1) for line in Path(f'/proc/{pid}/io').read_text().splitlines())
    ticks = os.sysconf('SC_CLK_TCK')
    status = Path(f'/proc/{pid}/status').read_text()
    memory = {k: int(v) for k, v in re.findall(r'^(VmRSS|VmHWM|RssAnon|RssFile):\s*(\d+) kB', status, re.M)}
    return {'unix_ns': time.time_ns(), 'pid': pid, 'start_ticks': int(stat[19]),
            'cpu_seconds': (int(stat[11])+int(stat[12])) / ticks, 'memory_kb': memory,
            'io': {k: int(v) for k, v in usage.items()},
            'host': {'cpus': os.cpu_count(), 'memory_kb': {k: int(v) for k, v in re.findall(r'^(MemTotal|MemAvailable|SwapTotal):\s*(\d+) kB', Path('/proc/meminfo').read_text(), re.M)},
                     'load': os.getloadavg(), 'kernel': run(['uname', '-srmo'])},
            'binary_sha256': hashlib.sha256(Path('/usr/local/bin/receipt').read_bytes()).hexdigest()}


def plans():
    key = sql('SELECT key FROM receipt_entries_v2 WHERE NOT quarantined LIMIT 1')
    credential = sql('SELECT id FROM receipt_credentials_v5 WHERE revoked_at IS NULL AND expires_at>clock_timestamp() LIMIT 1')
    assert re.fullmatch('[a-f0-9]{64}', key) and re.fullmatch('[a-f0-9]{32}', credential)
    queries = {'lookup': f"SELECT record,digest,quarantined FROM receipt_entries_v2 WHERE key='{key}'",
               'status': f"SELECT quarantined FROM receipt_entries_v2 WHERE key='{key}'",
               'authentication': f"SELECT id,subject,scopes,expires_at,token_hash,revoked_at IS NULL AND expires_at>clock_timestamp() FROM receipt_credentials_v5 WHERE id='{credential}'"}
    result = {'version': sql('SELECT version()'), 'captured_ns': time.time_ns(),
              'table_estimates': json.loads(sql("SELECT json_agg(row_to_json(t)) FROM (SELECT relname,n_live_tup FROM pg_stat_user_tables ORDER BY relname) t")),
              'indexes': json.loads(sql("SELECT json_agg(row_to_json(t)) FROM (SELECT indexname,indexdef FROM pg_indexes WHERE schemaname='public' ORDER BY indexname) t")),
              'plans': {}, 'scope': 'Actual production SELECT plans, read-only, statement timeout 3s; values in Index Cond redacted, no rows or credentials exported.'}
    for name, query in queries.items():
        value = sql('EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ' + query)
        result['plans'][name] = json.loads(value.replace(key, '<sample-key>').replace(credential, '<sample-credential-id>'))
    return result


def logs(since):
    raw = run(['journalctl', '-u', 'receipt', '--since', '@' + str(int(since)), '-o', 'json', '--no-pager'])
    records = []
    allowed = {'version', 'request_id', 'operation', 'status', 'duration_ns', 'stages'}
    for line in raw.splitlines():
        entry = json.loads(line)
        try:
            event = json.loads(entry.get('MESSAGE', ''))
        except (ValueError, TypeError):
            continue
        if isinstance(event, dict) and set(event) == allowed:
            records.append({'journal_unix_us': int(entry['__REALTIME_TIMESTAMP']), **event})
    return {'captured_ns': time.time_ns(), 'since_unix_seconds': int(since), 'events': records,
            'scope': 'All matching request summaries in this window, including any unrelated traffic; excludes application output and journal metadata.'}


if __name__ == '__main__':
    mode = sys.argv[1]
    if mode == 'snapshot':
        result = snapshot()
    elif mode == 'plans':
        result = plans()
    elif mode == 'logs':
        result = logs(sys.argv[2])
    else:
        raise SystemExit('expected snapshot, plans, or logs SINCE_UNIX_SECONDS')
    print(json.dumps(result, indent=2))
