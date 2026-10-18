#!/usr/bin/env python3
"""Plan actual Receipt SQL against disposable synthetic tables, never production.
Usage: python3 scripts/profile/query_plans.py DSN OUTPUT_DIRECTORY
Creates and drops only a uniquely named schema. Requires psql on PATH.
"""
import json
from pathlib import Path
import re
import subprocess
import sys
import uuid

root = Path(__file__).resolve().parents[2]
dsn, destination = sys.argv[1:]
out = Path(destination)
out.mkdir(parents=True, exist_ok=True)
schema = 'receipt_plan_' + uuid.uuid4().hex

def sql(statement):
    return subprocess.check_output(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', dsn,
                                   '-c', 'SET search_path TO ' + schema, '-c', statement], text=True)

def plan(name, statement):
    raw = sql('EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ' + statement)
    value = json.loads(raw[raw.index('['):])
    (out / (name + '.json')).write_text(json.dumps(value, indent=2) + '\n')
    (out / (name + '.txt')).write_text(sql('EXPLAIN (ANALYZE, BUFFERS) ' + statement))

try:
    sql('CREATE SCHEMA ' + schema)
    shared = (root / 'internal/cache/shared.go').read_text()
    ddl = re.search(r'`(CREATE TABLE IF NOT EXISTS receipt_entries_v2 .*?)`', shared, re.S).group(1)
    auth = (root / 'internal/auth/postgres.go').read_text()
    credentials = re.search(r'`(CREATE TABLE IF NOT EXISTS receipt_credentials_v5 .*?)`', auth, re.S).group(1)
    sql(ddl + credentials)
    sql("""INSERT INTO receipt_entries_v2(key,digest,record,quarantined,created_at)
      SELECT lpad(to_hex(n),64,'0'),repeat('a',64),jsonb_build_object('fixture',repeat('x',512)),
      n%10=0, TIMESTAMPTZ '2026-10-18 00:00:00+00' - n * interval '1 minute'
      FROM generate_series(1,10000) n;
      INSERT INTO receipt_evidence_v2(key,digest,report)
      SELECT lpad(to_hex(n),64,'0'),lpad(to_hex(d),64,'0'),'{"fixture":true}'::jsonb
      FROM generate_series(1,10000) n CROSS JOIN generate_series(1,10) d;
      INSERT INTO receipt_credentials_v5(id,token_hash,subject,scopes,expires_at)
      SELECT lpad(to_hex(n),32,'0'),repeat('a',64),'fixture',ARRAY['read'],clock_timestamp()+interval '1 day'
      FROM generate_series(1,1024) n;""")
    for table in ['receipt_entries_v2','receipt_evidence_v2','receipt_credentials_v5']:
        sql('VACUUM (ANALYZE) ' + table)
    (out / 'database.txt').write_text(sql("SELECT version()") + sql("SHOW shared_buffers") + sql("SHOW block_size") + sql("SELECT indexname,indexdef FROM pg_indexes WHERE schemaname=current_schema() ORDER BY indexname"))
    key = "lpad(to_hex(5000),64,'0')"
    plan('lookup', 'SELECT record,digest,quarantined FROM receipt_entries_v2 WHERE key=' + key)
    plan('status', 'SELECT quarantined FROM receipt_entries_v2 WHERE key=' + key)
    plan('evidence', "SELECT EXISTS(SELECT 1 FROM receipt_evidence_v2 WHERE key=" + key + " AND digest=lpad(to_hex(3),64,'0'))")
    plan('authentication', "SELECT id,subject,scopes,expires_at,token_hash,revoked_at IS NULL AND expires_at > clock_timestamp() FROM receipt_credentials_v5 WHERE id=lpad(to_hex(512),32,'0')")
    plan('recount', 'SELECT count(*),COALESCE(sum(octet_length(record::text)),0),count(*) FILTER(WHERE evidence_capped) FROM receipt_entries_v2')
    selective = "SELECT count(*) FROM receipt_entries_v2 WHERE NOT quarantined AND created_at < TIMESTAMPTZ '2026-10-18 00:00:00+00' - interval '9900 minutes'"
    broad = "SELECT count(*) FROM receipt_entries_v2 WHERE NOT quarantined AND created_at < TIMESTAMPTZ '2026-10-18 00:00:00+00' - interval '1000 minutes'"
    plan('retention-before', selective)
    sql('CREATE INDEX candidate_retention ON receipt_entries_v2(created_at) WHERE NOT quarantined; ANALYZE receipt_entries_v2;')
    plan('retention-after', selective)
    plan('retention-broad', broad)
    (out / 'candidate-index-size.txt').write_text(sql("SELECT pg_size_pretty(pg_relation_size('candidate_retention'))"))
finally:
    sql('DROP SCHEMA IF EXISTS ' + schema + ' CASCADE')
