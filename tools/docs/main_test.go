package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeadingsIgnoreFencesAndKeepDuplicateAnchors(t *testing.T) {
	ids := headingIDs("# A `key`\n# A key\n~~~\n# Fake\n~~~\n<a id=\"explicit\"></a>\n")
	if !ids["a-key"] || !ids["a-key-1"] || !ids["explicit"] || ids["fake"] {
		t.Fatal(ids)
	}
}

func TestBrokenLinksAndDriftingSymbols(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	os.MkdirAll(docs, 0o755)
	path := filepath.Join(docs, "chapter.md")
	os.WriteFile(path, []byte("# Present\n"), 0o600)
	source := filepath.Join(root, "example.go")
	os.WriteFile(source, []byte("package example\nfunc Present() {}\n"), 0o600)
	b := book{root: root, docs: docs, documents: map[string]string{path: "# Present\n"}}
	if err := b.sourceLink(path, "#present"); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"#missing", "absent.md", "../example.go"} {
		if err := b.sourceLink(path, link); err == nil {
			t.Fatal("accepted", link)
		}
	}
	for _, tc := range []struct {
		symbol, anchor string
		fail           bool
	}{{"Present", "#L2", false}, {"Missing", "#L2", true}, {"Present", "#L99", true}, {"Present", "", true}} {
		link := "[`" + tc.symbol + "`](" + github + "blob/main/example.go" + tc.anchor + ")"
		match := repositoryLink.FindStringSubmatch(link)
		if match == nil {
			t.Fatal("link not recognized")
		}
		if err := b.codeLink(match); (err != nil) != tc.fail {
			t.Fatal(link, err)
		}
	}
}

func TestExcerptMustMatchAnchoredSource(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "example.go"), []byte("package example\nfunc Present() {}\n"), 0o600)
	b := book{root: root}
	if err := b.excerpt([]string{"", "example.go", "2", "2", "func Present() {}"}); err != nil {
		t.Fatal(err)
	}
	if err := b.excerpt([]string{"", "example.go", "2", "2", "func Invented() {}"}); err == nil {
		t.Fatal("accepted invented excerpt")
	}
	for _, input := range []string{"", "<svg><g></svg>", "<svg/><svg/>", "not XML"} {
		if err := validSVG(input); err == nil {
			t.Fatal("accepted malformed SVG", input)
		}
	}
	if err := validSVG("<svg><g/></svg>"); err != nil {
		t.Fatal(err)
	}
}

func question(level string) string {
	return "### A question\n<!-- check: guide/01-example.md -->\n\n**Level:** " + level + "\n\nWhy?\n\n<details>\n<summary>Answer</summary>\n\nBecause.\n\n**Learn more:** [Example](../guide/01-example.md)\n\n</details>\n"
}

func TestQuestionsDetectStalenessAndMissingChapters(t *testing.T) {
	b := book{docs: "/docs", documents: map[string]string{}}
	for _, level := range levels {
		b.documents[filepath.Join(b.docs, "questions", strings.ToLower(level)+".md")] = "# Bank\n\n" + question(level)
	}
	chapter := "/docs/guide/01-example.md"
	b.documents[chapter] = "# Example\n<!-- questions:start -->\n<!-- questions:end -->\n"
	if err := b.questions(); err == nil {
		t.Fatal("accepted stale copies")
	}
	outputs, err := b.questionOutputs()
	if err != nil {
		t.Fatal(err)
	}
	b.documents[chapter] = outputs[chapter]
	if err := b.questions(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(outputs[chapter], "<!-- check:") {
		t.Fatal("copied selection markers")
	}
	delete(b.documents, chapter)
	if _, err := b.questionOutputs(); err == nil {
		t.Fatal("accepted missing chapter")
	}
	if err := validateQuestion(strings.ReplaceAll(question("Expert"), "<summary>Answer</summary>", ""), "Expert"); err == nil {
		t.Fatal("accepted malformed question")
	}
}

func TestBuiltLinksUseRealTagsAndDecodedAttributes(t *testing.T) {
	p := parsePage(`<script>const x = '<a id="fake">';</script><!-- <a id="fake"> --><h2 id='a&amp;b'>Title</h2><a href="#a&amp;b">next</a><img src="pic.svg">`)
	if p.ids["fake"] || !p.ids["a&b"] || len(p.links) != 2 || p.links[0] != "#a&b" {
		t.Fatal(p)
	}
	root := t.TempDir()
	path := filepath.Join(root, "index.html")
	os.WriteFile(path, nil, 0o600)
	if err := siteLinks(map[string]page{path: {ids: map[string]bool{"present": true}, links: []string{"#missing"}}}); err == nil {
		t.Fatal("accepted missing built anchor")
	}
}
