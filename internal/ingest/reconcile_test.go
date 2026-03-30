package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestReconcileCrossCollectionBM25Isolation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rootA := filepath.Join(tempDir, "docs-a")
	rootB := filepath.Join(tempDir, "docs-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatal(err)
	}

	a1 := filepath.Join(rootA, "a1.md")
	a2 := filepath.Join(rootA, "a2.md")
	if err := os.WriteFile(a1, []byte("alpha alpha alpha alpha beta gamma delta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a2, []byte("alpha beta beta beta beta beta beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignoreA, _ := EnsureIgnoreFile(rootA)
	ignoreB, _ := EnsureIgnoreFile(rootB)
	collectionA, err := s.CreateCollection("docs-a", rootA, ignoreA)
	if err != nil {
		t.Fatalf("create collection A: %v", err)
	}
	collectionB, err := s.CreateCollection("docs-b", rootB, ignoreB)
	if err != nil {
		t.Fatalf("create collection B: %v", err)
	}

	if _, err := ReconcileCollection(s, collectionA); err != nil {
		t.Fatalf("reconcile collection A: %v", err)
	}
	if _, err := ReconcileCollection(s, collectionB); err != nil {
		t.Fatalf("reconcile collection B initial: %v", err)
	}

	baseline, total, err := s.SearchRankedDocs(collectionA.ID, "alpha beta", 2)
	if err != nil {
		t.Fatalf("baseline ranked search: %v", err)
	}
	if total != 2 || len(baseline) != 2 {
		t.Fatalf("unexpected baseline results: total=%d len=%d", total, len(baseline))
	}

	baselinePaths := []string{baseline[0].Path, baseline[1].Path}
	if baselinePaths[0] != a2 {
		t.Fatalf("unexpected baseline top result: got %q, want %q", baselinePaths[0], a2)
	}

	for i := 0; i < 40; i++ {
		path := filepath.Join(rootB, fmt.Sprintf("beta-%02d.md", i))
		if err := os.WriteFile(path, []byte("beta\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ReconcileCollection(s, collectionB); err != nil {
		t.Fatalf("reconcile collection B with beta-heavy corpus: %v", err)
	}

	after, total, err := s.SearchRankedDocs(collectionA.ID, "alpha beta", 2)
	if err != nil {
		t.Fatalf("post-mutation ranked search: %v", err)
	}
	if total != 2 || len(after) != 2 {
		t.Fatalf("unexpected post-mutation results: total=%d len=%d", total, len(after))
	}

	afterPaths := []string{after[0].Path, after[1].Path}
	if !reflect.DeepEqual(afterPaths, baselinePaths) {
		t.Fatalf("collection A ranking changed after mutating collection B: baseline=%v after=%v", baselinePaths, afterPaths)
	}
}

func TestReconcileMultiSourceCollectionLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rootA := filepath.Join(tempDir, "source-a")
	rootB := filepath.Join(tempDir, "source-b")
	extraDir := filepath.Join(tempDir, "extra")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(rootA, "a.md"), []byte("alpha-source-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "b.md"), []byte("beta-source-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileSourcePath := filepath.Join(extraDir, "single.md")
	if err := os.WriteFile(fileSourcePath, []byte("gamma-standalone-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignoreA, err := EnsureIgnoreFile(rootA)
	if err != nil {
		t.Fatalf("EnsureIgnoreFile(rootA): %v", err)
	}
	c, err := s.CreateCollection("multi", rootA, ignoreA)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	if _, err := ReconcileCollection(s, c); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	if _, total, err := s.SearchRankedDocs(c.ID, "alpha source a", 5); err != nil {
		t.Fatalf("search alpha after initial reconcile: %v", err)
	} else if total != 1 {
		t.Fatalf("expected alpha match total 1, got %d", total)
	}
	if _, total, err := s.SearchRankedDocs(c.ID, "beta source b", 5); err != nil {
		t.Fatalf("search beta after initial reconcile: %v", err)
	} else if total != 0 {
		t.Fatalf("expected beta match total 0 before adding source, got %d", total)
	}

	ignoreB, err := EnsureIgnoreFile(rootB)
	if err != nil {
		t.Fatalf("EnsureIgnoreFile(rootB): %v", err)
	}
	if _, err := s.AddCollectionSource(c.ID, store.SourceTypeDirectory, rootB, ignoreB); err != nil {
		t.Fatalf("add directory source: %v", err)
	}
	if _, err := s.AddCollectionSource(c.ID, store.SourceTypeFile, fileSourcePath, ""); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	stats, err := ReconcileCollection(s, c)
	if err != nil {
		t.Fatalf("reconcile after adding sources: %v", err)
	}
	if stats.Added < 2 {
		t.Fatalf("expected at least two documents added after adding sources, got %+v", stats)
	}

	if _, total, err := s.SearchRankedDocs(c.ID, "beta source b", 5); err != nil {
		t.Fatalf("search beta after adding source: %v", err)
	} else if total != 1 {
		t.Fatalf("expected beta match total 1, got %d", total)
	}
	if _, total, err := s.SearchRankedDocs(c.ID, "gamma standalone file", 5); err != nil {
		t.Fatalf("search gamma after adding source: %v", err)
	} else if total != 1 {
		t.Fatalf("expected gamma match total 1, got %d", total)
	}

	removed, err := s.RemoveCollectionSourceByPath(c.ID, fileSourcePath)
	if err != nil {
		t.Fatalf("remove file source: %v", err)
	}
	if removed.SourceType != store.SourceTypeFile {
		t.Fatalf("expected removed source type file, got %q", removed.SourceType)
	}

	stats, err = ReconcileCollection(s, c)
	if err != nil {
		t.Fatalf("reconcile after removing file source: %v", err)
	}
	if stats.Deleted < 1 {
		t.Fatalf("expected at least one document deleted after removing file source, got %+v", stats)
	}

	if _, total, err := s.SearchRankedDocs(c.ID, "gamma standalone file", 5); err != nil {
		t.Fatalf("search gamma after removing source: %v", err)
	} else if total != 0 {
		t.Fatalf("expected gamma match total 0 after source removal, got %d", total)
	}
}
