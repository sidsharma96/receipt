// Command release builds local release archives; it never publishes them.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

var (
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	targets        = []struct{ system, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}
	documents      = []string{"LICENSE", "NOTICE", "THIRD-PARTY-NOTICES", "README.md"}
)

func build(root, binary, system, arch string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/receipt")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+system, "GOARCH="+arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s/%s: %w", system, arch, err)
	}
	return nil
}

func addFile(archive *tar.Writer, path, name string) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive input: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat archive input: %w", err)
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("archive header: %w", err)
	}
	header.Name = name
	if err := archive.WriteHeader(header); err != nil {
		return fmt.Errorf("write archive header: %w", err)
	}
	if _, err := io.Copy(archive, file); err != nil {
		return fmt.Errorf("write archive file: %w", err)
	}
	return nil
}

func archive(root, binary, path string) (_ string, err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(file, hash))
	writer := tar.NewWriter(compressed)
	if err := addFile(writer, binary, "receipt"); err != nil {
		return "", errors.Join(err, writer.Close(), compressed.Close())
	}
	for _, name := range documents {
		if err := addFile(writer, filepath.Join(root, name), name); err != nil {
			return "", errors.Join(err, writer.Close(), compressed.Close())
		}
	}
	if err := errors.Join(writer.Close(), compressed.Close()); err != nil {
		return "", fmt.Errorf("close archive: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func packageRelease(root, version string, builder func(string, string, string, string) error) (_ string, err error) {
	if !versionPattern.MatchString(version) {
		return "", fmt.Errorf("use a release version such as v0.1.0")
	}
	parent := filepath.Join(root, ".tools", "releases")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create releases directory: %w", err)
	}
	destination := filepath.Join(parent, version)
	if err := os.Mkdir(destination, 0o755); err != nil {
		return "", fmt.Errorf("create new release (existing directories are refused): %w", err)
	}
	temp, err := os.MkdirTemp("", "receipt-release-")
	if err != nil {
		return "", fmt.Errorf("create build directory: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(temp)) }()
	binary := filepath.Join(temp, "receipt")
	var sums strings.Builder
	for _, target := range targets {
		if err := builder(root, binary, target.system, target.arch); err != nil {
			return "", err
		}
		name := fmt.Sprintf("receipt_%s_%s_%s.tar.gz", version, target.system, target.arch)
		digest, err := archive(root, binary, filepath.Join(destination, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(destination, "SHA256SUMS"), []byte(sums.String()), 0o644); err != nil {
		return "", fmt.Errorf("write checksums: %w", err)
	}
	return destination, nil
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: go run ./tools/release vX.Y.Z")
	}
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	destination, err := packageRelease(root, args[0], build)
	if err != nil {
		return err
	}
	log.Print(destination)
	return nil
}

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
