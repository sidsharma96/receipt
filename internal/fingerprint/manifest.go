// Package fingerprint describes declared inputs and names them by content.
package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"receipt/internal/observe"
	"runtime"
	"slices"
	"sort"
	"strings"
)

const (
	pytestCommand = "pytest"
	cargoCommand  = "cargo"
	testCommand   = "test"
)

// InputKind identifies the declared source of a fingerprint component.
type InputKind string

const (
	// FileInput identifies working-tree or explicitly declared file bytes.
	FileInput InputKind = "file"
	// EnvironmentInput identifies hashed environment values.
	EnvironmentInput InputKind = "environment"
	// ExecutableInput identifies the command executable's bytes.
	ExecutableInput InputKind = "executable"
	// ToolchainInput identifies resolved runtime/package metadata.
	ToolchainInput InputKind = "toolchain"
)

// Input describes one declared input and its content hash.
type Input struct {
	Kind InputKind `json:"kind"`
	Name string    `json:"name"`
	Hash string    `json:"hash"`
}

// Manifest records the inputs that define a cache key; it does not prove output determinism.
type Manifest struct {
	Version   int      `json:"version"`
	Root      string   `json:"root"`
	Directory string   `json:"directory"`
	Platform  string   `json:"platform"`
	Command   []string `json:"command"`
	Inputs    []Input  `json:"inputs"`
}

// Hash returns the lowercase SHA-256 digest of bytes.
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Key hashes a sorted copy of the manifest without changing the caller's inputs.
func (m Manifest) Key() string {
	m.Inputs = append([]Input(nil), m.Inputs...)
	sort.Slice(m.Inputs, func(i, j int) bool {
		if m.Inputs[i].Kind != m.Inputs[j].Kind {
			return m.Inputs[i].Kind < m.Inputs[j].Kind
		}
		return m.Inputs[i].Name < m.Inputs[j].Name
	})
	b, _ := json.Marshal(m) //nolint:errcheck // This concrete type contains only JSON-supported values and no marshal hooks.
	return Hash(b)
}

func commandOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", args[0], err)
	}
	return b, nil
}

