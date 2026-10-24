package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type fingerprint struct {
	Tree, Base, RoleSHA256, CatalogSHA256, EvidenceSHA256 string
}

type reviewInputs struct {
	role, catalog, evidence []byte
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return data, nil
}

func capture(ctx context.Context, root string, o options) (fingerprint, reviewInputs, error) {
	var f fingerprint
	var in reviewInputs
	if err := readyIndex(ctx, root); err != nil {
		return f, in, err
	}
	tree, err := git(ctx, root, "write-tree")
	if err != nil {
		return f, in, err
	}
	base, err := git(ctx, root, "rev-parse", "--verify", "--end-of-options", o.base+"^{commit}")
	if err != nil {
		return f, in, err
	}
	f.Tree, f.Base = strings.TrimSpace(string(tree)), strings.TrimSpace(string(base))
	// Check the base too: deleted private files would otherwise leak through the diff.
	for _, revision := range []string{f.Tree, f.Base} {
		if _, err := treeFiles(ctx, root, revision); err != nil {
			return f, in, err
		}
	}
	in.role, err = os.ReadFile(filepath.Join(root, ".claude", "agents", o.reviewer+"-reviewer.md"))
	if err != nil {
		return f, in, fmt.Errorf("read local reviewer role: %w", err)
	}
	if _, err := rolePrompt(in.role, o.reviewer); err != nil {
		return f, in, err
	}
	in.catalog, err = os.ReadFile(filepath.Join(root, catalogPath))
	if err != nil {
		return f, in, fmt.Errorf("read local upstream catalog: %w", err)
	}
	if err := validCatalog(in.catalog); err != nil {
		return f, in, err
	}
	in.evidence, err = os.ReadFile(o.evidence)
	if err != nil {
		return f, in, err
	}
	if len(strings.TrimSpace(string(in.evidence))) == 0 {
		return f, in, errors.New("evidence must describe scope, checks and limitations")
	}
	f.RoleSHA256, f.CatalogSHA256, f.EvidenceSHA256 = digest(in.role), digest(in.catalog), digest(in.evidence)
	return f, in, nil
}

func readyIndex(ctx context.Context, root string) error {
	for _, args := range [][]string{{"diff", "--name-only", "-z"}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
		out, err := git(ctx, root, args...)
		if err != nil {
			return err
		}
		if len(out) != 0 {
			return errors.New("stage intended source changes and set aside other nonignored edits before review or verification")
		}
	}
	return nil
}

type treeFile struct{ path, object string }

func privatePath(path string) bool {
	return path == "docs" || strings.HasPrefix(path, "docs/") || path == ".claude" || strings.HasPrefix(path, ".claude/") || path == catalogPath
}

func treeFiles(ctx context.Context, root, revision string) ([]treeFile, error) {
	data, err := git(ctx, root, "ls-tree", "-rz", revision)
	if err != nil {
		return nil, err
	}
	var files []treeFile
	for _, entry := range strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00") {
		if entry == "" {
			continue
		}
		metadata, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || !filepath.IsLocal(path) || strings.Contains(path, "\\") {
			return nil, errors.New("invalid snapshot path")
		}
		if privatePath(path) {
			return nil, fmt.Errorf("private path %q is present in the source or comparison base", path)
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return nil, fmt.Errorf("snapshot requires regular files: %q", path)
		}
		files = append(files, treeFile{path: path, object: fields[2]})
	}
	return files, nil
}

func snapshot(ctx context.Context, root, directory string, f fingerprint, in reviewInputs) error {
	files, err := treeFiles(ctx, root, f.Tree)
	if err != nil {
		return err
	}
	// Reading blobs avoids export-ignore/export-subst attributes changing the reviewed tree.
	for _, file := range files {
		data, err := git(ctx, root, "cat-file", "blob", file.object)
		if err != nil {
			return err
		}
		if err := writeInput(filepath.Join(directory, "source", file.path), data); err != nil {
			return err
		}
	}
	patch, err := git(ctx, root, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", f.Base, f.Tree, "--")
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"change.patch": patch, "evidence.txt": in.evidence, "upstream.json": in.catalog} {
		if err := writeInput(filepath.Join(directory, "review-input", name), data); err != nil {
			return err
		}
	}
	return nil
}

func writeInput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
