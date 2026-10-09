"""Checks for the generated reader, not a claim about learner comprehension."""

from html.parser import HTMLParser
from pathlib import Path
import re
import unittest

import render_working_doc as renderer
import render_review_figures as review_renderer


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
    def test_review_rendering_and_links(self):
        path = renderer.ROOT / "PHASE-REVIEWS.html"
        html = path.read_text()
        self.assertEqual(review_renderer.render(html)[0], html)
        parser = ReaderParser()
        parser.feed(html)
        self.assertFalse(parser.tags)
        self.assertEqual(len(parser.ids), len(set(parser.ids)))
        self.assertEqual(parser.figures, 24)
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
        self.assertEqual(parser.figures, 7)
        self.assertEqual(parser.answers, 7)
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
        source = renderer.SOURCE.read_text()
        pattern = r"\[([^\]]+\.go)\]\(([^)]+\.go)\):\n\n```go\n(.*?)\n```"
        excerpts = re.findall(pattern, source, re.DOTALL)
        self.assertEqual(len(excerpts), 5)
        for _, filename, excerpt in excerpts:
            actual = (renderer.ROOT / filename).read_text()
            self.assertIn(re.sub(r"\s+", " ", excerpt).strip(),
                          re.sub(r"\s+", " ", actual), filename)

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
