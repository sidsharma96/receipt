package fingerprint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sidsharma96/receipt/internal/observe"
)

func cargoPolicy(dir string) (string, error) {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if value == "" {
			continue
		}
		switch name {
		case "CARGO_HOME", "CARGO_TARGET_DIR", "CARGO_TERM_COLOR":
			continue
		case "RUSTC", "RUSTDOC", "RUSTFLAGS", "RUSTDOCFLAGS", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER":
			return "", fmt.Errorf("cargo compiler override %s requires uncached execution; unset it to use the supported toolchain policy", name)
		}
		if strings.HasPrefix(name, "CARGO_") {
			return "", fmt.Errorf("cargo environment override outside supported policy")
		}
	}
	home := os.Getenv("CARGO_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(user, ".cargo")
	}
	if !filepath.IsAbs(home) {
		home = filepath.Join(dir, home)
	}
	for path := dir; ; path = filepath.Dir(path) {
		if err := noCargoConfig(filepath.Join(path, ".cargo")); err != nil {
			return "", err
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	if err := noCargoConfig(home); err != nil {
		return "", err
	}
	return home, nil
}

func noCargoConfig(dir string) error {
	for _, name := range []string{"config", "config.toml"} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if err == nil {
			return fmt.Errorf("cargo config files require uncached execution")
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func cargoTools(ctx context.Context, dir, cargo string) ([]Input, string, error) {
	defer observe.Measure(ctx, observe.CargoTools)()
	rustup, err := exec.LookPath("rustup")
	if err != nil {
		return nil, "", fmt.Errorf("cargo adapter requires an installed rustup-managed toolchain")
	}
	shim, err := os.Stat(rustup)
	if err != nil {
		return nil, "", err
	}
	var inputs []Input
	host := ""
	for _, tool := range []string{cargoCommand, "rustc", "rustdoc"} {
		resolved, err := cargoOutput(ctx, dir, rustup, "which", tool)
		if err != nil {
			return nil, "", fmt.Errorf("cannot resolve installed %s with rustup; check toolchain installation: %w", tool, err)
		}
		path := strings.TrimSpace(string(resolved))
		if !filepath.IsAbs(path) {
			return nil, "", fmt.Errorf("rustup returned a nonabsolute tool path")
		}
		selected := tool
		if tool == cargoCommand {
			selected = cargo
		}
		if err := checkCargoTool(selected, path, shim); err != nil {
			return nil, "", err
		}
		input, err := fileInput(path)
		if err != nil {
			return nil, "", err
		}
		input.Kind = ToolchainInput
		input.Name = "cargo-tool-v1:" + tool + ":" + path
		inputs = append(inputs, input)
		version, err := cargoOutput(ctx, dir, selected, "-vV")
		if err != nil {
			return nil, "", fmt.Errorf("cannot query installed %s version: %w", tool, err)
		}
		inputs = append(inputs, Input{ToolchainInput, "cargo-version-v1:" + tool, Hash(version)})
		if tool == "rustc" {
			for _, line := range strings.Split(string(version), "\n") {
				if strings.HasPrefix(line, "host: ") {
					host = strings.TrimPrefix(line, "host: ")
				}
			}
		}
	}
	if host == "" {
		return nil, "", fmt.Errorf("rustc host missing")
	}
	return inputs, host, nil
}

func checkCargoTool(selected, resolved string, shim os.FileInfo) error {
	path, err := exec.LookPath(selected)
	if err != nil {
		return err
	}
	actual, err := os.Stat(path)
	if err != nil {
		return err
	}
	installed, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !os.SameFile(actual, shim) && !os.SameFile(actual, installed) {
		return fmt.Errorf("cargo tool selection differs from rustup; running uncached")
	}
	return nil
}
