package fingerprint

import (
	"context"
	"os"
	"path/filepath"
	"receipt/internal/observe"
)

func executableInput(ctx context.Context, exe string, wantHeader bool) (Input, []byte, error) {
	defer observe.Measure(ctx, observe.Executable)()
	// Direct pytest needs its shebang. Retain the old full-read behavior there,
	// including arbitrarily long first lines and scripts without a newline.
	if wantHeader {
		data, err := os.ReadFile(exe)
		if err != nil {
			return Input{}, nil, err
		}
		return Input{ExecutableInput, exe, Hash(data)}, data, nil
	}
	digest, err := hashFile(exe, "", 32<<10)
	if err != nil {
		return Input{}, nil, err
	}
	return Input{ExecutableInput, exe, digest}, nil, nil
}

func runtimeProbe(ctx context.Context, dir string, args, probe []string) (Input, error) {
	defer observe.Measure(ctx, observe.Toolchain)()
	b, err := commandOutput(ctx, dir, probe...)
	if err != nil {
		return Input{}, err
	}
	name := "resolved-environment"
	if filepath.Base(args[0]) == "go" && args[1] == testCommand {
		b, err = stableGoEnvironment(b)
		if err != nil {
			return Input{}, err
		}
		name = "resolved-go-environment-v2"
	}
	return Input{ToolchainInput, name, Hash(b)}, nil
}
