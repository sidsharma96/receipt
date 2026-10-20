#!/usr/bin/env python3
"""Draw one actual client timeline. Server duration is separate because it overlaps."""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
s=json.loads((ROOT/'.tools/measurements/http-trace-2026-10-19/summary.json').read_text())
e=s['examples']['remote-fetch']
t=e['client']['http'][0]
clock={x['name']:x['ns']/1e6 for x in t['events']}
start=clock['request_start']
scale=4.5
x0=70
parts=[('DNS','dns_start','dns_done','#89b4fa'),('TCP','connect_start','connect_done','#74c7ec'),
       ('TLS','tls_start','tls_done','#cba6f7'),('First byte','request_written','first_response_byte','#f9e2af'),
       ('Body read','body_read_start','body_read_done','#a6e3a1'),('Decode','decode_start','decode_done','#fab387')]
svg=['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 960 410" role="img" aria-labelledby="title desc">',
 '<title id="title">One Go HTTP remote fetch: 172 milliseconds</title>',
 '<desc id="desc">The GitHub client spends about 3 milliseconds on DNS, 24 connecting, 38 on TLS, 50 to first byte, 48 reading and 8 decoding. The matching server handler takes 26 milliseconds within the client wait and transfer; it must not be added.</desc>',
 '<rect width="960" height="410" rx="14" fill="#111827"/>',
 '<g font-family="system-ui,sans-serif" fill="#e5e7eb">',
 '<text x="36" y="40" font-size="23" font-weight="bold">Follow one real Go HTTP fetch on Linux CI</text>',
 '<text x="36" y="68" font-size="16">Client clocks relative to request start • HTTP/2 • 60,238 body bytes</text>',
 '<text x="36" y="105" font-size="16">GitHub runner</text>',
 f'<rect x="{x0}" y="150" width="{(clock["body_closed"]-start)*scale:.2f}" height="54" fill="#4b5563"/>']
for i,(label,a,b,color) in enumerate(parts):
 x=x0+(clock[a]-start)*scale;w=(clock[b]-clock[a])*scale
 svg.append(f'<rect x="{x:.2f}" y="150" width="{w:.2f}" height="54" fill="{color}"/>')
 # Small segments receive external labels; all labels are kept horizontal.
 if label in ('DNS','Decode'):
  target=36 if label=='DNS' else 846
  svg.append(f'<path d="M {x+w/2:.2f} 150 V 128 H {target}" fill="none" stroke="{color}"/>')
  svg.append(f'<text x="{target}" y="120" font-size="14" fill="{color}">{label} {clock[b]-clock[a]:.1f} ms</text>')
 else:
  svg.append(f'<text x="{x+w/2:.2f}" y="173" text-anchor="middle" fill="#111827" font-size="15" font-weight="bold">{label}</text>')
  svg.append(f'<text x="{x+w/2:.2f}" y="193" text-anchor="middle" fill="#111827" font-size="14">{clock[b]-clock[a]:.1f} ms</text>')
for n in (0,40,80,120,160):
 x=x0+n*scale
 svg.extend([f'<path d="M{x} 208 v8" stroke="#9ca3af"/>',f'<text x="{x}" y="238" text-anchor="middle" font-size="14">{n} ms</text>'])
svg.extend(['<text x="36" y="275" font-size="16">Gray slivers: setup and gaps between measured operations.</text>',
 '<path d="M 36 295 H 924" stroke="#4b5563"/>',
 f'<text x="36" y="324" font-size="18">Matched droplet handler: {e["match"]["phases"]["server_handler_ms"]:.1f} ms</text>',
 '<text x="36" y="350" font-size="16">The server works while the client waits and receives bytes. These times overlap.</text>',
 '<text x="36" y="378" font-size="15">172 ms HTTP span; 228 ms whole CLI invocation. One request, not an average.</text>',
 '</g></svg>'])
(ROOT/'docs/assets/go-http-trace.svg').write_text('\n'.join(svg)+'\n')
