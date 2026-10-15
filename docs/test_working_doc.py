"""Checks for the generated reader, not a claim about learner comprehension."""

from html.parser import HTMLParser
from pathlib import Path
import re
import unittest

import render_working_doc as renderer
import render_review_figures as review_renderer


REPOSITORY = renderer.ROOT.parent
GITHUB_BLOB = "https://github.com/sidsharma96/receipt/blob/main/"
# A code link: [`symbol`](https://github.com/sidsharma96/receipt/blob/main/path#L10-L20)
CODE_LINK = re.compile(r"\[([^\]]+)\]\(" + re.escape(GITHUB_BLOB)
                       + r"([^)#\s]+)(?:#L(\d+)(?:-L(\d+))?)?\)")
# The named symbol may sit this many lines outside the anchored range.
LINK_SLACK = 3


class ReaderParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.ids = []
        self.links = []
        self.figures = 0
        self.answers = 0
        self.tags = []

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if "id" in attrs:
            self.ids.append(attrs["id"])
        if tag == "a":
            self.links.append(attrs.get("href", ""))
        if tag == "figure":
            self.figures += 1
        if tag == "details" and attrs.get("class") == "answer":
            self.answers += 1
        if tag not in {"meta", "link", "br", "hr", "img", "input"}:
            self.tags.append(tag)

    def handle_endtag(self, tag):
        if not self.tags or self.tags[-1] != tag:
            raise ValueError("Unbalanced HTML: " + tag)
        self.tags.pop()


