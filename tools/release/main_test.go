package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivesAndChecksums(t *testing.T) {
	root := t.TempDir()
	for _, name := range documents {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	builder := func(_, binary, system, arch string) error {
		return os.WriteFile(binary, []byte(system+"/"+arch), 0o755)
	}
	destination, err := packageRelease(root, "v1.2.3", builder)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(destination, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		digest, name, _ := strings.Cut(line, "  ")
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			t.Fatal(err)
		}
		if digest != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatal("checksum mismatch")
		}
		checkArchive(t, data)
	}
	if _, err := packageRelease(root, "v1.2.3", builder); err == nil {
		t.Fatal("overwrote release")
	}
	for _, version := range []string{"../outside", "v1.2.3/../x", "v1.2.3-rc1", "1.2.3"} {
		if _, err := packageRelease(root, version, builder); err == nil {
			t.Fatal("accepted", version)
		}
	}
}

func TestFailedBuildDoesNotWriteChecksums(t *testing.T) {
	root := t.TempDir()
	_, err := packageRelease(root, "v1.2.3", func(_, _, _, _ string) error { return errors.New("build failed") })
	if err == nil {
		t.Fatal("ignored failure")
	}
	if _, err := os.Stat(filepath.Join(root, ".tools/releases/v1.2.3/SHA256SUMS")); !os.IsNotExist(err) {
		t.Fatal("published incomplete checksums")
	}
}

func checkArchive(t *testing.T, data []byte) {
	t.Helper()
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(z)
	found := map[string]bool{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		found[header.Name] = true
		if header.Name == "receipt" && header.Mode&0o111 == 0 {
			t.Fatal("binary not executable")
		}
	}
	z.Close()
	if len(found) != 5 || !found["receipt"] {
		t.Fatal(found)
	}
	for _, name := range documents {
		if !found[name] {
			t.Fatal("missing", name)
		}
	}
}
