#!/usr/bin/env python3
"""Extract only Git/Receipt events from an xctrace syscall/thread-state XML export.
Usage: summarize_system_trace.py INPUT.xml OUTPUT.json
Raw system-wide exports should remain local.
"""
import json
from pathlib import Path
import sys
import xml.etree.ElementTree as ET

def extract(path):
    refs = {}
    rows = []
    keep = {'start-time','duration','duration-on-core','duration-waiting',
            'thread','process','syscall','thread-state','thread-state-reason',
            'frame','tagged-backtrace','thread-id','tid','pid','narrative','backtrace'}
    columns = []
    schema = ''
    def value(element):
        if 'ref' in element.attrib:
            return refs.get(element.attrib['ref'], {})
        if element.get('id') in refs:
            return refs[element.get('id')]
        return {'tag':element.tag,'text':element.text or '', 'fmt':element.get('fmt',''),
                'name':element.get('name','')}
    for event, element in ET.iterparse(path, events=('start','end')):
        if event == 'start':
            continue
        if element.tag == 'schema':
            schema = element.get('name')
            columns = [c.findtext('mnemonic') for c in element.findall('col')]
        if element.tag in keep and 'id' in element.attrib:
            entry = value(element)
            if element.tag == 'tagged-backtrace':
                entry['frames'] = [value(f).get('name','') for f in element.findall('frame')]
            refs[element.attrib['id']] = entry
        if element.tag == 'row':
            row_schema = schema
            row_columns = columns
            if len(element)>2 and element[2].tag == 'thread-state':
                row_schema = 'thread-state'
                row_columns = ['start','thread','state','duration','process'] + [f'field{i}' for i in range(5,len(element))]
            fields = {column:value(child) for column,child in zip(row_columns,element)}
            process = fields.get('process',{}).get('fmt','')
            thread = fields.get('thread',{}).get('fmt','')
            if process.startswith(('git (','receipt-diagnostic (')) or '(git, pid:' in thread or '(receipt-diagnostic, pid:' in thread:
                rows.append({'schema':row_schema, 'fields':fields})
            element.clear()
    return rows

if __name__ == '__main__':
    rows = extract(sys.argv[1])
    Path(sys.argv[2]).write_text(json.dumps(rows,indent=2)+'\n')
    print('Retained target rows:',len(rows))
