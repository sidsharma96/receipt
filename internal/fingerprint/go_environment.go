package fingerprint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// stableGoEnvironment retains Go's settings, normalizing only the random
// suffix in its generated scratch-directory prefix-map flag. Unknown formats
// remain unchanged (and may cause misses), rather than hiding compiler inputs.
func stableGoEnvironment(data []byte) ([]byte, error) {
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode Go environment: %w", err)
	}
	flags, ok := env["GOGCCFLAGS"]
	if !ok {
		return nil, fmt.Errorf("go environment missing GOGCCFLAGS")
	}
	root, ok := env["GOTMPDIR"]
	if !ok {
		return nil, fmt.Errorf("go environment missing GOTMPDIR")
	}
	if root == "" {
		root = os.TempDir()
	}
	// cmd/go uses os.MkdirTemp(GOTMPDIR, "go-build") and maps that
	// directory to /tmp/go-build. Retain the root, flag kind and destination.
	prefix := regexp.QuoteMeta(filepath.Join(root, "go-build"))
	pattern, err := regexp.Compile(`(^|[ \t"'])(-f(?:file|debug)-prefix-map=` + prefix + `)[0-9]+(=/tmp/go-build)([ \t"']|$)`)
	if err != nil {
		return nil, fmt.Errorf("compile Go scratch-path pattern: %w", err)
	}
	env["GOGCCFLAGS"] = pattern.ReplaceAllString(flags, `${1}${2}<scratch>${3}${4}`)
	return json.Marshal(env)
}
