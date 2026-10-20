#!/usr/bin/env python3
"""Draw actual Caddy byte counts, median latency and shared-process RSS chronology."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
s=json.loads((ROOT/'docs/measurements/caddy-gzip-2026-10-20/summary.json').read_text())
svg=['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 650" role="img" aria-labelledby="title desc">',
'<title id="title">Actual Caddy gzip: fewer bytes, a small latency difference on this short path</title>',
'<desc id="desc">Identity writes 64,252 body bytes; gzip writes about 20,040. Both decode identically. Median CLI latency is 120.53 versus 118.22 milliseconds. One Caddy process carries memory between batches: sampled maximum RSS grows from 55 to 59 megabytes across both encodings.</desc>',
'<rect width="960" height="650" rx="14" fill="#111827"/>',
'<g font-family="system-ui,sans-serif" fill="#e5e7eb">',
'<text x="34" y="38" font-size="23" font-weight="bold">Actual Caddy endpoint: same decoded result, fewer body bytes</text>',
'<text x="34" y="68" font-size="16">90 fetches per encoding • median TCP connection about 1.9 ms</text>']
for case,y,color in [('identity',108,'#89b4fa'),('gzip',158,'#a6e3a1')]:
 v=s['variants'][case];size=v['encoded_bytes']['median'];width=size*.0087
 svg.extend([f'<text x="34" y="{y+23}" font-size="17">{case.title()}</text>',f'<rect x="155" y="{y}" width="{width:.2f}" height="34" fill="{color}"/>',
 f'<text x="165" y="{y+23}" font-size="16" fill="#111827">{int(size):,} B</text>',
 f'<path d="M {155+width:.2f} {y+17} H 785" stroke="{color}" stroke-width="2" stroke-dasharray="5 4"/>',
 f'<path d="M 778 {y+11} l 8 6 l -8 6" fill="none" stroke="{color}" stroke-width="2"/>'])
svg.extend(['<text x="800" y="130" font-size="16">Go reconstructs</text>','<text x="800" y="155" font-size="16">64,252 B</text>',
'<text x="800" y="180" font-size="16">Same SHA-256</text>','<text x="34" y="225" font-size="15">68.8% less encoded body data; headers and TLS/TCP overhead excluded.</text>',
'<path d="M 34 246 H 926" stroke="#4b5563"/>','<text x="34" y="278" font-size="21" font-weight="bold">Whole CLI median latency (common scale from zero)</text>'])
for case,y,color in [('identity',299,'#89b4fa'),('gzip',338,'#a6e3a1')]:
 v=s['variants'][case]['elapsed_ms']['median'];w=v*4.5
 svg.extend([f'<text x="34" y="{y+22}" font-size="17">{case.title()}</text>',f'<rect x="155" y="{y}" width="{w:.2f}" height="29" fill="{color}"/>',
 f'<text x="{165+w:.2f}" y="{y+21}" font-size="16">{v:.2f} ms</text>'])
svg.extend(['<path d="M 34 392 H 926" stroke="#4b5563"/>',
'<text x="34" y="424" font-size="21" font-weight="bold">One Caddy process carries memory across both encodings</text>',
'<text x="34" y="451" font-size="15">Sampled max RSS by batch in time order; these are not fresh per-variant peaks.</text>'])
ordered=sorted(s['batches'],key=lambda b:b['resource_window']['start_ns'])
points=[]
for i,b in enumerate(ordered):
 x=155+i*133;value=b['resource_window']['services']['caddy']['sampled_max_rss_mb'];y=562-(value-54)*13
 color='#a6e3a1' if b['case']=='gzip' else '#89b4fa'
 points.append(f'{x},{y:.2f}')
 svg.extend([f'<circle cx="{x}" cy="{y:.2f}" r="5" fill="{color}"/>',f'<text x="{x}" y="{y-13:.2f}" text-anchor="middle" font-size="15">{value:.2f} MB</text>',
 f'<text x="{x}" y="591" text-anchor="middle" font-size="15">{b["case"]} {b["block"]+1}</text>'])
svg.extend([f'<polyline points="{" ".join(points)}" stroke="#9ca3af" fill="none" stroke-width="2"/>',
'<text x="34" y="630" font-size="15">All 180 calls retained. Original production configuration restored after the test.</text>','</g></svg>'])
(ROOT/'docs/assets/caddy-gzip-comparison.svg').write_text('\n'.join(svg)+'\n')
