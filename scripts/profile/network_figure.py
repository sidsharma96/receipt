#!/usr/bin/env python3
"""Actual request timeline and explicitly separate matched-run controls."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
D=ROOT/'docs/measurements/network-deep-dive-2026-10-20'
one=json.loads((D/'run-1-summary.json').read_text())['rows'][0]['metrics']
two=json.loads((D/'run-2-summary.json').read_text())['groups']
segments=[('DNS',one['dns_ms'],'#9575cd'),('TCP',one['tcp_ms'],'#1565c0'),('TLS',one['tls_ms'],'#00838f'),('First byte',one['first_byte_ms'],'#558b2f'),('Body',one['body_ms'],'#ef6c00'),('Decode',one['decode_gap_ms'],'#ad1457')]
svg=['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 880 800" role="img" aria-labelledby="title desc">','<title id="title">Why a fresh remote fetch takes hundreds of milliseconds</title>','<desc id="desc">One actual391ms request timeline. Separate second-run server window growth and fresh versus reused controls. No fingerprinting in these times.</desc>','<rect width="880" height="800" rx="12" fill="#f7fafc"/>','<style>text{font-family:system-ui,sans-serif;fill:#172b4d;font-size:20px}.small{font-size:17px}.title{font-size:25px;font-weight:700}</style>',f'<text x="24" y="38" class="title">One actual fetch: {one["operation_ms"]:.1f} ms</text>','<text x="24" y="67" class="small">65 ms network path · fresh process · same saved result · no fingerprinting</text>']
x=24
for name,value,color in segments:
 width=value/one['operation_ms']*820
 svg.append(f'<rect x="{x:.2f}" y="90" width="{width:.2f}" height="50" fill="{color}"/>');x+=width
for i,(name,value,color) in enumerate(segments):
 col=i%3;row=i//3;x=24+col*280;y=180+row*40
 svg.extend([f'<rect x="{x}" y="{y-15}" width="16" height="16" fill="{color}"/>',f'<text x="{x+26}" y="{y}">{name}: {value:.1f} ms</text>'])
svg+=['<text x="24" y="257" class="small">About 0.4 ms of small gaps completes this one-request timeline.</text>','<text x="24" y="312" class="title">Why fresh body delivery waits for acknowledgments</text>','<text x="24" y="341" class="small">Separate 51 ms-path run: selected states from one matched server socket</text>','<defs><marker id="a" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0 L10 5 L0 10z" fill="#176b87"/></marker></defs>']
for i,window in enumerate([10,20,40,60]):
 x=24+i*218
 svg.extend([f'<rect x="{x}" y="370" width="166" height="80" rx="7" fill="#dceef5" stroke="#7da7bb"/>',f'<text x="{x+83}" y="402" text-anchor="middle" font-weight="700">{window} segments</text>',f'<text x="{x+83}" y="432" text-anchor="middle" class="small">window</text>'])
 if i<3:svg.append(f'<text x="{x+191}" y="389" text-anchor="middle" class="small">ACK</text><path d="M{x+171} 410 H{x+210}" stroke="#176b87" stroke-width="2" marker-end="url(#a)"/>')
svg+=['<text x="24" y="480" class="small">Window grows as delivery is acknowledged; intermediate states omitted.</text>','<text x="24" y="537" class="title">Keep the connection: measured diagnostic control</text>']
for i,(name,key,color) in enumerate([('Fresh process','False/fresh/fetch/False','#1565c0'),('Reused connection','True/reuse/fetch/False','#176b87')]):
 value=two[key]['metrics']['operation_ms']['median'];y=578+i*66
 svg.extend([f'<text x="24" y="{y}">{name}</text>',f'<rect x="270" y="{y-22}" width="{value/320*470:.1f}" height="30" fill="{color}" rx="4"/>',f'<text x="{280+value/320*470:.1f}" y="{y}">{value:.1f} ms</text>'])
svg+=['<text x="24" y="706" class="small">Same 51 ms-path run. Reuse excludes the first cold request; later batch.</text>','<text x="24" y="735" class="small">A resident process enables this control. No daemon or optimization adopted.</text>','<text x="24" y="771" class="small">Server warming and observer costs remain; these are not production savings.</text>','</svg>']
(ROOT/'docs/assets/network-deep-dive.svg').write_text('\n'.join(svg)+'\n')
