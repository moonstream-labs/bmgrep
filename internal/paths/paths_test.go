package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPathTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	got, err := ExpandPath("~/docs")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "docs")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestExpandPathAbsolute(t *testing.T) {
	got, err := ExpandPath("/tmp/test")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/test" {
		t.Fatalf("expected /tmp/test, got %q", got)
	}
}

func TestExpandPathRelative(t *testing.T) {
	got, err := ExpandPath("relative/path")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestDefaultConfigPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/custom/config/bmgrep/config.yaml"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultConfigPathFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "bmgrep", "config.yaml")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultDBPathXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")

	got, err := DefaultDBPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/custom/data/bmgrep/bmgrep.db"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultDBPathFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")

	got, err := DefaultDBPath()
	if err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "bmgrep", "bmgrep.db")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
