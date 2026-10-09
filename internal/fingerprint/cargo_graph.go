package fingerprint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type cargoPackage struct {
	Name     string        `json:"name"`
	ID       string        `json:"id"`
	Manifest string        `json:"manifest_path"`
	Source   *string       `json:"source"`
	Targets  []cargoTarget `json:"targets"`
}

type cargoTarget struct {
	Kind   []cargoTargetKind `json:"kind"`
	Source string            `json:"src_path"`
}

type cargoTargetKind string

const (
	cargoLib         cargoTargetKind = "lib"
	cargoRlib        cargoTargetKind = "rlib"
	cargoBin         cargoTargetKind = "bin"
	cargoTest        cargoTargetKind = "test"
	cargoExample     cargoTargetKind = "example"
	cargoBench       cargoTargetKind = "bench"
	cargoBuildScript cargoTargetKind = "custom-build"
	cargoProcMacro   cargoTargetKind = "proc-macro"
)

type cargoNode struct {
	ID   string            `json:"id"`
	Deps []cargoDependency `json:"deps"`
}

type cargoDependency struct {
	Package string                `json:"pkg"`
	Kinds   []cargoDependencyKind `json:"dep_kinds"`
}

type cargoDependencyKind struct {
	Kind cargoDependencyType `json:"kind"`
}

type cargoDependencyType string

const (
	cargoNormalDependency cargoDependencyType = ""
	cargoDevDependency    cargoDependencyType = "dev"
	cargoBuildDependency  cargoDependencyType = "build"
)

type cargoResolution struct {
	Root  string      `json:"root"`
	Nodes []cargoNode `json:"nodes"`
}

type cargoMetadata struct {
	Version   int              `json:"version"`
	Workspace string           `json:"workspace_root"`
	Packages  []cargoPackage   `json:"packages"`
	Resolve   *cargoResolution `json:"resolve"`
}

func within(root, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cargoClosure(m cargoMetadata, root string) ([]cargoPackage, error) {
	if m.Version != 1 || m.Resolve == nil || m.Resolve.Root == "" || !within(root, m.Workspace) {
		return nil, fmt.Errorf("cargo metadata requires one package in this Git workspace")
	}
	packages := map[string]cargoPackage{}
	for _, p := range m.Packages {
		if p.ID == "" || packages[p.ID].ID != "" {
			return nil, fmt.Errorf("invalid cargo package identity")
		}
		packages[p.ID] = p
	}
	nodes := map[string]cargoNode{}
	for _, n := range m.Resolve.Nodes {
		if n.ID == "" || nodes[n.ID].ID != "" {
			return nil, fmt.Errorf("invalid cargo resolution node")
		}
		nodes[n.ID] = n
	}
	seen := map[string]bool{}
	pending := []string{m.Resolve.Root}
	var result []cargoPackage
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		p, ok := packages[id]
		n, exists := nodes[id]
		if !ok || !exists {
			return nil, fmt.Errorf("incomplete cargo dependency graph")
		}
		if n.Deps == nil {
			return nil, fmt.Errorf("cargo dependency edges missing; unsupported metadata")
		}
		if err := cargoPackagePolicy(p, root); err != nil {
			return nil, err
		}
		deps, err := cargoDependencies(n, id == m.Resolve.Root)
		if err != nil {
			return nil, err
		}
		pending = append(pending, deps...)
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func cargoDependencies(n cargoNode, includeDev bool) ([]string, error) {
	var result []string
	for _, d := range n.Deps {
		if d.Package == "" || len(d.Kinds) == 0 {
			return nil, fmt.Errorf("invalid cargo dependency edge")
		}
		include := false
		for _, k := range d.Kinds {
			switch k.Kind {
			case cargoNormalDependency, cargoBuildDependency:
				include = true
			case cargoDevDependency:
				include = include || includeDev
			default:
				return nil, fmt.Errorf("unknown cargo dependency kind")
			}
		}
		if include {
			result = append(result, d.Package)
		}
	}
	return result, nil
}

func cargoPackagePolicy(p cargoPackage, root string) error {
	if !filepath.IsAbs(p.Manifest) || filepath.Base(p.Manifest) != "Cargo.toml" || len(p.Targets) == 0 {
		return fmt.Errorf("invalid cargo package manifest or targets")
	}
	if p.Source == nil && !within(root, p.Manifest) {
		return fmt.Errorf("cargo path dependency outside Git root requires uncached execution")
	}
	if p.Source != nil && !strings.HasPrefix(*p.Source, "registry+") && !strings.HasPrefix(*p.Source, "sparse+") {
		return fmt.Errorf("cargo non-registry external dependency requires uncached execution")
	}
	for _, target := range p.Targets {
		if len(target.Kind) == 0 || !within(filepath.Dir(p.Manifest), target.Source) {
			return fmt.Errorf("cargo target outside package or missing target kind")
		}
		for _, kind := range target.Kind {
			switch kind {
			case cargoLib, cargoRlib, cargoBin, cargoTest, cargoExample, cargoBench:
			case cargoBuildScript, cargoProcMacro:
				return fmt.Errorf("cargo package %q uses %s; build scripts and procedural macros require uncached execution", cargoDiagnosticName(p.Name), kind)
			default:
				return fmt.Errorf("unsupported cargo target kind")
			}
		}
	}
	return nil
}

// Do not print package IDs: they can contain private registry URLs. Accept only
// a bounded crate-name alphabet; never echo arbitrary metadata into diagnostics.
func cargoDiagnosticName(name string) string {
	if name == "" || len(name) > 64 {
		return "<unnamed>"
	}
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return "<unnamed>"
	}
	return name
}
