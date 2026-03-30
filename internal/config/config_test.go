package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.DefaultCollection != "" {
		t.Fatalf("expected empty default collection, got %q", cfg.DefaultCollection)
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := &Config{DefaultCollection: "my-docs"}

	if err := Save(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.DefaultCollection != original.DefaultCollection {
		t.Fatalf("expected %q, got %q", original.DefaultCollection, loaded.DefaultCollection)
	}
}

func TestResolvePathExplicitFirst(t *testing.T) {
	explicit := "/tmp/explicit.yaml"
	t.Setenv("BMGREP_CONFIG", "/tmp/env.yaml")

	got, err := ResolvePath(explicit)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != explicit {
		t.Fatalf("expected explicit path %q, got %q", explicit, got)
	}
}

func TestResolvePathEnvFallback(t *testing.T) {
	envPath := "/tmp/env-config.yaml"
	t.Setenv("BMGREP_CONFIG", envPath)

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != envPath {
		t.Fatalf("expected env path %q, got %q", envPath, got)
	}
}

func TestResolvePathDefault(t *testing.T) {
	t.Setenv("BMGREP_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := ResolvePath("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "bmgrep", "config.yaml")
	if got != want {
		t.Fatalf("expected default path %q, got %q", want, got)
	}
}
