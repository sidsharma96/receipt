package fingerprint

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sidsharma96/receipt/internal/observe"
)

type gitKey struct{}

type gitSelection struct {
	path string
	err  error
}

// WithGit selects discovery's Git once for this invocation. It does not cache
// repository observations or alter the command that Receipt executes for users.
// A new invocation must start from a context without this selection.
func WithGit(ctx context.Context) context.Context {
	if _, ok := ctx.Value(gitKey{}).(gitSelection); ok {
		return ctx
	}
	defer observe.Measure(ctx, observe.GitSelection)()
	path, err := exec.LookPath("git")
	if err == nil {
		path = developerGit(ctx, path, runtime.GOOS, func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "/usr/bin/xcrun", "--find", "git").Output()
		})
	}
	return context.WithValue(ctx, gitKey{}, gitSelection{path: path, err: err})
}

// Only Apple's exact launcher path is eligible. Do not follow a custom symlink
// or bypass a PATH wrapper. xcrun honors the active developer-tool selection.
func developerGit(ctx context.Context, selected, platform string, find func(context.Context) ([]byte, error)) string {
	if platform != "darwin" || selected != "/usr/bin/git" {
		return selected
	}
	b, err := find(ctx)
	if err != nil {
		return selected
	}
	path := strings.TrimSpace(string(b))
	if !filepath.IsAbs(path) || path == selected || strings.ContainsAny(path, "\x00\r\n") {
		return selected
	}
	if resolved, err := exec.LookPath(path); err == nil {
		return resolved
	}
	return selected
}
