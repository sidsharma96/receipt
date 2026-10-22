// Command artifacts checks source paths and sizes, including the actual Git index.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

const maxSourceBytes = 1 << 20

type sourceFile struct {
	path string
	size int64
}

func violation(file sourceFile) string {
	if file.path == "scripts/utils" || strings.HasPrefix(file.path, "scripts/utils/") {
		return "personal utilities must remain local"
	}
	switch strings.ToLower(filepath.Ext(file.path)) {
	case ".json", ".jsonl", ".ndjson":
		return "generated data must remain outside source control"
	case ".py", ".pyw", ".pyc", ".pyo":
		return "shared tooling uses Go; keep disposable Python in scripts/utils/"
	}
	if file.size > maxSourceBytes {
		return "source file exceeds 1 MiB"
	}
	return ""
}

func git(root string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func indexFiles(root string) ([]sourceFile, error) {
	data, err := git(root, nil, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	var files []sourceFile
	var objects strings.Builder
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry == "" {
			continue
		}
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("invalid Git index entry")
		}
		if fields[0] == "160000" {
			continue
		}
		if fields[2] != "0" {
			return nil, fmt.Errorf("unmerged index path: %s", path)
		}
		files = append(files, sourceFile{path: path})
		objects.WriteString(fields[1] + "\n")
	}
	if len(files) == 0 {
		return files, nil
	}
	sizes, err := git(root, []byte(objects.String()), "cat-file", "--batch-check=%(objectsize)")
	if err != nil {
		return nil, err
	}
	lines := strings.Fields(string(sizes))
	if len(lines) != len(files) {
		return nil, fmt.Errorf("incomplete Git object sizes")
	}
	for i, line := range lines {
		files[i].size, err = strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("read Git object size: %w", err)
		}
	}
	return files, nil
}

func workingFiles(root string) ([]sourceFile, error) {
	data, err := git(root, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []sourceFile
	for _, path := range strings.Split(string(data), "\x00") {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Lstat(filepath.Join(root, path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if !info.IsDir() {
			files = append(files, sourceFile{path: path, size: info.Size()})
		}
	}
	return files, nil
}

func check(root string, staged bool) error {
	reader := workingFiles
	if staged {
		reader = indexFiles
	}
	files, err := reader(root)
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	var failures []string
	for _, file := range files {
		if reason := violation(file); reason != "" {
			failures = append(failures, file.path+": "+reason)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("source policy violations:\n%s", strings.Join(failures, "\n"))
	}
	return nil
}

func main() {
	log.SetFlags(0)
	staged := flag.Bool("staged", false, "inspect index blobs, rather than working files")
	flag.Parse()
	root, err := toolutil.Root()
	if err == nil {
		err = check(root, *staged)
	}
	if err != nil {
		log.Fatal(err)
	}
}
