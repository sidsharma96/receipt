#!/usr/bin/env python3
"""Draw the measured process-boundary timeline; no inferred thread states."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
data = json.loads((ROOT / '.tools/measurements/receipt-go-batch-2026-10-19/evidence.json').read_text())
row = max(data['rows'], key=lambda r: r['go']['wall_ms'])
go = row['go']
system = data['system_samples'][89]
origin = row['start_ns']
left, scale = 155, 265 / 1e9

def x(ns):
    return left + (ns-origin)*scale

svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="860" height="365" viewBox="0 0 860 365" role="img" aria-labelledby="title desc">
<title id="title">Receipt call 688: Go and the system sampler slow together</title>
<desc id="desc">A 2.28 second Receipt hit contains a 1.675 second Go probe. The separate system sampler takes 1.678 seconds over almost the same interval. These clocks do not identify OS thread states or prove a shared cause. Go reports 75.588 milliseconds CPU; Receipt reports 1.023 milliseconds GC pauses.</desc>
<rect width="860" height="365" rx="12" fill="#f8fafc"/>
<g font-family="Arial, sans-serif" fill="#172d43">
<text x="24" y="31" font-size="21" font-weight="bold">Two observed operations slowed together</text>
<text x="24" y="56" font-size="14">Call 688 · same horizontal clock · no native thread-state trace</text>
'''
for seconds in (0, .5, 1, 1.5, 2):
    px = left + seconds*265
    svg += f'<path d="M{px} 82V252" stroke="#d5dce5"/><text x="{px}" y="77" text-anchor="middle" font-size="12">{seconds:g}s</text>\n'
for label, y in [('Receipt hit', 113), ('Go probe', 171), ('System sampler', 228)]:
    svg += f'<text x="24" y="{y+5}" font-size="15">{label}</text>\n'
svg += f'''<rect x="{left}" y="100" width="{x(row['end_ns'])-left}" height="24" rx="4" fill="#98aec0"/>
<text x="{x(row['end_ns'])+8}" y="118" font-size="13">2,280 ms*</text>
<rect x="{x(go['start_ns'])}" y="157" width="{x(go['wait_return_ns'])-x(go['start_ns'])}" height="25" rx="4" fill="#b45309"/>
<rect x="{x(go['start_ns'])}" y="157" width="{x(go['start_return_ns'])-x(go['start_ns'])}" height="25" fill="#eab308"/>
<text x="{x(go['start_ns'])+24}" y="174" font-size="13" fill="white">Start 60 ms → Wait returns after another 1,615 ms</text>
<text x="{x(go['start_ns'])}" y="200" font-size="13">Reported CPU: 76 ms (its location on this clock is unknown)</text>
<rect x="{x(system['start_ns'])}" y="216" width="{x(system['end_ns'])-x(system['start_ns'])}" height="24" rx="4" fill="#156d77"/>
<text x="{x(system['start_ns'])+14}" y="233" font-size="13" fill="white">ps + vm_stat collection: 1,678 ms</text>
<text x="24" y="279" font-size="14">Receipt GC pauses total 1.02 ms; Go-child GC was not recorded in this batch.</text>
<text x="24" y="303" font-size="14">The overlapping delays suggest shared contention. They do not identify the waiting resource.</text>
<text x="24" y="336" font-size="12">*Unix timestamp span rounded; monotonic external duration is 2,279.595 ms. Other spans use parent clocks.</text>
</g></svg>
'''
(ROOT / 'docs/assets/receipt-go-batch-timeline.svg').write_text(svg)
