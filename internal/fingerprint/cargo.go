package fingerprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"receipt/internal/observe"
	"strings"
	"time"
)

const cargoProbeLimit = 16 << 20

// cargoOptions deliberately describes one package, host-target tests only.
// Arguments outside this grammar fall back to execution, never generic reuse.
type cargoOptions struct {
	metadata []string
}

func parseCargo(args []string) (cargoOptions, error) {
	o := cargoOptions{metadata: []string{"metadata", "--format-version", "1", "--frozen"}}
	if len(args) < 2 || args[1] != testCommand {
		return o, fmt.Errorf("cargo adapter requires cargo test --frozen")
	}
	frozen := false
	seen := map[string]bool{}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if seen[arg] {
			return o, fmt.Errorf("repeated cargo option")
		}
		seen[arg] = true
		switch arg {
		case "--frozen":
			frozen = true
		case "--lib", "--quiet", "-q", "--release":
		case "--all-features", "--no-default-features":
			o.metadata = append(o.metadata, arg)
		case "--manifest-path", "--features":
			i++
			if i == len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return o, fmt.Errorf("cargo option requires a value")
			}
			o.metadata = append(o.metadata, arg, args[i])
		default:
			return o, fmt.Errorf("unsupported cargo option; use one manifest, --frozen and supported feature/test flags")
		}
	}
	if !frozen {
		return o, fmt.Errorf("cargo reuse requires --frozen; prepare dependencies and Cargo.lock first")
	}
	return o, nil
}

func cargoInputs(ctx context.Context, dir string, args []string) ([]Input, error) {
	opts, err := parseCargo(args)
	if err != nil {
		return nil, err
	}
	home, err := cargoPolicy(dir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	inputs, host, err := cargoTools(ctx, dir, args[0])
	if err != nil {
		return nil, err
	}
	opts.metadata = append(opts.metadata, "--filter-platform", host)
	b, err := cargoMetadataOutput(ctx, dir, args[0], opts.metadata)
	if err != nil {
		return nil, err
	}
	var metadata cargoMetadata
	if err := json.Unmarshal(b, &metadata); err != nil {
		return nil, fmt.Errorf("invalid cargo metadata")
	}
	root, err := Root(ctx, dir)
	if err != nil {
		return nil, err
	}
	packages, err := cargoClosure(metadata, root)
	if err != nil {
		return nil, err
	}
	sources, err := cargoSources(ctx, metadata, packages, root, home)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, sources...)
	// Raw metadata includes the feature resolution and any fields this Cargo
	// version adds. Hash only: do not store dependency URLs or metadata contents.
	inputs = append(inputs, Input{ToolchainInput, "cargo-metadata-v1", Hash(b)})
	return inputs, nil
}

func cargoMetadataOutput(ctx context.Context, dir, cargo string, args []string) ([]byte, error) {
	defer observe.Measure(ctx, observe.CargoMetadata)()
	b, err := cargoOutput(ctx, dir, cargo, args...)
	if err != nil {
		return nil, fmt.Errorf("cargo frozen metadata unavailable; check Cargo.lock and prepare dependencies with Cargo outside Receipt, then retry (probe stderr withheld): %w", err)
	}
	return b, nil
}

type cargoBuffer struct{ bytes.Buffer }

func (b *cargoBuffer) Write(p []byte) (int, error) {
	if len(p) > cargoProbeLimit-b.Len() {
		return 0, fmt.Errorf("cargo probe exceeds byte limit")
	}
	return b.Buffer.Write(p)
}

func cargoOutput(ctx context.Context, dir, program string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, program, args...)
	c.Dir = dir
	var out cargoBuffer
	c.Stdout = &out
	// Probe stderr may include private registry URLs. The caller explains fallback
	// without copying it into diagnostics. Actual test stderr is unaffected.
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("cargo probe interrupted: %w", ctx.Err())
		}
		return nil, fmt.Errorf("cargo probe failed: %w", err)
	}
	return out.Bytes(), nil
}
