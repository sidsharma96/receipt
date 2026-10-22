package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	headings   = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
	labelLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	explicitID = regexp.MustCompile(`<a id="([^"]+)"`)
)

func headingIDs(text string) map[string]bool {
	text = withoutCode(text)
	found, counts := map[string]bool{}, map[string]int{}
	for _, m := range headings.FindAllStringSubmatch(text, -1) {
		heading := strings.ToLower(strings.ReplaceAll(labelLink.ReplaceAllString(m[1], "$1"), "`", ""))
		slug := strings.Map(func(r rune) rune {
			if r == ' ' {
				return '-'
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
				return r
			}
			return -1
		}, heading)
		count := counts[slug]
		counts[slug]++
		if count > 0 {
			slug += "-" + strconv.Itoa(count)
		}
		found[slug] = true
	}
	for _, m := range explicitID.FindAllStringSubmatch(text, -1) {
		found[m[1]] = true
	}
	return found
}

func (b book) sourceLinks() error {
	var problems []error
	for path, text := range b.documents {
		for _, m := range markdownLinks.FindAllStringSubmatch(withoutCode(text), -1) {
			if externalLink.MatchString(m[1]) {
				continue
			}
			if err := b.sourceLink(path, m[1]); err != nil {
				problems = append(problems, fmt.Errorf("%s: %s: %w", path, m[1], err))
			}
		}
	}
	return errors.Join(problems...)
}

func (b book) sourceLink(path, link string) error {
	filename, fragment, _ := strings.Cut(link, "#")
	target := path
	if filename != "" {
		target = filepath.Clean(filepath.Join(filepath.Dir(path), filename))
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("missing target: %w", err)
	}
	if within(b.docs, path) && filename != "" && (!within(b.docs, target) || filepath.Base(target) == "README.md") {
		return fmt.Errorf("book links must stay inside docs and avoid README.md")
	}
	if fragment != "" && (filepath.Ext(target) != markdownExtension || !headingIDs(b.documents[target])[fragment]) {
		return fmt.Errorf("missing heading %q", fragment)
	}
	return nil
}

var (
	repositoryLink = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\]]*\])+)\]\(` + regexp.QuoteMeta(github) + `(blob|tree)/main/([^)#\s]+)(?:#L(\d+)(?:-L(\d+))?)?\)`)
	symbols        = regexp.MustCompile("`([^`]+)`")
	symbolParts    = regexp.MustCompile(`[.\s]+`)
)

func (b book) codeLinks() error {
	var problems []error
	for path, text := range b.documents {
		for _, m := range repositoryLink.FindAllStringSubmatch(text, -1) {
			if err := b.codeLink(m); err != nil {
				problems = append(problems, fmt.Errorf("%s: %s: %w", path, m[0], err))
			}
		}
	}
	return errors.Join(problems...)
}

func (b book) codeLink(m []string) error {
	target := filepath.Join(b.root, m[3])
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("missing code target: %w", err)
	}
	names := symbols.FindAllStringSubmatch(m[1], -1)
	if m[4] == "" {
		if len(names) > 0 && m[2] == "blob" && filepath.Ext(target) == ".go" {
			return fmt.Errorf("a Go symbol link needs a line anchor; use a plain label for a file")
		}
		return nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read code target: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	first, last, err := lineRange(m[4], m[5], len(lines))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("anchored link needs a symbol in backticks")
	}
	window := strings.Join(lines[max(0, first-4):min(len(lines), last+3)], "\n")
	for _, name := range names {
		for _, part := range symbolParts.Split(strings.TrimSuffix(name[1], "()"), -1) {
			if part != "" && !strings.Contains(window, part) {
				return fmt.Errorf("anchor does not name %s", name[1])
			}
		}
	}
	return nil
}

func lineRange(start, end string, count int) (int, int, error) {
	first, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start line: %w", err)
	}
	last := first
	if end != "" {
		last, err = strconv.Atoi(end)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end line: %w", err)
		}
	}
	if first < 1 || last < first || last > count {
		return 0, 0, fmt.Errorf("line range %d-%d outside %d lines", first, last, count)
	}
	return first, last, nil
}

