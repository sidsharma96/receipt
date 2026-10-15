#!/usr/bin/env python3
"""Render Receipt's deliberately small Markdown format without dependencies.

The working guide is the prose source. Unknown fenced block kinds and malformed
figures fail rather than silently generating misleading content.
"""

import argparse
import difflib
import html
from pathlib import Path
import re


ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "WORKING-DOC.md"
OUTPUT = ROOT / "WORKING-DOC.html"


def slug(text):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


def inline(text):
    parts = []
    position = 0
    pattern = r"`([^`]+)`|\[([^\]]+)\]\(([^)]+)\)"
    for match in re.finditer(pattern, text):
        parts.append(html.escape(text[position:match.start()]))
        if match.group(1) is not None:
            parts.append("<code>" + html.escape(match.group(1)) + "</code>")
        else:
            label, target = match.group(2), match.group(3)
            if target.startswith(("javascript:", "data:")):
                raise ValueError("Unsupported link target")
            # A code link names its symbol in backticks; show it as code.
            label_html = re.sub(r"`([^`]+)`", r"<code>\1</code>", html.escape(label))
            parts.append('<a href="' + html.escape(target, quote=True) + '">'
                         + label_html + "</a>")
        position = match.end()
    parts.append(html.escape(text[position:]))
    return "".join(parts)


def figure(kind, lines):
    if len(lines) < 3:
        raise ValueError("A figure needs a caption and at least two steps")
    caption = inline(lines[0])
    body = []
    for line in lines[1:]:
        cells = [inline(cell.strip()) for cell in line.split("|")]
        expected = 3 if kind == "timeline" else 2
        if len(cells) != expected:
            raise ValueError("Malformed figure row: " + line)
        if kind == "timeline":
            body.append('<li><span class="time">Step ' + cells[0]
                        + '</span><div><strong>Reader</strong><p>' + cells[1]
                        + '</p></div><div><strong>Cleanup</strong><p>' + cells[2]
                        + '</p></div></li>')
        else:
            body.append('<li><strong>' + cells[0] + '</strong><p>'
                        + cells[1] + '</p></li>')
    return ('<figure class="' + kind + '"><figcaption>' + caption
            + '</figcaption><ol>' + "".join(body) + '</ol></figure>')


def render(source):
    lines = source.splitlines()
    if not lines or not lines[0].startswith("# "):
        raise ValueError("Expected one document title")
    title = lines[0][2:]
    chapters, intro = [], []
    current = intro
    chapter_id = ""
    answer_open = False
    i = 1
    while i < len(lines):
        line = lines[i]
        if not line.strip():
            i += 1
            continue
        if line.startswith("## "):
            if answer_open:
                current.append("</details>")
                answer_open = False
            heading = line[3:]
            chapter_id = slug(heading)
            current = ['<h2>' + inline(heading) + '</h2>']
            chapters.append((chapter_id, heading, current))
            i += 1
        elif line.startswith("### "):
            if answer_open:
                current.append("</details>")
                answer_open = False
            heading = line[4:]
            identifier = chapter_id + "--" + slug(heading)
            if heading.startswith("Answer:"):
                current.append('<details class="answer" id="' + identifier
                               + '"><summary>' + inline(heading) + '</summary>')
                answer_open = True
            else:
                current.append('<h3 id="' + identifier + '">'
                               + inline(heading) + '</h3>')
            i += 1
        elif line.startswith("!["):
            match = re.fullmatch(r"!\[([^\]]+)\]\((assets/[a-zA-Z0-9_./-]+\.svg)\)", line)
            if not match or '..' in match[2].split('/'):
                raise ValueError("Unsupported image: " + line)
            current.append('<figure><img src="' + html.escape(match[2], quote=True)
                           + '" alt="' + html.escape(match[1], quote=True)
                           + '" style="display:block;width:100%;max-width:480px;height:auto;margin:auto">'
                           + '<figcaption>' + inline(match[1]) + '</figcaption></figure>')
            i += 1
        elif line.startswith("```"):
            kind = line[3:]
            if kind not in {"go", "sh", "text", "diagram", "timeline"}:
                raise ValueError("Unknown block kind: " + kind)
            block = []
            i += 1
            while i < len(lines) and lines[i] != "```":
                block.append(lines[i])
                i += 1
            if i == len(lines):
                raise ValueError("Unclosed fenced block")
            if kind in {"diagram", "timeline"}:
                current.append(figure(kind, block))
            else:
                current.append('<pre><code class="language-' + kind + '">'
                               + html.escape("\n".join(block)) + '</code></pre>')
            i += 1
        elif line.startswith("- "):
            items = []
            while i < len(lines) and lines[i].startswith("- "):
                items.append("<li>" + inline(lines[i][2:]) + "</li>")
                i += 1
            current.append("<ul>" + "".join(items) + "</ul>")
        else:
            paragraph = []
            while i < len(lines) and lines[i].strip() and not lines[i].startswith(("#", "```", "- ", "![")):
                paragraph.append(lines[i])
                i += 1
            if not paragraph:
                raise ValueError("Unsupported Markdown at line " + str(i + 1))
            current.append("<p>" + inline(" ".join(paragraph)) + "</p>")
    if answer_open:
        current.append("</details>")
    ids = [chapter[0] for chapter in chapters]
    if len(set(ids)) != len(ids):
        raise ValueError("Duplicate chapter IDs")
    navigation = "".join('<a href="#' + identifier + '">' + inline(heading)
                         + '</a>' for identifier, heading, _ in chapters)
    pages = []
    for index, (identifier, heading, body) in enumerate(chapters):
        previous = chapters[index - 1] if index else None
        following = chapters[index + 1] if index + 1 < len(chapters) else None
        adjacent = []
        for neighbor, label in [(previous, "Previous"), (following, "Next")]:
            if neighbor:
                adjacent.append('<a href="#' + neighbor[0] + '">' + label
                                + ': ' + inline(neighbor[1]) + '</a>')
        pages.append('<article id="' + identifier + '" data-chapter><div class="chapter-number" aria-hidden="true">'
                     + f'{index + 1:02d}' + '</div>' + "\n".join(body)
                     + '<div class="adjacent">' + "".join(adjacent) + '</div></article>')
    return ('<!doctype html>\n<!-- Generated from WORKING-DOC.md by render_working_doc.py. -->\n<html lang="en">\n<head>\n<meta charset="utf-8">\n'
            + '<meta name="viewport" content="width=device-width, initial-scale=1">\n'
            + '<title>' + html.escape(title) + '</title>\n<style>\n' + CSS
            + '\n</style>\n</head>\n<body>\n<a class="skip" href="#reading">Skip to reading</a>\n'
            + '<header><a class="brand" href="#start-here">receipt / working notes</a>'
            + '<a href="PHASE-REVIEWS.html">Reviews and evidence</a>'
            + '<a href="WORKING-DOC.md">Phone text</a></header>\n'
            + '<div class="layout"><aside><details open id="chapters"><summary>Chapters</summary>'
            + '<nav aria-label="Chapters">' + navigation + '</nav></details>'
            + '<p class="sidebar-note">Follow a run. Read the code. Explain the choice.</p></aside>'
            + '<main id="reading" tabindex="-1"><h1>' + inline(title) + '</h1>'
            + "\n".join(intro) + "\n" + "\n".join(pages) + '</main></div>\n'
            + '<footer>Updated 15 October 2026. Generated from WORKING-DOC.md. '
            + 'No network connection required.</footer>\n<script>\n' + JS
            + '\n</script>\n</body>\n</html>\n')


