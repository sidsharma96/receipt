package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const schema = `{"type":"object","properties":{"verdict":{"enum":["APPROVE","REQUEST_CHANGES","INSUFFICIENT_EVIDENCE"]},"summary":{"type":"string"},"findings":{"type":"array","items":{"type":"string"}},"limitations":{"type":"array","items":{"type":"string"}}},"required":["verdict","summary","findings","limitations"],"additionalProperties":false}`

func rolePrompt(data []byte, reviewer string) (string, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return "", errors.New("role must begin with frontmatter")
	}
	header, body, ok := strings.Cut(text[4:], "\n---\n")
	if !ok || strings.TrimSpace(body) == "" {
		return "", errors.New("role is missing its instructions")
	}
	settings := make(map[string]string)
	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, ":")
		_, duplicate := settings[key]
		if !ok || duplicate {
			return "", errors.New("invalid or duplicate role setting")
		}
		settings[key] = strings.TrimSpace(value)
	}
	for key, expected := range map[string]string{"name": reviewer + "-reviewer", "model": model, "effort": effort, "tools": "Read, Grep, Glob"} {
		if settings[key] != expected {
			return "", fmt.Errorf("role %s must be %q", key, expected)
		}
	}
	return body, nil
}

func validCatalog(data []byte) error {
	var c struct {
		Sources []struct{ Project, Revision, URL, Observation string }
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("invalid upstream catalog: %w", err)
	}
	if len(c.Sources) == 0 {
		return errors.New("upstream catalog needs pinned sources")
	}
	for _, s := range c.Sources {
		if s.Project == "" || len(s.Revision) != 40 || !strings.Contains(s.URL, "/blob/"+s.Revision+"/") || s.Observation == "" {
			return errors.New("each upstream source needs a project, pinned revision URL and observation")
		}
	}
	return nil
}

func claudeArgs(role string) []string {
	return []string{
		"-p", "--model", model, "--effort", effort,
		"--safe-mode", "--restricted", "--strict-mcp-config", "--no-session-persistence",
		"--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob",
		"--permission-mode", "dontAsk", "--system-prompt", role,
		"--output-format", "json", "--json-schema", schema,
	}
}

func invoke(ctx context.Context, root string, f fingerprint, in reviewInputs, o options) (result json.RawMessage, resultErr error) {
	directory, err := os.MkdirTemp("", "receipt-review-")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(directory)) }()
	if err := snapshot(ctx, root, directory, f, in); err != nil {
		return nil, err
	}
	role, err := rolePrompt(in.role, o.reviewer)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf("Purpose: %s. Reviewer: %s. Base: %s. Tree: %s.\nRead review-input/change.patch, review-input/evidence.txt and review-input/upstream.json. The complete staged source tree is under source/. Inspect surrounding source and callers, not only the patch. For baseline reviews inspect the core package boundaries too. Do not read outside this snapshot. Distinguish supplied check evidence from personal code inspection. Return the structured review.\n", o.purpose, o.reviewer, f.Base, f.Tree)
	cmd := exec.CommandContext(ctx, "claude", claudeArgs(role)...)
	cmd.Dir = directory
	cmd.Stdin = strings.NewReader(prompt)
	// Never echo raw subprocess diagnostics: authentication and service errors may contain secrets.
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude review failed (no approval recorded): %w", err)
	}
	if _, err := validResponse(output); err != nil {
		return nil, err
	}
	return output, nil
}

type verdict struct {
	Verdict     string   `json:"verdict"`
	Summary     string   `json:"summary"`
	Findings    []string `json:"findings"`
	Limitations []string `json:"limitations"`
}

func validResponse(raw []byte) (verdict, error) {
	var v verdict
	var envelope struct {
		IsError           *bool                      `json:"is_error"`
		Subtype           string                     `json:"subtype"`
		ModelUsage        map[string]json.RawMessage `json:"modelUsage"`
		PermissionDenials []json.RawMessage          `json:"permission_denials"`
		Structured        json.RawMessage            `json:"structured_output"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return v, fmt.Errorf("invalid Claude response: %w", err)
	}
	if envelope.IsError == nil || *envelope.IsError || envelope.Subtype != "success" {
		return v, errors.New("claude did not complete successfully")
	}
	if _, ok := envelope.ModelUsage[model]; !ok || len(envelope.ModelUsage) != 1 {
		return v, errors.New("unexpected or missing model usage; fallback models cannot approve")
	}
	if len(envelope.PermissionDenials) > 0 {
		return v, errors.New("reviewer lacked requested access; resolve and re-review")
	}
	if err := validateVerdictFields(envelope.Structured); err != nil {
		return v, err
	}
	if err := json.Unmarshal(envelope.Structured, &v); err != nil {
		return v, err
	}
	if v.Verdict != verdictApprove && v.Verdict != "REQUEST_CHANGES" && v.Verdict != "INSUFFICIENT_EVIDENCE" {
		return v, errors.New("unknown review verdict")
	}
	return v, nil
}

func validateVerdictFields(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != 4 {
		return errors.New("missing or unexpected review fields")
	}
	for _, key := range []string{"verdict", "summary"} {
		var s string
		if err := json.Unmarshal(fields[key], &s); err != nil || strings.TrimSpace(s) == "" {
			return fmt.Errorf("review %s must be a nonempty string", key)
		}
	}
	for _, key := range []string{"findings", "limitations"} {
		var entries []json.RawMessage
		if err := json.Unmarshal(fields[key], &entries); err != nil || entries == nil {
			return fmt.Errorf("review %s must be an array", key)
		}
		for _, entry := range entries {
			var s string
			if err := json.Unmarshal(entry, &s); err != nil || bytes.Equal(bytes.TrimSpace(entry), []byte("null")) {
				return fmt.Errorf("review %s entries must be strings", key)
			}
		}
	}
	return nil
}