class WorkingGuideTests(unittest.TestCase):
    def test_deployment_review_and_assets(self):
        source = (renderer.ROOT / 'PHASE-REVIEWS.md').read_text()
        deployment = source.split('## Deployment review:', 1)[1]
        self.assertEqual(re.findall(r'(?m)^### ([1-9])\. ', deployment), list('123456789'))
        images = re.findall(r'!\[[^\]]+\]\((assets/[^)]+\.svg)\)', deployment)
        self.assertEqual(len(images), 3)
        import xml.etree.ElementTree as ET
        for image in images:
            ET.parse(renderer.ROOT / image)
        config = (renderer.ROOT.parent / 'deploy/digitalocean/Caddyfile').read_text().strip()
        self.assertIn(config, deployment)
        for bad in ('![bad](https://external.example/a.svg)', '![bad](assets/../secret.svg)'):
            with self.assertRaises(ValueError):
                renderer.render('# Title\n\n## Chapter\n\n' + bad)

    def test_review_rendering_and_links(self):
        path = renderer.ROOT / "PHASE-REVIEWS.html"
        html = path.read_text()
        self.assertEqual(review_renderer.render(html)[0], html)
        parser = ReaderParser()
        parser.feed(html)
        self.assertFalse(parser.tags)
        self.assertEqual(len(parser.ids), len(set(parser.ids)))
        self.assertEqual(parser.figures, 27)
        for link in parser.links:
            if link.startswith(("http://", "https://")):
                continue
            filename, separator, fragment = link.partition("#")
            target = renderer.ROOT / filename if filename else path
            self.assertTrue(target.is_file(), link)
            if separator and target.suffix == ".html":
                parsed = ReaderParser()
                parsed.feed(target.read_text())
                self.assertIn(fragment, parsed.ids, link)

    def test_generated_file_matches_source(self):
        self.assertEqual(renderer.OUTPUT.read_text(), renderer.render(renderer.SOURCE.read_text()))

    def test_reader_structure_and_local_links(self):
        parser = ReaderParser()
        parser.feed(renderer.OUTPUT.read_text())
        self.assertFalse(parser.tags)
        self.assertEqual(len(parser.ids), len(set(parser.ids)))
        self.assertEqual(parser.figures, 14)
        self.assertEqual(parser.answers, 10)
        for link in parser.links:
            if link.startswith(("http://", "https://")):
                continue
            filename, separator, fragment = link.partition("#")
            target = renderer.ROOT / filename if filename else renderer.OUTPUT
            self.assertTrue(target.is_file(), link)
            if separator and target.suffix == ".html":
                target_parser = ReaderParser()
                target_parser.feed(target.read_text())
                self.assertIn(fragment, target_parser.ids, link)

    def test_actual_go_excerpts_match_attributed_files(self):
        # Each excerpt is introduced by a GitHub link whose line anchor covers it.
        source = renderer.SOURCE.read_text()
        pattern = (r"\]\(" + re.escape(GITHUB_BLOB) + r"([^)#]+\.go)#L(\d+)(?:-L(\d+))?\):"
                   r"\n\n```go\n(.*?)\n```")
        excerpts = re.findall(pattern, source, re.DOTALL)
        self.assertEqual(len(excerpts), 9)
        for path, start, end, excerpt in excerpts:
            lines = (REPOSITORY / path).read_text().splitlines()
            covered = "\n".join(lines[int(start) - 1:int(end or start)])
            self.assertIn(re.sub(r"\s+", " ", excerpt).strip(),
                          re.sub(r"\s+", " ", covered), path + "#L" + start)

    def test_markdown_fragment_links_resolve(self):
        """GitHub turns each heading into an anchor; links to .md fragments must match one."""
        def anchors(path):
            text = re.sub(r"(?ms)^```.*?^```", "", path.read_text())
            found = set()
            for heading in re.findall(r"(?m)^#{1,6} (.+)$", text):
                slug = re.sub(r"[^\w\- ]", "", heading.replace("`", "").lower())
                found.add(slug.replace(" ", "-"))
            found.update(re.findall(r'<a id="([^"]+)"', text))
            return found
        documents = [REPOSITORY / "README.md", *sorted(renderer.ROOT.glob("*.md"))]
        for document in documents:
            for target, fragment in re.findall(r"\]\(([\w./-]*\.md)?#([^)\s]+)\)", document.read_text()):
                path = (document.parent / target).resolve() if target else document
                with self.subTest(document=document.name, link=target + "#" + fragment):
                    self.assertTrue(path.is_file())
                    self.assertTrue(fragment in anchors(path), "no heading or anchor named " + fragment)

    def test_github_code_links_name_symbols_in_anchored_lines(self):
        """Line anchors drift when code moves; fail instead of misleading readers."""
        checked = 0
        documents = [REPOSITORY / "README.md", *sorted(renderer.ROOT.glob("*.md"))]
        for document in documents:
            for label, path, start, end in CODE_LINK.findall(document.read_text()):
                where = document.name + ": " + path + "#L" + start
                checked += 1
                with self.subTest(link=where, label=label):
                    target = REPOSITORY / path
                    self.assertTrue(target.is_file(), where)
                    self.assertTrue(start, where + " needs a #L line anchor")
                    lines = target.read_text().splitlines()
                    first, last = int(start), int(end or start)
                    self.assertTrue(1 <= first <= last <= len(lines), where)
                    symbols = re.findall(r"`([^`]+)`", label)
                    self.assertTrue(symbols, where + ": link text must name a symbol in backticks")
                    window = "\n".join(lines[max(0, first - 1 - LINK_SLACK):last + LINK_SLACK])
                    for symbol in symbols:
                        for part in re.split(r"[.\s]+", symbol.removesuffix("()")):
                            if part:
                                self.assertIn(part, window, where + ": " + symbol)
        self.assertGreaterEqual(checked, 60)

    def test_all_seven_reviews_keep_nine_questions(self):
        source = (renderer.ROOT / "PHASE-REVIEWS.md").read_text()
        phases = re.split(r"(?m)^## Phase \d review:", source)[1:]
        self.assertEqual(len(phases), 7)
        for phase in phases:
            questions = re.findall(r"(?m)^### ([1-9])\. ", phase)
            self.assertEqual(questions[:9], list("123456789"))
        html = (renderer.ROOT / "PHASE-REVIEWS.html").read_text()
        self.assertEqual(html.count('<code class="language-go">'), 22)

    def test_historical_go_excerpts_match_source(self):
        class Excerpts(HTMLParser):
            def __init__(self):
                super().__init__(convert_charrefs=True)
                self.file = None
                self.capturing = False
                self.text = []
                self.excerpts = []

            def handle_starttag(self, tag, attributes):
                attrs = dict(attributes)
                if tag == "a" and attrs.get("href", "").endswith(".go"):
                    self.file = attrs["href"]
                if tag == "code" and attrs.get("class") == "language-go":
                    self.capturing = True
                    self.text = []

            def handle_data(self, data):
                if self.capturing:
                    self.text.append(data)

            def handle_endtag(self, tag):
                if tag == "code" and self.capturing:
                    self.excerpts.append((self.file, "".join(self.text)))
                    self.capturing = False

        parser = Excerpts()
        parser.feed((renderer.ROOT / "PHASE-REVIEWS.html").read_text())
        self.assertEqual(len(parser.excerpts), 22)
        for filename, excerpt in parser.excerpts:
            self.assertIsNotNone(filename)
            actual = (renderer.ROOT / filename).read_text()
            self.assertIn(re.sub(r"\s+", " ", excerpt).strip(),
                          re.sub(r"\s+", " ", actual), filename)

    def test_unknown_blocks_and_malformed_figures_fail(self):
        with self.assertRaises(ValueError):
            renderer.render("# Guide\n## Chapter\n```unknown\ncontent\n```\n")
        with self.assertRaises(ValueError):
            renderer.render("# Guide\n## Chapter\n```diagram\nCaption\nOne\nTwo\n```\n")
        with self.assertRaises(ValueError):
            renderer.render("# Guide\n## Chapter\n```go\nunclosed")

    def test_text_and_code_are_escaped(self):
        output = renderer.render("# Guide\n## Chapter\n<script>alert(1)</script>\n\n```go\nx < y\n```\n")
        self.assertIn("&lt;script&gt;alert(1)&lt;/script&gt;", output)
        self.assertIn("x &lt; y", output)
        self.assertNotIn("<script>alert(1)</script>", output)


if __name__ == "__main__":
    unittest.main()