var excerptPattern = regexp.MustCompile(`(?s)\]\(` + regexp.QuoteMeta(github) + `blob/main/([^)#]+\.go)#L(\d+)(?:-L(\d+))?\):\n\n` + "```go\\n(.*?)\\n```")

func (b book) excerpts() error {
	for path, text := range b.documents {
		if !within(filepath.Join(b.docs, "guide"), path) {
			continue
		}
		excerpts := excerptPattern.FindAllStringSubmatch(text, -1)
		if len(excerpts) != strings.Count(text, "```go\n") {
			return fmt.Errorf("%s: every Go excerpt needs attribution", path)
		}
		for _, m := range excerpts {
			if err := b.excerpt(m); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	return nil
}

func (b book) excerpt(m []string) error {
	data, err := os.ReadFile(filepath.Join(b.root, m[1]))
	if err != nil {
		return fmt.Errorf("read excerpt source: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	first, last, err := lineRange(m[2], m[3], len(lines))
	if err != nil {
		return err
	}
	covered := strings.Join(strings.Fields(strings.Join(lines[first-1:last], "\n")), " ")
	if !strings.Contains(covered, strings.Join(strings.Fields(m[4]), " ")) {
		return fmt.Errorf("excerpt differs from %s#L%s", m[1], m[2])
	}
	return nil
}

var (
	dates        = regexp.MustCompile(`\b(?:19|20)\d{2}-\d{2}-\d{2}\b|\b\d{1,2} (?:January|February|March|April|May|June|July|August|September|October|November|December)\b|\b[0-9a-f]{40,64}\b`)
	destinations = regexp.MustCompile(`\]\([^)]*\)`)
)

func (b book) prose() error {
	for path, text := range b.documents {
		if dates.MatchString(destinations.ReplaceAllString(text, "]")) {
			return fmt.Errorf("%s: keep dates and full hashes out of reader prose", path)
		}
	}
	return nil
}

func (b book) summary() error {
	listed := map[string]bool{}
	for _, m := range markdownLinks.FindAllStringSubmatch(b.documents[filepath.Join(b.docs, "SUMMARY.md")], -1) {
		if filepath.Ext(m[1]) != markdownExtension {
			continue
		}
		path := filepath.Join(b.docs, m[1])
		if listed[path] {
			return fmt.Errorf("duplicate summary page: %s", m[1])
		}
		if _, ok := b.documents[path]; !ok {
			return fmt.Errorf("missing summary page: %s", m[1])
		}
		listed[path] = true
	}
	for path := range b.documents {
		if within(b.docs, path) && filepath.Base(path) != "SUMMARY.md" && !listed[path] {
			return fmt.Errorf("page absent from SUMMARY.md: %s", path)
		}
	}
	return nil
}

func validSVG(data string) error {
	decoder := xml.NewDecoder(strings.NewReader(data))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if roots != 1 || depth != 0 {
				return fmt.Errorf("SVG needs one complete root element")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid SVG: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 || value.Name.Local != "svg" {
					return fmt.Errorf("expected one SVG root")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return fmt.Errorf("text outside SVG root")
			}
		}
	}
}

func (b book) assets() error {
	paths, err := filesUnder(b.docs)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if within(filepath.Join(b.docs, "book"), path) || filepath.Base(path) == "book.toml" {
			continue
		}
		ext := filepath.Ext(path)
		allowed := ext == markdownExtension || ext == svgExtension || (ext == ".png" && within(filepath.Join(b.docs, "assets"), path)) || (ext == ".pprof" && within(filepath.Join(b.docs, "measurements"), path))
		if !allowed || strings.HasPrefix(filepath.Base(path), ".") {
			return fmt.Errorf("non-document source asset: %s", path)
		}
		if ext == svgExtension {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read SVG: %w", err)
			}
			if err := validSVG(string(data)); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	return nil
}
