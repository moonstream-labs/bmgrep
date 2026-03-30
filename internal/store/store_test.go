package store

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
)

func TestTermIDFWeightsScopedToCollection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c1, err := s.CreateCollection("c1", filepath.Join(root, "c1"), filepath.Join(root, "c1", ".bmgrepignore"))
	if err != nil {
		t.Fatalf("create c1: %v", err)
	}
	c2, err := s.CreateCollection("c2", filepath.Join(root, "c2"), filepath.Join(root, "c2", ".bmgrepignore"))
	if err != nil {
		t.Fatalf("create c2: %v", err)
	}

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c1.ID,
		Path:         filepath.Join(root, "c1", "a.md"),
		RelPath:      "a.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha beta\n",
		CleanContent: "alpha beta\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c1.ID,
		Path:         filepath.Join(root, "c1", "b.md"),
		RelPath:      "b.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "beta\n",
		CleanContent: "beta\n",
	})

	// Populate a second collection with extra "alpha" documents. These must
	// not affect IDF calculation for c1.
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c2.ID,
		Path:         filepath.Join(root, "c2", "x.md"),
		RelPath:      "x.md",
		FileHash:     "h3",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c2.ID,
		Path:         filepath.Join(root, "c2", "y.md"),
		RelPath:      "y.md",
		FileHash:     "h4",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c2.ID,
		Path:         filepath.Join(root, "c2", "z.md"),
		RelPath:      "z.md",
		FileHash:     "h5",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})

	weights, err := s.TermIDFWeights(c1.ID, []string{"alpha"})
	if err != nil {
		t.Fatalf("TermIDFWeights: %v", err)
	}

	got := weights["alpha"]
	want := math.Log((2.0-1.0+0.5)/(1.0+0.5) + 1.0)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected IDF weight: got %.12f, want %.12f", got, want)
	}
	if got <= 0 {
		t.Fatalf("expected positive IDF for alpha in c1, got %.12f", got)
	}
}

func mustUpsertDoc(t *testing.T, s *Store, doc DocumentRecord) {
	t.Helper()
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		return s.UpsertDocument(tx, doc)
	})
	if err != nil {
		t.Fatalf("upsert %s: %v", doc.Path, err)
	}
}
