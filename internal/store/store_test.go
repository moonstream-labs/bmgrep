package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

func TestCreateCollectionEnsuresCollectionSearchIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c, err := s.CreateCollection("docs", filepath.Join(root, "docs"), filepath.Join(root, "docs", ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	exists, err := s.CollectionSearchIndexExists(c.ID)
	if err != nil {
		t.Fatalf("CollectionSearchIndexExists: %v", err)
	}
	if !exists {
		t.Fatalf("expected collection search index to exist for collection %d", c.ID)
	}

	vocabName, err := collectionVocabTableName(c.ID)
	if err != nil {
		t.Fatalf("collectionVocabTableName: %v", err)
	}
	vocabExists, err := tableExists(s, vocabName)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", vocabName, err)
	}
	if !vocabExists {
		t.Fatalf("expected collection vocab table %q to exist", vocabName)
	}
}

func TestDefaultCollectionLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if _, err := s.GetDefaultCollection(); !errors.Is(err, ErrNoDefaultCollection) {
		t.Fatalf("expected ErrNoDefaultCollection before set, got %v", err)
	}

	root := t.TempDir()
	c1, err := s.CreateCollection("docs-a", filepath.Join(root, "docs-a"), filepath.Join(root, "docs-a", ".bmignore"))
	if err != nil {
		t.Fatalf("create docs-a: %v", err)
	}
	c2, err := s.CreateCollection("docs-b", filepath.Join(root, "docs-b"), filepath.Join(root, "docs-b", ".bmignore"))
	if err != nil {
		t.Fatalf("create docs-b: %v", err)
	}

	if err := s.SetDefaultCollectionByName(c1.Name); err != nil {
		t.Fatalf("SetDefaultCollectionByName(c1): %v", err)
	}

	defaultCollection, err := s.GetDefaultCollection()
	if err != nil {
		t.Fatalf("GetDefaultCollection: %v", err)
	}
	if defaultCollection.ID != c1.ID {
		t.Fatalf("expected default %d, got %d", c1.ID, defaultCollection.ID)
	}

	collections, err := s.ListCollections()
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	seenDefault := 0
	for _, c := range collections {
		if c.IsDefault {
			seenDefault++
			if c.Name != c1.Name {
				t.Fatalf("expected default collection %q, got %q", c1.Name, c.Name)
			}
		}
	}
	if seenDefault != 1 {
		t.Fatalf("expected exactly one default collection marker, got %d", seenDefault)
	}

	if err := s.SetDefaultCollectionByName(c2.Name); err != nil {
		t.Fatalf("SetDefaultCollectionByName(c2): %v", err)
	}

	defaultCollection, err = s.GetDefaultCollection()
	if err != nil {
		t.Fatalf("GetDefaultCollection after set: %v", err)
	}
	if defaultCollection.ID != c2.ID {
		t.Fatalf("expected default %d, got %d", c2.ID, defaultCollection.ID)
	}

	if err := s.DeleteCollection(c2.Name); err != nil {
		t.Fatalf("DeleteCollection(c2): %v", err)
	}

	if _, err := s.GetDefaultCollection(); !errors.Is(err, ErrNoDefaultCollection) {
		t.Fatalf("expected ErrNoDefaultCollection after deleting default, got %v", err)
	}
}

func TestCollectionSourceLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	baseRoot := filepath.Join(root, "docs")
	c, err := s.CreateCollection("docs", baseRoot, filepath.Join(baseRoot, ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	sources, err := s.ListCollectionSources(c.ID)
	if err != nil {
		t.Fatalf("ListCollectionSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source after collection create, got %d", len(sources))
	}
	if sources[0].SourceType != SourceTypeDirectory {
		t.Fatalf("expected initial source type %q, got %q", SourceTypeDirectory, sources[0].SourceType)
	}

	dirSourcePath := filepath.Join(root, "extra")
	dirSource, err := s.AddCollectionSource(c.ID, SourceTypeDirectory, dirSourcePath, filepath.Join(dirSourcePath, ".bmignore"))
	if err != nil {
		t.Fatalf("AddCollectionSource dir: %v", err)
	}
	if dirSource.ID == 0 {
		t.Fatalf("expected non-zero source id")
	}

	fileSourcePath := filepath.Join(root, "note.md")
	fileSource, err := s.AddCollectionSource(c.ID, SourceTypeFile, fileSourcePath, "")
	if err != nil {
		t.Fatalf("AddCollectionSource file: %v", err)
	}
	if fileSource.SourceType != SourceTypeFile {
		t.Fatalf("expected source type %q, got %q", SourceTypeFile, fileSource.SourceType)
	}

	if _, err := s.AddCollectionSource(c.ID, SourceTypeFile, fileSourcePath, ""); err == nil {
		t.Fatalf("expected duplicate source add to fail")
	}

	sources, err = s.ListCollectionSources(c.ID)
	if err != nil {
		t.Fatalf("ListCollectionSources after add: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("expected 3 sources after add, got %d", len(sources))
	}

	removedByPath, err := s.RemoveCollectionSourceByPath(c.ID, fileSourcePath)
	if err != nil {
		t.Fatalf("RemoveCollectionSourceByPath: %v", err)
	}
	if removedByPath.ID != fileSource.ID {
		t.Fatalf("removed path source mismatch: got id %d want %d", removedByPath.ID, fileSource.ID)
	}

	removedByID, err := s.RemoveCollectionSourceByID(c.ID, dirSource.ID)
	if err != nil {
		t.Fatalf("RemoveCollectionSourceByID: %v", err)
	}
	if removedByID.SourcePath != dirSourcePath {
		t.Fatalf("removed id source mismatch: got %q want %q", removedByID.SourcePath, dirSourcePath)
	}

	sources, err = s.ListCollectionSources(c.ID)
	if err != nil {
		t.Fatalf("ListCollectionSources final: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 remaining source, got %d", len(sources))
	}
}

func TestUpsertAllowsDuplicateRelPathsPerCollection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c, err := s.CreateCollection("docs", filepath.Join(root, "docs"), filepath.Join(root, "docs", ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(root, "source-a", "README.md"),
		RelPath:      "README.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(root, "source-b", "README.md"),
		RelPath:      "README.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "beta\n",
		CleanContent: "beta\n",
	})

	docs, err := s.ListDocumentsForCollection(c.ID)
	if err != nil {
		t.Fatalf("ListDocumentsForCollection: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents with duplicate rel_path, got %d", len(docs))
	}
}

func TestRebuildCollectionSearchIndexBackfillsOnlyCollectionDocs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c1, err := s.CreateCollection("c1", filepath.Join(root, "c1"), filepath.Join(root, "c1", ".bmignore"))
	if err != nil {
		t.Fatalf("create c1: %v", err)
	}
	c2, err := s.CreateCollection("c2", filepath.Join(root, "c2"), filepath.Join(root, "c2", ".bmignore"))
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
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c2.ID,
		Path:         filepath.Join(root, "c2", "b.md"),
		RelPath:      "b.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})

	if err := rebuildCollectionIndex(t, s, c1.ID); err != nil {
		t.Fatalf("rebuild c1 index: %v", err)
	}

	count, err := indexRowCount(s, c1.ID)
	if err != nil {
		t.Fatalf("indexRowCount(c1): %v", err)
	}
	if count != 1 {
		t.Fatalf("expected c1 index row count 1, got %d", count)
	}

	count, err = indexRowCount(s, c2.ID)
	if err != nil {
		t.Fatalf("indexRowCount(c2): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected c2 index row count 0 before rebuild, got %d", count)
	}
}

func TestDropCollectionSearchIndexRemovesShardTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c, err := s.CreateCollection("docs", filepath.Join(root, "docs"), filepath.Join(root, "docs", ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	if err := s.RunInTransaction(func(tx *sql.Tx) error {
		return s.DropCollectionSearchIndexTx(tx, c.ID)
	}); err != nil {
		t.Fatalf("DropCollectionSearchIndexTx: %v", err)
	}

	exists, err := s.CollectionSearchIndexExists(c.ID)
	if err != nil {
		t.Fatalf("CollectionSearchIndexExists: %v", err)
	}
	if exists {
		t.Fatalf("expected collection fts table to be dropped")
	}

	vocabName, err := collectionVocabTableName(c.ID)
	if err != nil {
		t.Fatalf("collectionVocabTableName: %v", err)
	}
	vocabExists, err := tableExists(s, vocabName)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", vocabName, err)
	}
	if vocabExists {
		t.Fatalf("expected collection vocab table %q to be dropped", vocabName)
	}
}

