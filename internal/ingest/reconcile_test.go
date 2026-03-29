package ingest

import (
	"os"
	"path/filepath"
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
