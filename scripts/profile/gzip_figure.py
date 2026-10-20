#!/usr/bin/env python3
"""Draw measured body sizes and median whole-CLI latency; never sum phase medians."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
s=json.loads((ROOT/'docs/measurements/gzip-2026-10-20/summary.json').read_text())
svg=['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 640" role="img" aria-labelledby="title desc">',
'<title id="title">Gzip sends 70 percent fewer body bytes and helps on the delayed link</title>',
'<desc id="desc">Identity transfers 62,728 body bytes; gzip transfers 19,073. Both decode to the same 62,728-byte result. Median whole-command latency is 69.5 versus 71.3 milliseconds without added delay, 192.3 versus 169.2 at 24 milliseconds added round-trip delay, and 392.4 versus 329.2 at 64 milliseconds.</desc>',
'<rect width="960" height="640" rx="14" fill="#111827"/>',
'<g font-family="system-ui,sans-serif" fill="#e5e7eb">',
'<text x="34" y="38" font-size="23" font-weight="bold">Same saved result, fewer transferred body bytes</text>',
'<text x="34" y="68" font-size="16">Linux CI • current Receipt handler • fresh Go gzip level-5 encoder</text>',
'<text x="155" y="105" font-size="15">Server → encoded HTTP body (bars share one byte scale)</text>']
for case,y,color in [('identity',132,'#89b4fa'),('gzip',186,'#a6e3a1')]:
 v=s['conditions']['12']['variants'][case]
 size=v['encoded_bytes']['median'];width=size*.0088
 svg.extend([f'<text x="34" y="{y+23}" font-size="17">{case.title()}</text>',
 f'<rect x="155" y="{y}" width="{width:.2f}" height="36" fill="{color}"/>',
 f'<text x="165" y="{y+24}" font-size="16" fill="#111827">{int(size):,} B</text>',
 f'<path d="M {155+width:.2f} {y+18} H 785" stroke="{color}" stroke-width="2" stroke-dasharray="5 4"/>',
 f'<path d="M 778 {y+12} l 8 6 l -8 6" fill="none" stroke="{color}" stroke-width="2"/>'])
svg.extend(['<text x="800" y="150" font-size="16">Client decodes</text>',
'<text x="800" y="174" font-size="16">62,728 B</text>',
'<text x="800" y="200" font-size="16">Same SHA-256</text>',
'<text x="34" y="254" font-size="16">69.6% less HTTP body data; headers, TLS and TCP overhead are excluded.</text>',
'<path d="M 34 277 H 926" stroke="#4b5563"/>',
'<text x="34" y="312" font-size="22" font-weight="bold">Whole CLI latency: 60 measured calls per bar</text>',
'<rect x="688" y="297" width="15" height="15" fill="#89b4fa"/><text x="711" y="311" font-size="15">Identity</text>',
'<rect x="804" y="297" width="15" height="15" fill="#a6e3a1"/><text x="827" y="311" font-size="15">Gzip</text>'])
for index,delay in enumerate((0,12,32)):
 y=346+index*76
 svg.append(f'<text x="34" y="{y+29}" font-size="16">+{delay*2} ms RTT</text>')
 for case,offset,color in [('identity',0,'#89b4fa'),('gzip',29,'#a6e3a1')]:
  value=s['conditions'][str(delay)]['variants'][case]['elapsed_ms']['median'];width=value*1.55
  svg.extend([f'<rect x="170" y="{y+offset}" width="{width:.2f}" height="23" fill="{color}"/>',
    f'<text x="{180+width:.2f}" y="{y+offset+17}" font-size="15">{value:.1f} ms</text>'])
svg.extend(['<text x="34" y="596" font-size="15">RTT = round-trip delay added on a private link, not Internet latency.</text>',
'<text x="34" y="621" font-size="15">All 360 calls retained, including outliers. Medians; no production deployment claim.</text>',
'</g></svg>'])
(ROOT/'docs/assets/gzip-comparison.svg').write_text('\n'.join(svg)+'\n')
