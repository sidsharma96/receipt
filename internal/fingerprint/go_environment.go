package fingerprint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Retain the root, flag kind and destination; only the numeric suffix varies.
	flags, err := scratchFlags(flags, filepath.Join(root, "go-build"))
	if err != nil {
		return nil, err
	}

	env["GOGCCFLAGS"] = flags
	return json.Marshal(env)
}
