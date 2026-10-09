"""Render the new Cargo review from Markdown; preserve older hand-written reviews.

Called by render_review_figures before its SVG expansion. This limited grammar
rejects unfamiliar blocks rather than silently rendering broken teaching prose.
"""
import html
import re

from render_working_doc import inline


def cargo_review(markdown):
    section = markdown.split('## Phase 7 review:', 1)[1].split('## Documentation revamp verification:', 1)[0]
    section = '## Phase 7 review:' + section
    section = re.sub(r'<!-- visual-summary:start -->.*?<!-- visual-summary:end -->\n*', '', section, flags=re.S)
    lines = section.strip().splitlines()
    output = []
    i = 0
    figure = None
    while i < len(lines):
        line = lines[i]
        if not line.strip():
            i += 1
            continue
        if line.startswith('<!-- review-figure:'):
            name, edge = line.removeprefix('<!-- review-figure:').removesuffix(' -->').rsplit(':', 1)
            if edge == 'start' and figure is None:
                figure = name
                output.extend([line, f'<figure class="review-figure" id="{name}" aria-labelledby="{name}-caption">'])
            elif edge == 'end' and figure == name:
                output.extend(['</figure>', line])
                figure = None
            else:
                raise ValueError('Mismatched Cargo figure')
        elif line.startswith('#### ') and figure:
            output.append(f'<figcaption id="{figure}-caption">{inline(line[5:])}</figcaption>')
        elif line.startswith('### '):
            output.append('<h3>' + inline(line[4:]) + '</h3>')
        elif line.startswith('## '):
            output.append('<h2 id="phase-7-review-bounded-cargo-support">' + inline(line[3:]) + '</h2>')
        elif line == '```go':
            code = []
            i += 1
            while i < len(lines) and lines[i] != '```':
                code.append(lines[i])
                i += 1
            if i == len(lines):
                raise ValueError('Unclosed Cargo code excerpt')
            output.append('<pre><code class="language-go">' + html.escape('\n'.join(code)) + '</code></pre>')
        else:
            if line.startswith(('#', '```', '<!--', '- ', '![')):
                raise ValueError('Unsupported Cargo review block: ' + line)
            paragraph = [line]
            while i + 1 < len(lines) and lines[i + 1].strip() and not lines[i + 1].startswith(('#', '```', '<!--')):
                i += 1
                paragraph.append(lines[i])
            output.append('<p>' + inline(' '.join(paragraph)) + '</p>')
        i += 1
    if figure:
        raise ValueError('Unclosed Cargo figure')
    return '\n'.join(output) + '\n\n'


def update_review(document, markdown):
    start = '<h2 id="phase-7-review-bounded-cargo-support">'
    end = '<h2 id="documentation-revamp-verification-9-october-2026">'
    if start in document:
        begin = document.index(start)
        finish = document.index(end, begin)
        document = document[:begin] + document[finish:]
    document = document.replace(end, cargo_review(markdown) + end, 1)
    phase_item = next(line[2:] for line in markdown.splitlines() if line.startswith('- Phase 7 adds'))
    document = document.replace('durable caller history.</li></ul>',
                                'durable caller history.</li><li>' + inline(phase_item) + '</li></ul>')
    for number, anchor, title in [(6, 'find-where-the-time-went', 'Find where the time went'),
                                  (7, 'bounded-cargo-support', 'Bounded Cargo support')]:
        href = f'#phase-{number}-review-{anchor}'
        if f'href="{href}"' not in document:
            item = f'<li><a href="{href}">Phase {number}: {title}</a></li>'
            document = document.replace('</ol></details></nav>', item + '</ol></details></nav>', 1)
    return document
