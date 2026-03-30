package dbprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathsWorkspaceFallback(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "proj")
	bmgrepDir := filepath.Join(workspace, WorkspaceDirName)
	t.Setenv("BMGREP_DB", "")
	t.Setenv("BMGREP_CONFIG", "")

	if err := ensureDir(filepath.Join(bmgrepDir, WorkspaceDBName)); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(bmgrepDir, WorkspaceConfig)); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolvePathsFrom(workspace, "", "")
	if err != nil {
		t.Fatalf("ResolvePathsFrom: %v", err)
	}

	if resolved.DBSource != SourceWorkspace {
		t.Fatalf("expected DB source %q, got %q", SourceWorkspace, resolved.DBSource)
	}
	if resolved.ConfigSource != SourceWorkspace {
		t.Fatalf("expected config source %q, got %q", SourceWorkspace, resolved.ConfigSource)
	}
	if resolved.DBPath != filepath.Join(bmgrepDir, WorkspaceDBName) {
		t.Fatalf("unexpected workspace db path: %q", resolved.DBPath)
	}
}

func TestResolvePathsFlagAndEnvPrecedence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BMGREP_DB", filepath.Join(root, "env.db"))
	t.Setenv("BMGREP_CONFIG", filepath.Join(root, "env.yaml"))

	resolved, err := ResolvePathsFrom(root, filepath.Join(root, "flag.yaml"), filepath.Join(root, "flag.db"))
	if err != nil {
		t.Fatalf("ResolvePathsFrom: %v", err)
	}

	if resolved.DBSource != SourceFlag {
		t.Fatalf("expected DB source %q, got %q", SourceFlag, resolved.DBSource)
	}
	if resolved.ConfigSource != SourceFlag {
		t.Fatalf("expected config source %q, got %q", SourceFlag, resolved.ConfigSource)
	}
}

func TestRegistryLifecycle(t *testing.T) {
	reg := &Registry{}

	entry, err := RegisterDatabase(reg, DatabaseEntry{Name: "proj", DBPath: "/tmp/proj.db", ConfigPath: "/tmp/proj.yaml", Scope: ScopeLocal})
	if err != nil {
		t.Fatalf("RegisterDatabase: %v", err)
	}
	if entry.Name != "proj" {
		t.Fatalf("expected entry name proj, got %q", entry.Name)
	}

	used, err := UseDatabase(reg, "proj")
	if err != nil {
		t.Fatalf("UseDatabase: %v", err)
	}
	if used.Name != "proj" {
		t.Fatalf("expected used profile proj, got %q", used.Name)
	}
	if reg.Active != "proj" {
		t.Fatalf("expected active profile proj, got %q", reg.Active)
	}

	removed, ok, err := UnregisterDatabase(reg, "proj")
	if err != nil {
		t.Fatalf("UnregisterDatabase: %v", err)
	}
	if !ok {
		t.Fatalf("expected unregister success")
	}
	if removed.Name != "proj" {
		t.Fatalf("expected removed profile proj, got %q", removed.Name)
	}
	if len(reg.Databases) != 0 {
		t.Fatalf("expected no databases after unregister, got %d", len(reg.Databases))
	}
	if reg.Active != "" {
		t.Fatalf("expected active cleared after unregister, got %q", reg.Active)
	}
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeEmpty(path)
}

func writeEmpty(path string) error {
	return os.WriteFile(path, []byte(""), 0o600)
}
