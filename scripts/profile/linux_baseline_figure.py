#!/usr/bin/env python3
"""Draw execution locations, not an additive breakdown of marginal medians."""
from pathlib import Path
import json
ROOT = Path(__file__).resolve().parents[2]
r = json.loads((ROOT / 'docs/measurements/linux-baseline-2026-10-19/summary.json').read_text())
client = r['clients'][1]['summary']
server = r['server']['windows'][1]['request_summary']
local = client['local']['metrics']['elapsed_ms']['median']
checked = client['checked-local']['metrics']['elapsed_ms']['median']
fetch = client['remote-fetch']['metrics']['elapsed_ms']['median']
svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="860" height="375" viewBox="0 0 860 375" role="img" aria-labelledby="title desc">
<title id="title">GitHub runs the client and tests; the droplet serves saved results</title>
<desc id="desc">On a hit, the GitHub runner fingerprints inputs, asks the droplet over HTTPS, and replays output on the runner. On a miss it executes Go tests on that same runner. The droplet authenticates and reads PostgreSQL and files; it does not run the tests. Numbers describe the second session and are not additive.</desc>
<defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="#426a85"/></marker></defs>
<rect width="860" height="375" rx="12" fill="#f8fafc"/>
<g font-family="Arial, sans-serif" fill="#172d43">
<text x="22" y="30" font-size="21" font-weight="bold">Two machines, different work</text>
<rect x="22" y="52" width="310" height="230" rx="8" fill="#e3eef8" stroke="#7398b5"/>
<rect x="528" y="52" width="310" height="230" rx="8" fill="#def1ec" stroke="#66a498"/>
<text x="40" y="81" font-size="18" font-weight="bold">GitHub Ubuntu runner</text>
<text x="40" y="111" font-size="15">Receipt client:</text>
<text x="40" y="137" font-size="15">1. Git + input hashes + Go probe</text>
<text x="40" y="163" font-size="15">2. Build key; find a saved result</text>
<text x="40" y="189" font-size="15">3. On hit: replay saved output</text>
<path d="M40 209H312" stroke="#7398b5"/>
<text x="40" y="234" font-size="15" font-weight="bold">On miss: execute go test here</text>
<text x="40" y="260" font-size="14">Fingerprint median: 46 ms</text>
<text x="546" y="81" font-size="18" font-weight="bold">Linux droplet</text>
<text x="546" y="111" font-size="15">Caddy → Receipt server</text>
<text x="546" y="141" font-size="15">Authenticate each request</text>
<text x="546" y="171" font-size="15">PostgreSQL records + output files</text>
<text x="546" y="207" font-size="14">Status handler: {server['status']['successful_median_ms']:.2f} ms median</text>
<text x="546" y="233" font-size="14">Fetch handler: {server['fetch']['successful_median_ms']:.2f} ms median</text>
<text x="546" y="260" font-size="14">No client test execution</text>
<path d="M334 127H524" stroke="#426a85" stroke-width="2" marker-end="url(#arrow)"/>
<text x="430" y="112" text-anchor="middle" font-size="14">HTTPS request</text>
<path d="M524 189H336" stroke="#426a85" stroke-width="2" marker-end="url(#arrow)"/>
<text x="430" y="214" text-anchor="middle" font-size="14">Status / saved result</text>
<text x="22" y="312" font-size="15">Whole-hit medians: local {local:.0f} ms · checked local {checked:.0f} ms · remote fetch {fetch:.0f} ms</text>
<text x="22" y="340" font-size="13">Second session. Component and whole-hit medians come from different observations; do not add them.</text>
<text x="22" y="360" font-size="13">The deployed server is an older modified build; this is a baseline, not an optimization comparison.</text>
</g></svg>'''
(ROOT / 'docs/assets/linux-client-server-baseline.svg').write_text(svg)
