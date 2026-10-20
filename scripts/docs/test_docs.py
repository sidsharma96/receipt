"""Checks for the documentation book.

These check links, code anchors, attributed excerpts, question format and the
built site. They are not a claim about how well a reader learns from the text.

Run from the repository root:

    python3 -m unittest discover -s scripts/docs -p 'test_*.py'

The built-site checks run only after `make docs` (or `mdbook build docs`).
"""

from html.parser import HTMLParser
from pathlib import Path
import re
import sys
import unittest
import xml.etree.ElementTree as ET

sys.path.insert(0, str(Path(__file__).resolve().parent))
import review_figures  # noqa: E402
import sync_questions  # noqa: E402

REPOSITORY = Path(__file__).resolve().parents[2]
DOCS = REPOSITORY / "docs"
BOOK = DOCS / "book"
GITHUB = "https://github.com/sidsharma96/receipt/"
GITHUB_BLOB = GITHUB + "blob/main/"
# A code link: [`symbol`](https://github.com/sidsharma96/receipt/blob/main/path#L10-L20)
REPO_LINK = re.compile(r"\[((?:[^\[\]]|\[[^\]]*\])+)\]\(" + re.escape(GITHUB)
                       + r"(blob|tree)/main/([^)#\s]+)(?:#L(\d+)(?:-L(\d+))?)?\)")
# The named symbol may sit this many lines outside the anchored range.
LINK_SLACK = 3
LEVELS = ("Newcomer", "Practitioner", "Expert")
# File types the published site may contain. Anything else under docs/ is a
# script, configuration or data that does not belong in the book.
SOURCE_TYPES = {".md", ".svg", ".json"}
# Symbol-only CPU/allocation profiles are intentional downloadable evidence.
PROFILE_TYPES = {".pprof"}
BOOK_TYPES = {".html", ".css", ".js", ".svg", ".png", ".woff2", ".json", ".txt", ""}


def documents():
    """Every Markdown file in the book source, plus the README."""
    found = [REPOSITORY / "README.md"]
    for path in sorted(DOCS.rglob("*.md")):
        if BOOK not in path.parents:
            found.append(path)
    return found


def without_code(text):
    """Drop fenced code so example links and headings inside it are ignored."""
    return re.sub(r"(?ms)^(```|~~~).*?^\1", "", text)


