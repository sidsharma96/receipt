#!/usr/bin/env python3
"""Draw measured disjoint CPU leaf groups beside the parsing sequence."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
d = json.loads((ROOT/'.tools/measurements/application-profile-2026-10-20/perf-summary.json').read_text())
g = d['leaf_groups']; n = d['samples']
rows = [('JSON functions',g['encoding/json.']),('Regular expressions',g['regexp.']+g['regexp/']),('SHA-256',g['crypto/sha256.']),('Allocator',g['runtime.mallocgc'])]
rows.append(('Other / missing frames',n-sum(v for _,v in rows)))
svg = ['<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 760 690" role="img" aria-labelledby="title desc">',
       '<title id="title">Live Receipt CPU samples and repeated JSON reading</title>',
       '<desc id="desc">Disjoint leaf CPU categories and a three-step shape-check, typed-decode and nested-check sequence. CPU samples do not measure waiting.</desc>',
       '<rect width="760" height="690" rx="12" fill="#f7fafc"/>',
       '<style>text{font-family:system-ui,sans-serif;fill:#172b4d;font-size:19px}.small{font-size:16px}.heading{font-size:23px;font-weight:700}</style>',
       '<text x="24" y="38" class="heading">Where the CPU sample stopped</text>',
       '<text x="24" y="66" class="small">1,589 live user-CPU samples · categories do not overlap</text>']
for i,(name,count) in enumerate(rows):
    y = 102+i*51
    svg += [f'<text x="24" y="{y}">{name}</text>',f'<rect x="300" y="{y-19}" width="{count/n*620:.1f}" height="26" rx="4" fill="#176b87"/>',f'<text x="{max(316,310+count/n*620):.1f}" y="{y}">{count/n*100:.1f}%</text>']
svg += ['<text x="24" y="377" class="heading">Why one object is read again</text>',
        '<defs><marker id="arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#176b87"/></marker></defs>']
steps = [('1  Check shape','Read names; copy each field into RawMessage.'),('2  Fill Go fields','json.Unmarshal reads the original object again.'),('3  Enter a nested type','Its checking hook repeats steps 1 and 2.')]
for i,(title,desc) in enumerate(steps):
    y=399+i*87
    svg += [f'<rect x="24" y="{y}" width="712" height="65" rx="7" fill="#e0eef5" stroke="#80a9be"/>',f'<text x="40" y="{y+26}" font-weight="700">{title}</text>',f'<text x="40" y="{y+51}">{desc}</text>']
    if i<2: svg += [f'<path d="M 380 {y+65} V {y+84}" stroke="#176b87" stroke-width="2" marker-end="url(#arrow)"/>']
svg += ['<text x="24" y="680" class="small">Sequence illustrates current code; it is not a count of scans per byte.</text>','</svg>']
(ROOT/'docs/assets/application-profile.svg').write_text('\n'.join(svg)+'\n')
