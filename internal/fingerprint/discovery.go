package fingerprint

import (
	"context"
	"fmt"

	"github.com/sidsharma96/receipt/internal/observe"
)

// BuildAtRoot reuses the root just returned by Root for this invocation's
// configuration lookup. Callers must not retain it across command execution or
// invocations; use Build to discover the root again after running a command.
// Like Build, this observes a mutable working tree, not an atomic snapshot.
func BuildAtRoot(ctx context.Context, root, dir string, args, extra, ignoredEnv []string, generic bool) (Manifest, error) {
	defer observe.Measure(ctx, observe.Fingerprint)()
	if len(args) == 0 {
		return Manifest{}, fmt.Errorf("missing test command")
	}
	return buildAtRoot(ctx, root, dir, args, extra, ignoredEnv, generic)
}
