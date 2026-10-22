package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type page struct {
	ids   map[string]bool
	links []string
}

// mdBook emits quoted attributes. Strip raw script/style text and comments first
// so example markup there cannot manufacture an anchor or a link.
var (
	rawHTML    = regexp.MustCompile(`(?is)<!--.*?-->|<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>`)
	htmlTags   = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9:-]*)(?:\s+(?:[^>"']|"[^"]*"|'[^']*')*)?\s*/?>`)
	attributes = regexp.MustCompile(`(?:^|\s)(id|href|src)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

func parsePage(text string) page {
	p := page{ids: map[string]bool{}}
	for _, tag := range htmlTags.FindAllString(rawHTML.ReplaceAllString(text, ""), -1) {
		for _, m := range attributes.FindAllStringSubmatch(tag, -1) {
			value := html.UnescapeString(m[2] + m[3] + m[4])
			if m[1] == "id" {
				p.ids[value] = true
			} else if value != "" {
				p.links = append(p.links, value)
			}
		}
	}
	return p
}

func (b book) site() error {
	root := filepath.Join(b.docs, "book")
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		return fmt.Errorf("build the book first with make docs: %w", err)
	}
	paths, err := filesUnder(root)
	if err != nil {
		return err
	}
	pages := map[string]page{}
	for _, path := range paths {
		if !webAsset(root, path) {
			return fmt.Errorf("non-web asset in built site: %s", path)
		}
		if filepath.Ext(path) != htmlExtension {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read built page: %w", err)
		}
		pages[path] = parsePage(string(data))
	}
	return siteLinks(pages)
}

func webAsset(root, path string) bool {
	if filepath.Base(path) == ".nojekyll" {
		return true
	}
	switch filepath.Ext(path) {
	case htmlExtension, ".css", ".js", svgExtension, ".png", ".woff2", ".json", ".txt", "":
		return true
	case ".pprof":
		return within(filepath.Join(root, "measurements"), path)
	}
	return false
}

func siteLinks(pages map[string]page) error {
	for path, p := range pages {
		switch filepath.Base(path) {
		case "print.html", "toc.html", "404.html":
			continue
		}
		for _, link := range p.links {
			if externalLink.MatchString(link) {
				continue
			}
			filename, fragment, _ := strings.Cut(link, "#")
			target := path
			if filename != "" {
				target = filepath.Clean(filepath.Join(filepath.Dir(path), filename))
			}
			if _, err := os.Stat(target); err != nil {
				return fmt.Errorf("%s: missing %s: %w", path, link, err)
			}
			if fragment != "" && filepath.Ext(target) == htmlExtension && !pages[target].ids[fragment] {
				return fmt.Errorf("%s: missing anchor in %s", path, link)
			}
		}
	}
	return nil
}
