package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestInstallLocalVersionUsesProvidedCacheVersion(t *testing.T) {
	cache, err := InstallLocalVersion(
		t.TempDir(),
		repository.CoreMihomo,
		"mihomo",
		"v1.2.3",
		strings.NewReader("#!/bin/sh\n"),
	)
	if err != nil {
		t.Fatalf("InstallLocalVersion() error = %v", err)
	}
	if cache.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want v1.2.3", cache.Version)
	}
	if !strings.Contains(cache.Path, "v1.2.3") {
		t.Fatalf("Path = %q, want provided version segment", cache.Path)
	}
}

func TestInstallLocalVersionRejectsPathVersion(t *testing.T) {
	_, err := InstallLocalVersion(
		t.TempDir(),
		repository.CoreMihomo,
		"mihomo",
		"../v1.2.3",
		strings.NewReader("#!/bin/sh\n"),
	)
	if err == nil {
		t.Fatal("InstallLocalVersion() error = nil, want invalid version error")
	}
}

func TestResolveBinaryUsesSystemCommandBeforeEmbeddedBinary(t *testing.T) {
	originalLookPath := execLookPath
	defer func() {
		execLookPath = originalLookPath
	}()
	execLookPath = func(file string) (string, error) {
		return filepath.Join(t.TempDir(), file), nil
	}

	path, err := ResolveBinary(t.Context(), t.TempDir(), repository.CoreMihomo, "")
	if err != nil {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
	if !strings.HasSuffix(path, CoreBinaryName(repository.CoreMihomo)) {
		t.Fatalf("path = %q, want system mihomo path", path)
	}
}

func TestResolveBinaryInstallsEmbeddedBinaryWhenSystemCommandMissing(t *testing.T) {
	source := embeddedBinaryPath(repository.CoreMihomo)
	if source == "" {
		t.Skip("no embedded binary for current platform")
	}
	file, err := embeddedBinaryFS.Open(source)
	if err != nil {
		t.Skipf("no embedded binary fixture for current platform: %v", err)
	}
	_ = file.Close()

	originalLookPath := execLookPath
	defer func() {
		execLookPath = originalLookPath
	}()
	execLookPath = func(file string) (string, error) {
		return "", os.ErrNotExist
	}

	dataDir := t.TempDir()
	path, err := ResolveBinary(t.Context(), dataDir, repository.CoreMihomo, "")
	if err != nil {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
	expected := filepath.Join(dataDir, "cores", string(repository.CoreMihomo), "embedded", CoreBinaryName(repository.CoreMihomo))
	if path != expected {
		t.Fatalf("path = %q, want %q", path, expected)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(embedded binary) error = %v", err)
	} else if info.IsDir() || info.Size() == 0 {
		t.Fatalf("embedded binary info = %#v, want non-empty file", info)
	}
}
