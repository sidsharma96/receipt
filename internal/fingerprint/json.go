package fingerprint

import (
	"errors"
	"fmt"
	"receipt/internal/strictjson"
	"regexp"
)

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ValidHash reports whether text is a lowercase SHA-256 digest.
func ValidHash(text string) bool { return digestPattern.MatchString(text) }

// UnmarshalJSON validates one declared input without changing its wire encoding.
func (in *Input) UnmarshalJSON(data []byte) error {
	type wire Input
	var next wire
	if err := strictjson.Object(data, &next, strictjson.Field{Name: "kind"}, strictjson.Field{Name: "name"}, strictjson.Field{Name: "hash"}); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if err := Input(next).validate(); err != nil {
		return err
	}
	*in = Input(next)
	return nil
}

func (in Input) validate() error {
	var errs []error
	switch in.Kind {
	case FileInput, EnvironmentInput, ExecutableInput, ToolchainInput:
	default:
		errs = append(errs, fmt.Errorf("input.kind: unknown value %q", in.Kind))
	}
	if in.Name == "" {
		errs = append(errs, fmt.Errorf("input.name: must not be empty"))
	}
	if !ValidHash(in.Hash) {
		errs = append(errs, fmt.Errorf("input.hash: expected lowercase SHA-256"))
	}
	return errors.Join(errs...)
}

// UnmarshalJSON enforces the version-one manifest schema. Empty context strings
// and null inputs remain accepted for compatibility; a runnable command is required.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type wire Manifest
	var next wire
	if err := strictjson.Object(data, &next,
		strictjson.Field{Name: "version"}, strictjson.Field{Name: "root"},
		strictjson.Field{Name: "directory"}, strictjson.Field{Name: "platform"},
		strictjson.Field{Name: "command", StringArray: true}, strictjson.Field{Name: "inputs", Nullable: true}); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if err := Manifest(next).Validate(); err != nil {
		return err
	}
	*m = Manifest(next)
	return nil
}

// Validate checks semantic rules for both decoded and directly constructed manifests.
// Context strings are recorded identity, not paths that the server executes.
func (m Manifest) Validate() error {
	var errs []error
	if m.Version != 1 {
		errs = append(errs, fmt.Errorf("manifest.version: expected 1"))
	}
	if len(m.Command) == 0 || m.Command[0] == "" {
		errs = append(errs, fmt.Errorf("manifest.command: executable required"))
	}
	seen := make(map[string]map[string]bool)
	for i, in := range m.Inputs {
		if err := in.validate(); err != nil {
			errs = append(errs, fmt.Errorf("manifest.inputs[%d]: %w", i, err))
		}
		kind := string(in.Kind)
		if seen[kind] == nil {
			seen[kind] = make(map[string]bool)
		}
		if seen[kind][in.Name] {
			errs = append(errs, fmt.Errorf("manifest.inputs[%d]: repeated kind/name", i))
		}
		seen[kind][in.Name] = true
	}
	return errors.Join(errs...)
}
