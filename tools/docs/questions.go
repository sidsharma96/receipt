package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	levels          = []string{"Newcomer", "Practitioner", "Expert"}
	questionPattern = regexp.MustCompile(`(?ms)^### .*?^</details>\n`)
	marker          = regexp.MustCompile(`(?m)^<!-- check: (guide/[\w.-]+\.md) -->\n`)
	questionBlock   = regexp.MustCompile(`(?s)(<!-- questions:start -->\n)(.*?)(<!-- questions:end -->)`)
	chapterName     = regexp.MustCompile(`^\d\d-.*\.md$`)
	learnMore       = regexp.MustCompile(`\n\*\*Learn more:\*\* (.+)\n`)
	sectionLink     = regexp.MustCompile(`\]\((\.\./|#)[^)]+\)`)
)

func validateQuestion(block, level string) error {
	if !strings.Contains(block, "\n**Level:** "+level+"\n") || !strings.Contains(block, "\n<details>\n<summary>Answer</summary>\n\n") || !strings.HasSuffix(block, "\n</details>\n") || strings.Count(block, "<details>") != 1 {
		return fmt.Errorf("malformed question in %s bank", level)
	}
	learn := learnMore.FindStringSubmatch(block)
	if len(learn) != 2 || !sectionLink.MatchString(learn[1]) {
		return fmt.Errorf("question needs a Learn more book link")
	}
	if strings.Contains(block, "—") {
		return fmt.Errorf("question contains an em dash")
	}
	return nil
}

func (b book) selected() (map[string][]string, error) {
	chosen := map[string][]string{}
	for _, level := range levels {
		text := b.documents[filepath.Join(b.docs, "questions", strings.ToLower(level)+".md")]
		blocks := questionPattern.FindAllString(text, -1)
		if len(blocks) == 0 || len(blocks) != strings.Count(text, "\n### ") {
			return nil, fmt.Errorf("malformed or empty %s question bank", level)
		}
		for _, block := range blocks {
			if err := validateQuestion(block, level); err != nil {
				return nil, err
			}
			for _, m := range marker.FindAllStringSubmatch(block, -1) {
				chosen[m[1]] = append(chosen[m[1]], marker.ReplaceAllString(block, ""))
			}
		}
	}
	return chosen, nil
}

func (b book) questionOutputs() (map[string]string, error) {
	chosen, err := b.selected()
	if err != nil {
		return nil, err
	}
	outputs := map[string]string{}
	for path, text := range b.documents {
		if filepath.Dir(path) != filepath.Join(b.docs, "guide") || !chapterName.MatchString(filepath.Base(path)) {
			continue
		}
		if len(questionBlock.FindAllString(text, -1)) != 1 {
			return nil, fmt.Errorf("%s needs exactly one questions block", path)
		}
		key := "guide/" + filepath.Base(path)
		blocks := chosen[key]
		if len(blocks) == 0 {
			return nil, fmt.Errorf("%s needs selected questions", path)
		}
		delete(chosen, key)
		outputs[path] = questionBlock.ReplaceAllStringFunc(text, func(s string) string {
			m := questionBlock.FindStringSubmatch(s)
			return m[1] + "\n" + strings.Join(blocks, "\n") + "\n" + m[3]
		})
	}
	for path := range chosen {
		return nil, fmt.Errorf("questions name missing chapter: %s", path)
	}
	return outputs, nil
}

func (b book) questions() error {
	outputs, err := b.questionOutputs()
	if err != nil {
		return err
	}
	for path, text := range outputs {
		if b.documents[path] != text {
			return fmt.Errorf("%s: stale questions; run go run ./tools/docs sync", path)
		}
	}
	return nil
}

func (b book) syncQuestions() error {
	outputs, err := b.questionOutputs()
	if err != nil {
		return err
	}
	for path, text := range outputs {
		if b.documents[path] == text {
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return fmt.Errorf("write questions: %w", err)
		}
	}
	return nil
}
