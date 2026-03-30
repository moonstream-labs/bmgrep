package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

func TestReconcileCollectionLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("alpha beta\n"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("beta gamma\n"), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	ignorePath, err := EnsureIgnoreFile(root)
	if err != nil {
		t.Fatalf("ensure ignore file: %v", err)
	}

	collection, err := s.CreateCollection("docs", root, ignorePath)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	stats, err := ReconcileCollection(s, collection)
	if err != nil {
		t.Fatalf("reconcile #1: %v", err)
	}
	if stats.Added != 2 {
		t.Fatalf("expected 2 added files, got %+v", stats)
	}

	ranked, total, err := s.SearchRankedDocs(collection.ID, "alpha", 5)
	if err != nil {
		t.Fatalf("ranked search: %v", err)
	}
	if total != 1 || len(ranked) != 1 {
		t.Fatalf("unexpected rank results: total=%d len=%d", total, len(ranked))
	}

	if err := AppendIgnorePatterns(ignorePath, []string{"b.md"}); err != nil {
		t.Fatalf("append ignore pattern: %v", err)
	}

	stats, err = ReconcileCollection(s, collection)
	if err != nil {
		t.Fatalf("reconcile #2: %v", err)
	}
	if stats.Deleted != 1 {
		t.Fatalf("expected 1 deleted file after ignore, got %+v", stats)
	}
}

func TestReconcileDetectsContentUpdate(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignorePath, _ := EnsureIgnoreFile(root)
	collection, _ := s.CreateCollection("docs", root, ignorePath)

	stats, err := ReconcileCollection(s, collection)
	if err != nil {
		t.Fatalf("reconcile #1: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("expected 1 added, got %+v", stats)
	}

	// Modify file content
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("updated content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err = ReconcileCollection(s, collection)
	if err != nil {
		t.Fatalf("reconcile #2: %v", err)
	}
	if stats.Updated != 1 {
		t.Fatalf("expected 1 updated after content change, got %+v", stats)
	}
}

func TestReconcileDetectsFileDeletion(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "a.md")
	if err := os.WriteFile(filePath, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignorePath, _ := EnsureIgnoreFile(root)
	collection, _ := s.CreateCollection("docs", root, ignorePath)
	ReconcileCollection(s, collection)

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	stats, err := ReconcileCollection(s, collection)
	if err != nil {
		t.Fatalf("reconcile after delete: %v", err)
	}
	if stats.Deleted != 1 {
		t.Fatalf("expected 1 deleted, got %+v", stats)
	}
}

func TestReconcileSampleLineNumberFidelity(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	content := "line one\nline two\nalpha target\nline four\nline five\n"
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ignorePath, _ := EnsureIgnoreFile(root)
	collection, _ := s.CreateCollection("docs", root, ignorePath)
	ReconcileCollection(s, collection)

	docs, _, err := s.SearchSampleDocs(collection.ID, "alpha target", 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	// Verify raw content line 3 matches the source file
	lines := strings.Split(docs[0].RawContent, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	if lines[2] != "alpha target" {
		t.Fatalf("line 3 mismatch: got %q, want %q", lines[2], "alpha target")
	}
}