func TestOpenMigratesAndDropsLegacyGlobalFTSArtifacts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	root := t.TempDir()
	c, err := s.CreateCollection("docs", filepath.Join(root, "docs"), filepath.Join(root, "docs", ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(root, "docs", "a.md"),
		RelPath:      "a.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})

	if err := s.RunInTransaction(func(tx *sql.Tx) error {
		if err := s.DropCollectionSearchIndexTx(tx, c.ID); err != nil {
			return err
		}

		legacy := []string{
			`CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(clean_content, content='documents', content_rowid='id', tokenize='unicode61');`,
			`CREATE TRIGGER IF NOT EXISTS documents_ai AFTER INSERT ON documents BEGIN
				INSERT INTO docs_fts(rowid, clean_content) VALUES (new.id, new.clean_content);
			END;`,
			`CREATE TRIGGER IF NOT EXISTS documents_ad AFTER DELETE ON documents BEGIN
				INSERT INTO docs_fts(docs_fts, rowid, clean_content) VALUES('delete', old.id, old.clean_content);
			END;`,
			`CREATE TRIGGER IF NOT EXISTS documents_au AFTER UPDATE ON documents BEGIN
				INSERT INTO docs_fts(docs_fts, rowid, clean_content) VALUES('delete', old.id, old.clean_content);
				INSERT INTO docs_fts(rowid, clean_content) VALUES (new.id, new.clean_content);
			END;`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS docs_vocab USING fts5vocab(docs_fts, 'instance');`,
			`INSERT INTO docs_fts(rowid, clean_content)
			 SELECT id, clean_content FROM documents WHERE collection_id = ` + fmt.Sprintf("%d", c.ID) + `;`,
		}

		for _, stmt := range legacy {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed legacy global fts artifacts: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()

	legacyTableNames := []string{"docs_fts", "docs_vocab"}
	for _, name := range legacyTableNames {
		exists, err := tableExists(s2, name)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", name, err)
		}
		if exists {
			t.Fatalf("expected legacy table %q to be dropped", name)
		}
	}

	legacyTriggerNames := []string{"documents_ai", "documents_ad", "documents_au"}
	for _, name := range legacyTriggerNames {
		exists, err := triggerExists(s2, name)
		if err != nil {
			t.Fatalf("triggerExists(%s): %v", name, err)
		}
		if exists {
			t.Fatalf("expected legacy trigger %q to be dropped", name)
		}
	}

	collectionIndexExists, err := s2.CollectionSearchIndexExists(c.ID)
	if err != nil {
		t.Fatalf("CollectionSearchIndexExists: %v", err)
	}
	if !collectionIndexExists {
		t.Fatalf("expected collection index to exist after migration")
	}

	count, err := indexRowCount(s2, c.ID)
	if err != nil {
		t.Fatalf("indexRowCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migrated collection index row count 1, got %d", count)
	}

	docs, total, err := s2.SearchRankedDocs(c.ID, "alpha", 5)
	if err != nil {
		t.Fatalf("SearchRankedDocs after migration: %v", err)
	}
	if total != 1 || len(docs) != 1 {
		t.Fatalf("unexpected ranked search after migration: total=%d len=%d", total, len(docs))
	}
}

func TestTermIDFWeightsScopedToCollection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	root := t.TempDir()
	c1, err := s.CreateCollection("c1", filepath.Join(root, "c1"), filepath.Join(root, "c1", ".bmignore"))
	if err != nil {
		t.Fatalf("create c1: %v", err)
	}
	c2, err := s.CreateCollection("c2", filepath.Join(root, "c2"), filepath.Join(root, "c2", ".bmignore"))
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

	if err := rebuildCollectionIndex(t, s, c1.ID); err != nil {
		t.Fatalf("rebuild c1 index: %v", err)
	}
	if err := rebuildCollectionIndex(t, s, c2.ID); err != nil {
		t.Fatalf("rebuild c2 index: %v", err)
	}

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

func rebuildCollectionIndex(t *testing.T, s *Store, collectionID int64) error {
	t.Helper()
	return s.RunInTransaction(func(tx *sql.Tx) error {
		return s.RebuildCollectionSearchIndexTx(tx, collectionID)
	})
}

func tableExists(s *Store, name string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = ?
		LIMIT 1
	`, name).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func triggerExists(s *Store, name string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'trigger'
		  AND name = ?
		LIMIT 1
	`, name).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func indexRowCount(s *Store, collectionID int64) (int, error) {
	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return 0, err
	}

	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, ftsIdent)
	var count int
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