def heading_ids(path):
    """Anchors GitHub and mdBook both generate, plus explicit <a id> anchors."""
    text = without_code(path.read_text())
    found, counts = set(), {}
    for heading in re.findall(r"(?m)^#{1,6} (.+)$", text):
        heading = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", heading).replace("`", "")
        slug = re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-")
        count = counts.get(slug, 0)
        counts[slug] = count + 1
        found.add(slug if count == 0 else slug + "-" + str(count))
    found.update(re.findall(r'<a id="([^"]+)"', text))
    return found


class SourceChecks(unittest.TestCase):
    def test_relative_links_and_anchors_resolve(self):
        """Relative links must reach a file, and a #fragment must name a heading."""
        checked = 0
        for document in documents():
            text = without_code(document.read_text())
            for target in re.findall(r"\]\(([^)\s]+)\)", text):
                if re.match(r"[a-z]+:", target):
                    continue
                checked += 1
                filename, _, fragment = target.partition("#")
                path = (document.parent / filename).resolve() if filename else document
                with self.subTest(document=str(document.relative_to(REPOSITORY)), link=target):
                    self.assertTrue(path.exists(), "missing file")
                    if document.parent != REPOSITORY and filename:
                        self.assertIn(DOCS, path.parents, "book links must stay inside docs/")
                        # mdBook renders README.md as index.html but leaves links to it unchanged.
                        self.assertNotEqual(path.name, "README.md", "name the page something else")
                    if fragment:
                        self.assertTrue(path.suffix == ".md", "fragment on a non-Markdown file")
                        self.assertIn(fragment, heading_ids(path), "no heading or anchor named " + fragment)
        self.assertGreater(checked, 200)

    def test_github_code_links_name_symbols_in_anchored_lines(self):
        """Line anchors drift when code moves; fail instead of misleading readers."""
        checked = 0
        for document in documents():
            for label, kind, path, start, end in REPO_LINK.findall(document.read_text()):
                where = document.name + ": " + path + ("#L" + start if start else "")
                target = REPOSITORY / path
                with self.subTest(link=where, label=label):
                    self.assertTrue(target.exists(), where)
                    symbols = re.findall(r"`([^`]+)`", label)
                    if not start:
                        # A file link names a file, not a symbol in it.
                        self.assertFalse(symbols and kind == "blob" and target.suffix == ".go",
                                         where + ": a link naming a Go symbol needs a #L anchor")
                        continue
                    checked += 1
                    lines = target.read_text().splitlines()
                    first, last = int(start), int(end or start)
                    self.assertTrue(1 <= first <= last <= len(lines), where)
                    self.assertTrue(symbols, where + ": link text must name a symbol in backticks")
                    window = "\n".join(lines[max(0, first - 1 - LINK_SLACK):last + LINK_SLACK])
                    for symbol in symbols:
                        for part in re.split(r"[.\s]+", symbol.removesuffix("()")):
                            if part:
                                self.assertIn(part, window, where + ": " + symbol)
        self.assertGreaterEqual(checked, 150)

    def test_guide_go_excerpts_match_attributed_files(self):
        # Each excerpt is introduced by a GitHub link whose line anchor covers it.
        pattern = (r"\]\(" + re.escape(GITHUB_BLOB) + r"([^)#]+\.go)#L(\d+)(?:-L(\d+))?\):"
                   r"\n\n```go\n(.*?)\n```")
        excerpts = []
        for chapter in sorted((DOCS / "guide").glob("*.md")):
            excerpts += re.findall(pattern, chapter.read_text(), re.DOTALL)
        self.assertEqual(len(excerpts), 26)
        for path, start, end, excerpt in excerpts:
            lines = (REPOSITORY / path).read_text().splitlines()
            covered = "\n".join(lines[int(start) - 1:int(end or start)])
            self.assertIn(re.sub(r"\s+", " ", excerpt).strip(),
                          re.sub(r"\s+", " ", covered), path + "#L" + start)

    def test_historical_go_excerpts_match_source(self):
        """Each review excerpt must still appear in the Go file linked before it."""
        source = (DOCS / "PHASE-REVIEWS.md").read_text()
        events = re.finditer(r"\]\(" + re.escape(GITHUB_BLOB) + r"([^)#\s]+\.go)[^)]*\)"
                             r"|(?ms:^(```|~~~)go\n(.*?)^\2)", source)
        current, excerpts = None, 0
        for event in events:
            if event[1]:
                current = event[1]
                continue
            excerpts += 1
            with self.subTest(excerpt=event[3][:40]):
                self.assertIsNotNone(current)
                actual = (REPOSITORY / current).read_text()
                self.assertIn(re.sub(r"\s+", " ", event[3]).strip(),
                              re.sub(r"\s+", " ", actual), current)
        self.assertEqual(excerpts, 25)

    def test_all_seven_reviews_keep_nine_questions(self):
        source = (DOCS / "PHASE-REVIEWS.md").read_text()
        phases = re.split(r"(?m)^## Phase \d review:", source)[1:]
        self.assertEqual(len(phases), 7)
        for phase in phases:
            questions = re.findall(r"(?m)^### ([1-9])\. ", phase)
            self.assertEqual(questions[:9], list("123456789"))

    def test_deployment_review_and_assets(self):
        source = (DOCS / "PHASE-REVIEWS.md").read_text()
        deployment = source.split("## Deployment review:", 1)[1]
        self.assertEqual(re.findall(r"(?m)^### ([1-9])\. ", deployment), list("123456789"))
        images = re.findall(r"!\[[^\]]+\]\((assets/[^)]+\.svg)\)", deployment)
        self.assertEqual(len(images), 3)
        for image in images:
            ET.parse(DOCS / image)
        config = (REPOSITORY / "deploy/digitalocean/Caddyfile").read_text().strip()
        self.assertIn(config, deployment)

    def test_review_figures_are_current(self):
        self.assertEqual(review_figures.stale(), [])

    def test_every_svg_parses(self):
        for image in sorted((DOCS / "assets").rglob("*.svg")):
            with self.subTest(image=image.name):
                ET.parse(image)

    def test_summary_lists_every_page(self):
        summary = (DOCS / "SUMMARY.md").read_text()
        listed = re.findall(r"\]\(([^)]+\.md)\)", summary)
        self.assertEqual(len(listed), len(set(listed)))
        for page in listed:
            self.assertTrue((DOCS / page).is_file(), page)
        pages = {str(p.relative_to(DOCS)) for p in documents()[1:]} - {"SUMMARY.md"}
        self.assertEqual(pages - set(listed), set(), "pages missing from SUMMARY.md")

    def test_book_source_holds_only_documentation(self):
        """mdBook copies every non-Markdown file under docs/ into the site."""
        for path in sorted(DOCS.rglob("*")):
            if path.is_dir() or BOOK in path.parents or path == BOOK:
                continue
            with self.subTest(path=str(path.relative_to(DOCS))):
                if path.name == "book.toml":
                    continue
                allowed = SOURCE_TYPES | ({".png"} if DOCS / "assets" in path.parents else set())
                if DOCS / "measurements" in path.parents:
                    allowed |= PROFILE_TYPES
                self.assertIn(path.suffix, allowed)
                self.assertFalse(path.name.startswith("."))


class QuestionChecks(unittest.TestCase):
    def bank(self, level):
        text = (DOCS / "questions" / (level.lower() + ".md")).read_text()
        return list(sync_questions.questions(level.lower())), text

    def test_questions_follow_the_format(self):
        for level in LEVELS:
            blocks, text = self.bank(level)
            self.assertEqual(text.count("\n### "), len(blocks), level + ": a question is malformed")
            self.assertGreaterEqual(len(blocks), 15, level)
            for block in blocks:
                title = block.splitlines()[0]
                with self.subTest(level=level, question=title):
                    self.assertIn("\n**Level:** " + level + "\n", block)
                    self.assertIn("\n<details>\n<summary>Answer</summary>\n\n", block)
                    self.assertTrue(block.endswith("\n</details>\n"))
                    self.assertEqual(block.count("<details>"), 1)
                    learn = re.search(r"\n\*\*Learn more:\*\* (.+)\n", block)
                    self.assertTrue(learn, "missing Learn more line")
                    self.assertRegex(learn[1], r"\]\((\.\./|#)[^)]+\)", "Learn more needs a book section")
                    self.assertNotIn("—", block)

    def test_chapter_questions_match_the_bank(self):
        self.assertEqual(sync_questions.stale(), [])

    def test_each_chapter_checks_three_to_five_questions(self):
        for chapter, blocks in sorted(sync_questions.selected().items()):
            with self.subTest(chapter=chapter):
                self.assertTrue(3 <= len(blocks) <= 5, len(blocks))
        chapters = {"guide/" + p.name for p in (DOCS / "guide").glob("[0-9][0-9]-*.md")}
        self.assertEqual(chapters - set(sync_questions.selected()), set())


class Page(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.ids, self.links = set(), []

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if "id" in attrs:
            self.ids.add(attrs["id"])
        for name in ("href", "src"):
            if attrs.get(name):
                self.links.append(attrs[name])


@unittest.skipUnless((BOOK / "index.html").is_file(), "build the book first: make docs")
class BuiltBookChecks(unittest.TestCase):
    def test_site_holds_only_web_files(self):
        for path in sorted(BOOK.rglob("*")):
            if path.is_file():
                with self.subTest(path=str(path.relative_to(BOOK))):
                    allowed = BOOK_TYPES | (PROFILE_TYPES if BOOK / "measurements" in path.parents else set())
                    self.assertIn(path.suffix, allowed)

    def test_links_and_anchors_resolve_in_the_site(self):
        pages = {}
        for path in BOOK.rglob("*.html"):
            page = Page()
            page.feed(path.read_text())
            pages[path] = page
        checked = 0
        for path, page in sorted(pages.items()):
            if path.name in {"print.html", "toc.html", "404.html"}:
                continue
            for link in page.links:
                if re.match(r"[a-z]+:|//", link):
                    continue
                checked += 1
                filename, _, fragment = link.partition("#")
                target = (path.parent / filename).resolve() if filename else path
                with self.subTest(page=str(path.relative_to(BOOK)), link=link):
                    self.assertTrue(target.exists(), "missing file")
                    if fragment and target.suffix == ".html":
                        self.assertIn(fragment, pages[target].ids, "missing anchor")
        self.assertGreater(checked, 1000)


if __name__ == "__main__":
    unittest.main()
