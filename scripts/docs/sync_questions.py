"""Copy question-bank entries into each chapter's "Check your understanding" block.

A question in docs/questions/*.md opts in with a marker under its heading:

    ### What is a manifest?
    <!-- check: guide/01-remember-a-successful-run.md -->

The chapter's block between `<!-- questions:start -->` and
`<!-- questions:end -->` is regenerated from those entries, in bank order
(newcomer, practitioner, expert). The bank is the only copy to edit. The
question files and the chapters both sit one directory below docs/, so
relative links work unchanged in both places.

    python3 scripts/docs/sync_questions.py          # rewrite chapter blocks
    python3 scripts/docs/sync_questions.py --check  # fail if any are stale
"""
from pathlib import Path
import re
import sys

DOCS = Path(__file__).resolve().parents[2] / 'docs'
LEVELS = ('newcomer', 'practitioner', 'expert')
MARKER = re.compile(r'^<!-- check: (guide/[\w.-]+\.md) -->\n', re.M)
BLOCK = re.compile(r'(<!-- questions:start -->\n)(.*?)(<!-- questions:end -->)', re.S)


def questions(level):
    """Yield (heading, body) for each question in one bank file."""
    text = (DOCS / 'questions' / (level + '.md')).read_text()
    for match in re.finditer(r'(?ms)^### .*?^</details>\n', text):
        yield match[0]


def selected():
    """Map chapter path -> list of question blocks, markers removed."""
    chosen = {}
    for level in LEVELS:
        for block in questions(level):
            for chapter in MARKER.findall(block):
                chosen.setdefault(chapter, []).append(MARKER.sub('', block))
    return chosen


def outputs():
    chosen = selected()
    files = {}
    for path in sorted((DOCS / 'guide').glob('[0-9][0-9]-*.md')):
        text = path.read_text()
        if not BLOCK.search(text):
            raise ValueError(path.name + ' has no questions block')
        blocks = chosen.pop('guide/' + path.name, [])
        body = '\n' + '\n'.join(blocks) + '\n' if blocks else ''
        files[path] = BLOCK.sub(lambda m: m[1] + body + m[3], text)
    if chosen:
        raise ValueError('Questions name missing chapters: ' + ', '.join(chosen))
    return files


def stale():
    return [p.name for p, text in outputs().items() if p.read_text() != text]


def main():
    if '--check' in sys.argv:
        names = stale()
        if names:
            raise SystemExit('Stale question blocks: ' + ', '.join(names)
                             + '; run python3 scripts/docs/sync_questions.py')
        print('Chapter questions match the bank.')
        return
    for path, text in outputs().items():
        path.write_text(text)
    print('Synchronised chapter questions.')


if __name__ == '__main__':
    main()
