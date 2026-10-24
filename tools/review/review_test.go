package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const goodResponse = `{"is_error":false,"subtype":"success","modelUsage":{"claude-opus-5-5":{}},"permission_denials":[],"structured_output":{"verdict":"APPROVE","summary":"Inspected the change.","findings":[],"limitations":["Tests were supplied by the implementer."]}}`

func TestResponseRejectsIncompleteAndFallbackResults(t *testing.T) {
	if _, err := validResponse([]byte(goodResponse)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing success flag": strings.Replace(goodResponse, `"is_error":false,`, "", 1),
		"service error":        strings.Replace(goodResponse, `"is_error":false`, `"is_error":true`, 1),
		"incomplete":           strings.Replace(goodResponse, `"success"`, `"error_max_turns"`, 1),
		"fallback":             strings.Replace(goodResponse, model, "other-model", 1),
		"extra model":          strings.Replace(goodResponse, `"modelUsage":{`, `"modelUsage":{"other-model":{},`, 1),
		"missing model":        strings.Replace(goodResponse, `"modelUsage":{"claude-opus-5-5":{}},`, "", 1),
		"denied access":        strings.Replace(goodResponse, `"permission_denials":[]`, `"permission_denials":[{}]`, 1),
		"missing findings":     strings.Replace(goodResponse, `"findings":[],`, "", 1),
		"null findings":        strings.Replace(goodResponse, `"findings":[]`, `"findings":null`, 1),
		"null entry":           strings.Replace(goodResponse, `"findings":[]`, `"findings":[null]`, 1),
		"wrong type":           strings.Replace(goodResponse, `"findings":[]`, `"findings":[3]`, 1),
		"unknown verdict":      strings.Replace(goodResponse, verdictApprove, "MAYBE", 1),
		"extra field":          strings.Replace(goodResponse, `"findings":[]`, `"extra":true,"findings":[]`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := validResponse([]byte(input)); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestApprovalBindsAllInputsAndPurpose(t *testing.T) {
	current := fingerprint{Tree: "tree", Base: "base", RoleSHA256: "role", CatalogSHA256: "catalog", EvidenceSHA256: "evidence"}
	o := options{reviewer: "code", purpose: purposeChange}
	original := report{Purpose: purposeChange, Reviewer: "code", Model: model, Effort: effort, Inputs: current, Response: json.RawMessage(goodResponse)}
	if err := verifyReport(original, current, o); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*report){
		"tree":     func(r *report) { r.Inputs.Tree = "new" },
		"base":     func(r *report) { r.Inputs.Base = "new" },
		"role":     func(r *report) { r.Inputs.RoleSHA256 = "new" },
		"catalog":  func(r *report) { r.Inputs.CatalogSHA256 = "new" },
		"evidence": func(r *report) { r.Inputs.EvidenceSHA256 = "new" },
		"reviewer": func(r *report) { r.Reviewer = "architecture" },
		"model":    func(r *report) { r.Model = "fallback" },
		"effort":   func(r *report) { r.Effort = "high" },
		"baseline": func(r *report) { r.Purpose = "baseline" },
		"design":   func(r *report) { r.Purpose = "design" },
		"changes requested": func(r *report) {
			r.Response = json.RawMessage(strings.Replace(goodResponse, verdictApprove, "REQUEST_CHANGES", 1))
		},
		"insufficient": func(r *report) {
			r.Response = json.RawMessage(strings.Replace(goodResponse, verdictApprove, "INSUFFICIENT_EVIDENCE", 1))
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := original
			change(&r)
			if err := verifyReport(r, current, o); err == nil {
				t.Fatal("accepted stale or unapproved report")
			}
		})
	}
}

func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), root, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, root, path, content string) {
	t.Helper()
	if err := writeInput(filepath.Join(root, path), []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, options) {
	t.Helper()
	root := t.TempDir()
	fixtureGit(t, root, "init", "-q")
	put(t, root, ".git/info/exclude", "/.claude/\n/review/upstream.json\n/.tools/\n/docs/\n")
	put(t, root, "main.go", "package main\n")
	fixtureGit(t, root, "add", "main.go")
	fixtureGit(t, root, "-c", "user.name=Review test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	put(t, root, ".claude/agents/code-reviewer.md", "---\nname: code-reviewer\nmodel: "+model+"\neffort: low\ntools: Read, Grep, Glob\n---\nInspect code independently.\n")
	put(t, root, catalogPath, `{"sources":[{"project":"example","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","url":"https://github.com/example/project/blob/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/file.go","observation":"Selected example"}]}`)
	put(t, root, ".tools/evidence.txt", "Scope: fixture. Tests supplied, not run by reviewer.")
	return root, options{reviewer: "code", base: "HEAD", purpose: purposeChange, evidence: filepath.Join(root, ".tools/evidence.txt"), report: filepath.Join(root, ".tools/report.json")}
}

func TestSnapshotUsesStagedBlobsAndExcludesPrivateFiles(t *testing.T) {
	root, o := fixture(t)
	put(t, root, "main.go", "package main\n// changed\n")
	put(t, root, ".gitattributes", "main.go export-ignore\n")
	put(t, root, "docs/private.md", "private book")
	fixtureGit(t, root, "add", "main.go", ".gitattributes")
	f, in, err := capture(context.Background(), root, o)
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := snapshot(context.Background(), root, destination, f, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "source/main.go"))
	if err != nil || !strings.Contains(string(data), "changed") {
		t.Fatalf("snapshot contents: %q, %v", data, err)
	}
	for _, path := range []string{"source/.git", "source/docs", "source/.claude", "source/review/upstream.json", "source/.tools"} {
		if _, err := os.Stat(filepath.Join(destination, path)); !os.IsNotExist(err) {
			t.Fatalf("leaked %s", path)
		}
	}
	patch, err := os.ReadFile(filepath.Join(destination, "review-input/change.patch"))
	if err != nil || !strings.Contains(string(patch), "+// changed") {
		t.Fatalf("missing diff: %v", err)
	}
	put(t, root, "main.go", "unstaged edit")
	if _, _, err := capture(context.Background(), root, o); err == nil {
		t.Fatal("accepted unstaged edit")
	}
}

func TestRejectsUntrackedAndForbiddenSnapshotEntries(t *testing.T) {
	t.Run("untracked", func(t *testing.T) {
		root, o := fixture(t)
		put(t, root, "forgotten.go", "new code")
		if _, _, err := capture(context.Background(), root, o); err == nil {
			t.Fatal("omitted untracked code")
		}
	})
	for _, path := range []string{"docs/private.md", ".claude/secret", catalogPath} {
		t.Run(path, func(t *testing.T) {
			root, o := fixture(t)
			// Simulate a historical forbidden entry without force-adding private files.
			object := fixtureGit(t, root, "rev-parse", "HEAD:main.go")
			fixtureGit(t, root, "update-index", "--add", "--cacheinfo", "100644,"+object+","+path)
			tree := fixtureGit(t, root, "write-tree")
			if _, err := treeFiles(context.Background(), root, tree); err == nil {
				t.Fatal("accepted private entry")
			}
			if _, _, err := capture(context.Background(), root, o); err == nil {
				t.Fatal("accepted private snapshot")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		root, o := fixture(t)
		if err := os.Symlink("/etc/passwd", filepath.Join(root, "outside")); err != nil {
			t.Skip(err)
		}
		fixtureGit(t, root, "add", "outside")
		if _, _, err := capture(context.Background(), root, o); err == nil {
			t.Fatal("accepted symlink")
		}
	})
}

func TestRoleAndReportSafeguards(t *testing.T) {
	root, o := fixture(t)
	ctx := context.Background()
	if err := reportDestination(ctx, root, o.report); err != nil {
		t.Fatal(err)
	}
	if err := reportDestination(ctx, root, filepath.Join(root, "public.json")); err == nil {
		t.Fatal("accepted nonignored report")
	}
	r := report{Response: json.RawMessage(goodResponse)}
	if err := writeReport(o.report, r); err != nil {
		t.Fatal(err)
	}
	if err := writeReport(o.report, r); err == nil {
		t.Fatal("overwrote a report")
	}
	if err := reportDestination(ctx, root, o.report); err == nil {
		t.Fatal("accepted existing report")
	}
	put(t, root, ".claude/agents/code-reviewer.md", "---\nname: code-reviewer\nmodel: fallback\neffort: low\ntools: Read, Grep, Glob\n---\nReview code.")
	if _, _, err := capture(ctx, root, o); err == nil {
		t.Fatal("accepted fallback role")
	}
}

func TestRoleRejectsDuplicateEmptySetting(t *testing.T) {
	role := "---\nname: code-reviewer\nmodel:\nmodel: " + model + "\neffort: low\ntools: Read, Grep, Glob\n---\nInspect the code.\n"
	if _, err := rolePrompt([]byte(role), "code"); err == nil {
		t.Fatal("accepted duplicate setting after an empty value")
	}
}

func TestInvokeWithSubstituteClaude(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("shell substitute requires Unix")
	}
	root, o := fixture(t)
	f, in, err := capture(context.Background(), root, o)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	put(t, bin, "claude", "#!/bin/sh\ntest -f source/main.go || exit 2\ntest -f review-input/upstream.json || exit 3\ncat > \"$REVIEW_TEST_PROMPT\"\nprintf '%s\\n' \"$@\" > \"$REVIEW_TEST_ARGS\"\nprintf '%s' \"$REVIEW_TEST_RESPONSE\"\n")
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REVIEW_TEST_RESPONSE", goodResponse)
	t.Setenv("REVIEW_TEST_PROMPT", filepath.Join(bin, "prompt"))
	t.Setenv("REVIEW_TEST_ARGS", filepath.Join(bin, "args"))
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatal(err)
	}
	raw, err := invoke(context.Background(), root, f, in, o)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != goodResponse {
		t.Fatal("unexpected response")
	}
	args, err := os.ReadFile(filepath.Join(bin, "args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"--model\n" + model, "--effort\nlow", "--safe-mode", "--restricted", "--strict-mcp-config", "--no-session-persistence", "--permission-mode\ndontAsk", "--tools\nRead,Grep,Glob", "--allowedTools\nRead,Grep,Glob"} {
		if !strings.Contains(string(args), required+"\n") {
			t.Fatalf("missing %s", required)
		}
	}
	t.Setenv("REVIEW_TEST_RESPONSE", strings.Replace(goodResponse, model, "fallback", 1))
	if _, err := invoke(context.Background(), root, f, in, o); err == nil {
		t.Fatal("accepted fallback invocation")
	}
}
