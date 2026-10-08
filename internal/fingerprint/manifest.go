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
	"runtime"
	"sort"
	"strings"
)

type Input struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type Manifest struct {
	Version   int      `json:"version"`
	Root      string   `json:"root"`
	Directory string   `json:"directory"`
	Platform  string   `json:"platform"`
	Command   []string `json:"command"`
	Inputs    []Input  `json:"inputs"`
}

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (m Manifest) Key() string {
	m.Inputs = append([]Input(nil), m.Inputs...)
	sort.Slice(m.Inputs, func(i, j int) bool {
		if m.Inputs[i].Kind != m.Inputs[j].Kind {
			return m.Inputs[i].Kind < m.Inputs[j].Kind
		}
		return m.Inputs[i].Name < m.Inputs[j].Name
	})
	b, _ := json.Marshal(m)
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

func Root(ctx context.Context, dir string) (string, error) {
	b, err := commandOutput(ctx, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("Receipt requires a Git working tree: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(b))), nil
}

// Build includes working-tree bytes, untracked nonignored files, executable
// identity, environment value hashes, and explicitly declared extra inputs.
// Environment values are never persisted in plaintext.
func Build(ctx context.Context, dir string, args, extra, ignoredEnv []string, generic bool) (Manifest, error) {
	if len(args) == 0 {
		return Manifest{}, fmt.Errorf("missing test command")
	}
	root, err := Root(ctx, dir)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{Version: 1, Root: root, Directory: dir, Platform: runtime.GOOS + "/" + runtime.GOARCH, Command: args, Inputs: []Input{}}
	add := func(kind, name string, b []byte) { m.Inputs = append(m.Inputs, Input{kind, name, Hash(b)}) }
	b, err := commandOutput(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return m, err
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
			return m, fmt.Errorf("extra input %s: %w", name, e)
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
				return m, e
			}
		} else {
			files[p] = true
		}
	}
	for path := range files {
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			add("file", path, []byte("deleted"))
			continue
		}
		if e != nil {
			return m, e
		}
		if info.IsDir() {
			return m, fmt.Errorf("submodules are unsupported: %s", path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return m, fmt.Errorf("symlink inputs require explicit resolution; refusing cache: %s", path)
		}
		if !info.Mode().IsRegular() {
			return m, fmt.Errorf("nonregular input: %s", path)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return m, e
		}
		add("file", path, append([]byte(fmt.Sprintf("%o\x00", info.Mode().Perm())), data...))
	}
	skip := map[string]bool{}
	for _, s := range ignoredEnv {
		skip[s] = true
	}
	for _, v := range os.Environ() {
		name, value, ok := strings.Cut(v, "=")
		if ok && !skip[name] {
			add("environment", name, []byte(value))
		}
	}
	exe, err := exec.LookPath(args[0])
	if err != nil {
		return m, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return m, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return m, err
	}
	add("executable", exe, data)
	base := filepath.Base(args[0])
	var probe []string
	const pythonProbe = `import sys,json,importlib.metadata as m; print(json.dumps({"python":sys.version,"executable":sys.executable,"packages":sorted((d.metadata.get("Name",""),d.version) for d in m.distributions())},sort_keys=True))`
	switch {
	case base == "uv" && len(args) > 2 && args[1] == "run" && args[2] == "pytest":
		probe = []string{args[0], "run", "--no-sync", "python", "-c", pythonProbe}
	case (base == "python" || base == "python3" || strings.HasPrefix(base, "python3.")) && len(args) > 2 && args[1] == "-m" && args[2] == "pytest":
		probe = []string{args[0], "-c", pythonProbe}
	case base == "pytest":
		line := strings.SplitN(string(data), "\n", 2)[0]
		if !strings.HasPrefix(line, "#!/") || strings.Contains(line, " ") {
			return m, fmt.Errorf("cannot identify pytest interpreter; use python -m pytest")
		}
		probe = []string{strings.TrimPrefix(line, "#!"), "-c", pythonProbe}
	case base == "go" && len(args) > 1 && args[1] == "test":
		probe = []string{args[0], "env", "-json"}
	default:
		if !generic {
			return m, fmt.Errorf("supported commands: uv run pytest, python -m pytest, pytest, go test; --allow-generic opts into a declared-input-only command")
		}
	}
	if len(probe) > 0 {
		b, e := commandOutput(ctx, dir, probe...)
		if e != nil {
			return m, e
		}
		add("toolchain", "resolved-environment", b)
	}
	sort.Slice(m.Inputs, func(i, j int) bool {
		if m.Inputs[i].Kind != m.Inputs[j].Kind {
			return m.Inputs[i].Kind < m.Inputs[j].Kind
		}
		return m.Inputs[i].Name < m.Inputs[j].Name
	})
	return m, nil
}

func Diff(old, next Manifest) []string {
	var changes []string
	a, _ := json.Marshal(old.Command)
	b, _ := json.Marshal(next.Command)
	if string(a) != string(b) {
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
		prev[in.Kind+": "+in.Name] = in.Hash
	}
	for _, in := range next.Inputs {
		n := in.Kind + ": " + in.Name
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