CSS = """
:root{color-scheme:light;--paper:#fcfcf9;--ink:#24332e;--muted:#54635d;--line:#d5ded7;--accent:#166246;--wash:#eef4ef}
*{box-sizing:border-box}
body{margin:0;background:var(--paper);color:var(--ink);font:17px/1.75 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;overflow-wrap:anywhere}
a{color:var(--accent);text-underline-offset:3px}
a:focus-visible,summary:focus-visible{outline:3px solid #388462;outline-offset:4px}
header{display:flex;flex-wrap:wrap;gap:16px 30px;padding:20px 28px;border-bottom:1px solid var(--line);font-size:14px}
.brand{font-weight:700;margin-right:auto;text-decoration:none;letter-spacing:.025em}
.layout{display:grid;grid-template-columns:235px minmax(0,820px);gap:50px;max-width:1180px;margin:auto;padding:38px 28px 60px}
aside{position:sticky;top:25px;align-self:start;max-height:calc(100vh - 50px);overflow:auto;font-size:14px}
summary{cursor:pointer;font-weight:650}
nav{display:grid;gap:8px;margin-top:15px}
nav a{padding:8px 10px;text-decoration:none;border-left:2px solid transparent;line-height:1.5}
nav a[aria-current=page]{background:var(--wash);border-color:var(--accent);font-weight:650}
.sidebar-note{font-size:13px;color:var(--muted);border-top:1px solid var(--line);padding-top:18px}
main{min-width:0}
h1{font-size:clamp(27px,4vw,39px);line-height:1.2;letter-spacing:-.035em;margin:0 0 20px}
h2{font-size:30px;line-height:1.3;letter-spacing:-.025em;margin:8px 0 22px}
h3{font-size:21px;line-height:1.4;letter-spacing:-.015em;margin:34px 0 12px}
p{margin:16px 0}
article{border-top:1px solid var(--line);margin-top:35px;padding-top:25px;scroll-margin-top:24px}
.chapter-number{font:13px ui-monospace,SFMono-Regular,monospace;color:var(--muted)}
code{font: .85em ui-monospace,SFMono-Regular,Menlo,monospace}
pre{max-width:100%;overflow:auto;padding:20px;background:#f0f3f0;border:1px solid var(--line);border-radius:6px;tab-size:4;line-height:1.6}
pre code{font-size:13px;white-space:pre}
ul{padding-left:22px}li{margin:12px 0}
figure{margin:26px 0;background:var(--wash);border:1px solid var(--line);border-radius:6px;padding:20px}
figcaption{font-size:14px;font-weight:650;margin-bottom:18px}
figure ol{list-style:none;padding:0;margin:0;counter-reset:step}
.diagram li{position:relative;margin:0 0 24px;padding:16px 18px 16px 50px;background:var(--paper);border:1px solid var(--line);border-radius:4px;counter-increment:step}
.diagram li:before{content:counter(step);position:absolute;left:16px;top:16px;font:14px/1.7 ui-monospace,monospace;color:var(--accent)}
.diagram li:not(:last-child):after{content:"↓";position:absolute;bottom:-27px;left:23px;color:var(--accent);font-size:20px}
.diagram li:last-child{margin-bottom:0}
figure strong{font-size:15px}
figure p{font-size:15px;line-height:1.55;margin:5px 0 0}
.timeline li{display:grid;grid-template-columns:55px 1fr 1fr;gap:14px;margin:0;padding:17px 0;border-top:1px solid var(--line)}
.time{font:12px/1.6 ui-monospace,monospace;color:var(--muted)}
.answer{margin:22px 0;padding:16px 20px;border:1px solid var(--line);border-radius:5px}
.answer summary{font-size:16px;color:var(--accent)}
.adjacent{display:flex;flex-wrap:wrap;justify-content:space-between;gap:20px;border-top:1px solid var(--line);margin-top:38px;padding-top:20px;font-size:14px}
footer{max-width:1180px;margin:auto;padding:10px 28px 35px;font-size:13px;color:var(--muted)}
.skip{position:absolute;left:10px;top:-100px;padding:10px;background:var(--paper);z-index:2}.skip:focus{top:8px}
[hidden]{display:none!important}
@media(max-width:850px){.layout{display:block;padding:24px 20px 40px}aside{position:static;max-height:none;margin-bottom:30px;border-bottom:1px solid var(--line);padding-bottom:18px}.sidebar-note{display:none}nav{grid-template-columns:1fr 1fr}h2{font-size:27px}}
@media(max-width:480px){body{font-size:16px;line-height:1.7}header{padding:15px 18px;gap:8px 18px}.brand{flex-basis:100%}.layout{padding:20px 16px 30px}nav{grid-template-columns:1fr}pre{padding:14px}pre code{font-size:12px}figure{padding:14px}.timeline li{grid-template-columns:1fr;gap:10px}.time{font-weight:650}.timeline li div{padding-left:12px;border-left:2px solid var(--line)}h3{font-size:20px}}
@media print{aside,header,.adjacent,footer{display:none}.layout{display:block;padding:0}body{font-size:11pt}article[hidden]{display:block!important}.answer>*{display:block}pre{white-space:pre-wrap}pre code{white-space:pre-wrap}figure,pre{break-inside:avoid}}
"""

