// Command docs checks the book or synchronizes questions from the question bank.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

const (
	markdownExtension = ".md"
	htmlExtension     = ".html"
	svgExtension      = ".svg"
)

const github = "https://github.com/sidsharma96/receipt/"

type book struct {
	root, docs string
	documents  map[string]string
}

func readBook(root string) (book, error) {
	b := book{root: root, docs: filepath.Join(root, "docs"), documents: map[string]string{}}
	paths, err := filesUnder(b.docs)
	if err != nil {
		return b, err
	}
	paths = append(paths, filepath.Join(root, "README.md"))
	for _, path := range paths {
		if filepath.Ext(path) != markdownExtension || within(filepath.Join(b.docs, "book"), path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return b, fmt.Errorf("read document: %w", err)
		}
		b.documents[path] = string(data)
	}
	return b, nil
}

func filesUnder(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", root, err)
	}
	return paths, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Fences are removed line by line; Go's regexp does not support backreferences.
func withoutCode(text string) string {
	var result []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		if fence != "" {
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fence = line[:3]
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

var (
	markdownLinks = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	externalLink  = regexp.MustCompile(`^[a-z]+:|^//`)
)

func (b book) check() error {
	return errors.Join(b.sourceLinks(), b.codeLinks(), b.excerpts(), b.prose(), b.summary(), b.assets(), b.questions(), b.site())
}

func run(args []string) error {
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	b, err := readBook(root)
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "sync" {
		return b.syncQuestions()
	}
	if len(args) != 1 || args[0] != "check" {
		return fmt.Errorf("usage: go run ./tools/docs {check|sync}")
	}
	return b.check()
}

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	log.Print("Documentation checks complete.")
}
