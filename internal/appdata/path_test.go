package appdata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUsesXDGConfigHomeWhenPresent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/fastproxy-xdg")

	dir, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if dir != "/tmp/fastproxy-xdg/fastproxy" {
		t.Fatalf("Resolve() = %q, want %q", dir, "/tmp/fastproxy-xdg/fastproxy")
	}
}

func TestResolveFallsBackToDotConfigUnderHome(t *testing.T) {
	oldXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	if hadXDG {
		t.Cleanup(func() {
			_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
		})
	} else {
		t.Cleanup(func() {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		})
	}
	_ = os.Unsetenv("XDG_CONFIG_HOME")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}

	dir, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(home, ".config", "fastproxy")
	if dir != want {
		t.Fatalf("Resolve() = %q, want %q", dir, want)
	}
}

func TestResolveUsesOverrideWhenProvided(t *testing.T) {
	dir, err := Resolve("./tmp-fastproxy-data")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if !filepath.IsAbs(dir) {
		t.Fatalf("Resolve() should return absolute path, got %q", dir)
	}
	if filepath.Base(dir) != "tmp-fastproxy-data" {
		t.Fatalf("filepath.Base(dir) = %q, want %q", filepath.Base(dir), "tmp-fastproxy-data")
	}
}

func TestEnsureDoesNotCreateStaticResourceFileDirectories(t *testing.T) {
	root := t.TempDir()
	if err := Ensure(root); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "profiles"),
		filepath.Join(root, "repository", "profiles"),
		filepath.Join(root, "repository", "subscriptions"),
		filepath.Join(root, "repository", "node-sets"),
		filepath.Join(root, "repository", "routing-rule-sets"),
		filepath.Join(root, "repository", "rule-source-repositories"),
		filepath.Join(root, "repository", "sing-box-rule-sets"),
		filepath.Join(root, "repository", "mihomo-rule-providers"),
		filepath.Join(root, "repository", "group-sets"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("static resource file directory %q should not be created, stat error = %v", path, err)
		}
	}
}