JS = """
'use strict';
const pages = [...document.querySelectorAll('[data-chapter]')];
const links = [...document.querySelectorAll('nav a')];
const menu = document.querySelector('#chapters');
const legacy = {
 'phase-1-review-remembering-a-local-run': '1-remember-a-successful-run',
 'phase-2-review-sharing-results-between-clients': '2-let-another-client-reuse-the-result',
 'phase-3-review-limits-and-cleanup-without-deleting-needed-files': '3-delete-old-data-without-breaking-a-reader',
 'phase-4-review-checking-incoming-data-and-interrupted-writes': '4-check-data-before-accepting-it',
 'phase-5-review-separate-caller-credentials-and-operation-permissions': '5-give-callers-different-permissions'
};
function route(scroll) {
 const requested = decodeURIComponent(location.hash.slice(1));
 const id = legacy[requested] || requested;
 const target = document.getElementById(id);
 const page = target?.closest('[data-chapter]') || pages[0];
 pages.forEach(p => { p.hidden = p !== page; });
 links.forEach(a => {
  if (a.hash === '#' + page.id) a.setAttribute('aria-current', 'page');
  else a.removeAttribute('aria-current');
 });
 if (target?.classList.contains('answer')) target.open = true;
 if (scroll) {
  if (target && target !== page) target.scrollIntoView();
  else document.querySelector('#reading').scrollIntoView();
 }
}
if (matchMedia('(max-width:850px)').matches) menu.open = false;
window.addEventListener('hashchange', () => route(true));
route(Boolean(location.hash));
document.querySelector('.skip').addEventListener('click', event => {
 event.preventDefault();
 const reading = document.querySelector('#reading');
 reading.focus(); reading.scrollIntoView();
});
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Fail if HTML needs regeneration")
    args = parser.parse_args()
    generated = render(SOURCE.read_text())
    if args.check:
        existing = OUTPUT.read_text() if OUTPUT.exists() else ""
        if existing != generated:
            print("".join(difflib.unified_diff(existing.splitlines(True), generated.splitlines(True),
                                             fromfile=str(OUTPUT), tofile="generated")))
            raise SystemExit("Working HTML is stale; run python3 docs/render_working_doc.py")
        print("Working HTML matches its Markdown source.")
    else:
        OUTPUT.write_text(generated)
        print("Generated " + str(OUTPUT))


if __name__ == "__main__":
    main()