// Root resolves the enclosing Git working tree.
func Root(ctx context.Context, dir string) (string, error) {
	b, err := commandOutput(ctx, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("receipt requires a Git working tree: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(b))), nil
}

// Build includes working-tree bytes, untracked nonignored files, executable
// identity, environment value hashes, and explicitly declared extra inputs.
// Environment values are never persisted in plaintext.
func Build(ctx context.Context, dir string, args, extra, ignoredEnv []string, generic bool) (Manifest, error) {
	defer observe.Measure(ctx, observe.Fingerprint)()
	if len(args) == 0 {
		return Manifest{}, fmt.Errorf("missing test command")
	}
	root, err := Root(ctx, dir)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{Version: 1, Root: root, Directory: dir, Platform: runtime.GOOS + "/" + runtime.GOARCH, Command: args, Inputs: []Input{}}
	files, err := discoverFiles(ctx, root, extra)
	if err != nil {
		return m, err
	}
	for path := range files {
		input, err := fileInput(path)
		if err != nil {
			return m, err
		}
		m.Inputs = append(m.Inputs, input)
	}
	m.Inputs = append(m.Inputs, environmentInputs(os.Environ(), ignoredEnv)...)
	inputs, err := executionInputs(ctx, dir, args, generic)
	if err != nil {
		return m, err
	}
	m.Inputs = append(m.Inputs, inputs...)
	sort.Slice(m.Inputs, func(i, j int) bool {
		if m.Inputs[i].Kind != m.Inputs[j].Kind {
			return m.Inputs[i].Kind < m.Inputs[j].Kind
		}
		return m.Inputs[i].Name < m.Inputs[j].Name
	})
	return m, nil
}

func discoverFiles(ctx context.Context, root string, extra []string) (map[string]bool, error) {
	b, err := commandOutput(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	// Configuration changes affect which inputs are declared, even if ignored.
	if _, e := os.Stat(filepath.Join(root, ".receipt.json")); e == nil {
		files[filepath.Join(root, ".receipt.json")] = true
	}
	for _, name := range strings.Split(string(b), "\x00") {
		if name == "" || name == ".receipt" || strings.HasPrefix(name, ".receipt/") {
			continue
		}
		files[filepath.Join(root, name)] = true
	}
	// Include .gitignore itself even when the repository has not been committed.
	for _, name := range extra {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		info, e := os.Stat(p)
		if e != nil {
			return nil, fmt.Errorf("extra input %s: %w", name, e)
		}
		if info.IsDir() {
			e = filepath.WalkDir(p, func(path string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if !d.IsDir() {
					files[path] = true
				}
				return nil
			})
			if e != nil {
				return nil, e
			}
		} else {
			files[p] = true
		}
	}

	return files, nil
}

func fileInput(path string) (Input, error) {
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return Input{FileInput, path, Hash([]byte("deleted"))}, nil
	}
	if e != nil {
		return Input{}, e
	}
	if info.IsDir() {
		return Input{}, fmt.Errorf("submodules are unsupported: %s", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Input{}, fmt.Errorf("symlink inputs require explicit resolution; refusing cache: %s", path)
	}
	if !info.Mode().IsRegular() {
		return Input{}, fmt.Errorf("nonregular input: %s", path)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return Input{}, e
	}
	return Input{FileInput, path, Hash(append([]byte(fmt.Sprintf("%o\x00", info.Mode().Perm())), data...))}, nil
}

func environmentInputs(env, ignored []string) []Input {
	skip := map[string]bool{}
	for _, name := range ignored {
		skip[name] = true
	}
	var inputs []Input
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && !skip[name] {
			inputs = append(inputs, Input{EnvironmentInput, name, Hash([]byte(value))})
		}
	}
	return inputs
}

func executionInputs(ctx context.Context, dir string, args []string, generic bool) ([]Input, error) {
	exe, err := exec.LookPath(args[0])
	if err != nil {
		return nil, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return nil, err
	}
	inputs := []Input{{ExecutableInput, exe, Hash(data)}}
	if filepath.Base(args[0]) == cargoCommand {
		cargo, err := cargoInputs(ctx, dir, args)
		if err != nil {
			return nil, err
		}
		return append(inputs, cargo...), nil
	}

	probe, err := toolchainProbe(args, data, generic)
	if err != nil {
		return nil, err
	}
	if len(probe) > 0 {
		b, e := commandOutput(ctx, dir, probe...)
		if e != nil {
			return nil, e
		}
		name := "resolved-environment"
		if filepath.Base(args[0]) == "go" && args[1] == testCommand {
			b, e = stableGoEnvironment(b)
			if e != nil {
				return nil, e
			}
			name = "resolved-go-environment-v2"
		}
		inputs = append(inputs, Input{ToolchainInput, name, Hash(b)})
	}

	return inputs, nil
}

func toolchainProbe(args []string, data []byte, generic bool) ([]string, error) {
	base := filepath.Base(args[0])
	var probe []string
	const pythonProbe = `import sys,json,importlib.metadata as m; print(json.dumps({"python":sys.version,"executable":sys.executable,"packages":sorted((d.metadata.get("Name",""),d.version) for d in m.distributions())},sort_keys=True))`
	switch {
	case base == "uv" && len(args) > 2 && args[1] == "run" && args[2] == pytestCommand:
		probe = []string{args[0], "run", "--no-sync", "python", "-c", pythonProbe}
	case (base == "python" || base == "python3" || strings.HasPrefix(base, "python3.")) && len(args) > 2 && args[1] == "-m" && args[2] == pytestCommand:
		probe = []string{args[0], "-c", pythonProbe}
	case base == pytestCommand:
		line := strings.SplitN(string(data), "\n", 2)[0]
		if !strings.HasPrefix(line, "#!/") || strings.Contains(line, " ") {
			return nil, fmt.Errorf("cannot identify pytest interpreter; use python -m pytest")
		}
		probe = []string{strings.TrimPrefix(line, "#!"), "-c", pythonProbe}
	case base == "go" && len(args) > 1 && args[1] == testCommand:
		probe = []string{args[0], "env", "-json"}
	default:
		if !generic {
			return nil, fmt.Errorf("supported commands: uv run pytest, python -m pytest, pytest, go test, bounded cargo test --frozen; --allow-generic opts into a declared-input-only command")
		}
	}
	return probe, nil
}

// Diff describes changes between two declared-input manifests.
func Diff(old, next Manifest) []string {
	var changes []string
	if (old.Command == nil) != (next.Command == nil) || !slices.Equal(old.Command, next.Command) {
		changes = append(changes, "command changed")
	}
	if old.Directory != next.Directory {
		changes = append(changes, "working directory changed")
	}
	if old.Platform != next.Platform {
		changes = append(changes, "platform changed")
	}
	prev := map[string]string{}
	for _, in := range old.Inputs {
		prev[string(in.Kind)+": "+in.Name] = in.Hash
	}
	for _, in := range next.Inputs {
		n := string(in.Kind) + ": " + in.Name
		h, ok := prev[n]
		if !ok {
			changes = append(changes, "added "+n)
		} else if h != in.Hash {
			changes = append(changes, "changed "+n)
		}
		delete(prev, n)
	}
	for n := range prev {
		changes = append(changes, "removed "+n)
	}
	sort.Strings(changes)
	return changes
}
