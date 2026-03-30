package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDBRuntimePathFlagPrecedence(t *testing.T) {
	t.Setenv("BMGREP_DB", "/tmp/env.db")
	resolved, err := resolveDBRuntimePathFrom(t.TempDir(), "/tmp/flag.db")
	if err != nil {
		t.Fatalf("resolveDBRuntimePathFrom: %v", err)
	}
	if resolved.DBSource != dbSourceFlag {
		t.Fatalf("expected source %q, got %q", dbSourceFlag, resolved.DBSource)
	}
	if resolved.DBPath != "/tmp/flag.db" {
		t.Fatalf("expected /tmp/flag.db, got %q", resolved.DBPath)
	}
}

func TestResolveDBRuntimePathEnvPrecedence(t *testing.T) {
	t.Setenv("BMGREP_DB", "/tmp/env.db")
	resolved, err := resolveDBRuntimePathFrom(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolveDBRuntimePathFrom: %v", err)
	}
	if resolved.DBSource != dbSourceEnv {
		t.Fatalf("expected source %q, got %q", dbSourceEnv, resolved.DBSource)
	}
	if resolved.DBPath != "/tmp/env.db" {
		t.Fatalf("expected /tmp/env.db, got %q", resolved.DBPath)
	}
}

func TestResolveDBRuntimePathWorkspaceFallback(t *testing.T) {
	t.Setenv("BMGREP_DB", "")
	root := t.TempDir()
	workspace := filepath.Join(root, "proj")
	nested := filepath.Join(workspace, "deep", "child")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceDir := workspaceDirPath(workspace)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceDBPath(workspace), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveDBRuntimePathFrom(nested, "")
	if err != nil {
		t.Fatalf("resolveDBRuntimePathFrom: %v", err)
	}
	if resolved.DBSource != dbSourceWorkspace {
		t.Fatalf("expected source %q, got %q", dbSourceWorkspace, resolved.DBSource)
	}
	if resolved.DBPath != workspaceDBPath(workspace) {
		t.Fatalf("expected %q, got %q", workspaceDBPath(workspace), resolved.DBPath)
	}
	if resolved.Workspace != workspace {
		t.Fatalf("expected workspace %q, got %q", workspace, resolved.Workspace)
	}
}

func TestResolveDBRuntimePathDefaultFallback(t *testing.T) {
	t.Setenv("BMGREP_DB", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

	resolved, err := resolveDBRuntimePathFrom(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolveDBRuntimePathFrom: %v", err)
	}
	if resolved.DBSource != dbSourceDefault {
		t.Fatalf("expected source %q, got %q", dbSourceDefault, resolved.DBSource)
	}
	want := filepath.Join("/tmp/xdg-data", "bmgrep", "bmgrep.db")
	if resolved.DBPath != want {
		t.Fatalf("expected %q, got %q", want, resolved.DBPath)
	}
}
