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

func TestCollectionSearchIndexSchemaIncludesTitleColumn(t *testing.T) {
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

	cols, err := collectionFTSColumns(s, c.ID)
	if err != nil {
		t.Fatalf("collectionFTSColumns: %v", err)
	}
	if !containsString(cols, "title") {
		t.Fatalf("expected title column in collection FTS schema, got %v", cols)
	}
	if !containsString(cols, "clean_content") {
		t.Fatalf("expected clean_content column in collection FTS schema, got %v", cols)
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

func TestOpenMigratesLegacyCollectionFTSShardToTwoColumns(t *testing.T) {
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

	docPath := filepath.Join(root, "docs", "pattern-syntax.md")
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         docPath,
		RelPath:      "pattern-syntax.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    5,
		RawContent:   "---\ntitle: Pattern Syntax\n---\n\nUnrelated body.\n",
		CleanContent: "Unrelated body.\n",
	})

	if err := s.RunInTransaction(func(tx *sql.Tx) error {
		if err := s.DropCollectionSearchIndexTx(tx, c.ID); err != nil {
			return err
		}

		_, ftsIdent, _, vocabIdent, err := collectionSearchIndexIdents(c.ID)
		if err != nil {
			return err
		}

		legacyFTS := fmt.Sprintf(`
			CREATE VIRTUAL TABLE %s USING fts5(
				clean_content,
				tokenize='unicode61'
			)
		`, ftsIdent)
		if _, err := tx.Exec(legacyFTS); err != nil {
			return err
		}

		legacyVocab := fmt.Sprintf(`
			CREATE VIRTUAL TABLE %s USING fts5vocab(%s, 'instance')
		`, vocabIdent, ftsIdent)
		if _, err := tx.Exec(legacyVocab); err != nil {
			return err
		}

		legacyPopulate := fmt.Sprintf(`
			INSERT INTO %s(rowid, clean_content)
			SELECT id, clean_content
			FROM documents
			WHERE collection_id = ?
		`, ftsIdent)
		if _, err := tx.Exec(legacyPopulate, c.ID); err != nil {
			return err
		}

		return nil
	}); err != nil {
		t.Fatalf("seed legacy one-column collection fts shard: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()

	cols, err := collectionFTSColumns(s2, c.ID)
	if err != nil {
		t.Fatalf("collectionFTSColumns: %v", err)
	}
	if !containsString(cols, "title") {
		t.Fatalf("expected migrated shard to include title column, got %v", cols)
	}

	ranked, total, err := s2.SearchRankedDocs(c.ID, "pattern syntax", 5)
	if err != nil {
		t.Fatalf("SearchRankedDocs after shard migration: %v", err)
	}
	if total != 1 || len(ranked) != 1 {
		t.Fatalf("expected one title-only match after shard migration, total=%d len=%d", total, len(ranked))
	}
	if ranked[0].Path != docPath {
		t.Fatalf("unexpected ranked path: got %q want %q", ranked[0].Path, docPath)
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

func TestSearchRankedDocsWithTermsCoverageForAnyQuery(t *testing.T) {
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

	docsRoot := filepath.Join(root, "docs")
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(docsRoot, "alpha.md"),
		RelPath:      "alpha.md",
		FileHash:     "h-alpha",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(docsRoot, "beta.md"),
		RelPath:      "beta.md",
		FileHash:     "h-beta",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "beta\n",
		CleanContent: "beta\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         filepath.Join(docsRoot, "both.md"),
		RelPath:      "both.md",
		FileHash:     "h-both",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha beta\n",
		CleanContent: "alpha beta\n",
	})

	if err := rebuildCollectionIndex(t, s, c.ID); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}

	queryTerms := []string{"alpha", "beta"}
	ranked, total, err := s.SearchRankedDocsWithTerms(c.ID, "alpha OR beta", queryTerms, 10, true)
	if err != nil {
		t.Fatalf("SearchRankedDocsWithTerms: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked docs, got %d", len(ranked))
	}

	byPath := make(map[string]RankedDoc, len(ranked))
	for _, d := range ranked {
		byPath[d.Path] = d
	}

	alphaDoc, ok := byPath[filepath.Join(docsRoot, "alpha.md")]
	if !ok {
		t.Fatalf("missing alpha.md in ranked results")
	}
	if alphaDoc.MatchedTerms != 1 {
		t.Fatalf("alpha.md matched terms: got %d, want 1", alphaDoc.MatchedTerms)
	}

	betaDoc, ok := byPath[filepath.Join(docsRoot, "beta.md")]
	if !ok {
		t.Fatalf("missing beta.md in ranked results")
	}
	if betaDoc.MatchedTerms != 1 {
		t.Fatalf("beta.md matched terms: got %d, want 1", betaDoc.MatchedTerms)
	}

	bothDoc, ok := byPath[filepath.Join(docsRoot, "both.md")]
	if !ok {
		t.Fatalf("missing both.md in ranked results")
	}
	if bothDoc.MatchedTerms != 2 {
		t.Fatalf("both.md matched terms: got %d, want 2", bothDoc.MatchedTerms)
	}
}

func TestGetRawContentByDocIDsScopedToCollection(t *testing.T) {
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

	c1Path := filepath.Join(root, "c1", "a.md")
	c2Path := filepath.Join(root, "c2", "b.md")

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c1.ID,
		Path:         c1Path,
		RelPath:      "a.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "c1-raw\n",
		CleanContent: "c1-raw\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c2.ID,
		Path:         c2Path,
		RelPath:      "b.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "c2-raw\n",
		CleanContent: "c2-raw\n",
	})

	c1Docs, err := s.ListDocumentsForCollection(c1.ID)
	if err != nil {
		t.Fatalf("ListDocumentsForCollection(c1): %v", err)
	}
	c2Docs, err := s.ListDocumentsForCollection(c2.ID)
	if err != nil {
		t.Fatalf("ListDocumentsForCollection(c2): %v", err)
	}

	c1Doc, ok := c1Docs[c1Path]
	if !ok {
		t.Fatalf("missing c1 document %q", c1Path)
	}
	c2Doc, ok := c2Docs[c2Path]
	if !ok {
		t.Fatalf("missing c2 document %q", c2Path)
	}

	rawByID, err := s.GetRawContentByDocIDs(c1.ID, []int64{c1Doc.ID, c2Doc.ID})
	if err != nil {
		t.Fatalf("GetRawContentByDocIDs: %v", err)
	}

	if len(rawByID) != 1 {
		t.Fatalf("expected 1 scoped row, got %d", len(rawByID))
	}
	if rawByID[c1Doc.ID] != "c1-raw\n" {
		t.Fatalf("unexpected raw content for c1 doc: %q", rawByID[c1Doc.ID])
	}
	if _, ok := rawByID[c2Doc.ID]; ok {
		t.Fatalf("unexpected raw content returned for doc outside collection scope")
	}

	empty, err := s.GetRawContentByDocIDs(c1.ID, nil)
	if err != nil {
		t.Fatalf("GetRawContentByDocIDs(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty result for nil ids, got %d", len(empty))
	}
}

func TestSearchRankedDocsUsesTitleWeightedBM25(t *testing.T) {
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

	titlePath := filepath.Join(root, "docs", "pattern-syntax.md")
	bodyPath := filepath.Join(root, "docs", "tutorial.md")

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         titlePath,
		RelPath:      "pattern-syntax.md",
		FileHash:     "h-title",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    6,
		RawContent:   "---\ntitle: Pattern Syntax\n---\n\nReference overview.\n",
		CleanContent: "Reference overview.\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         bodyPath,
		RelPath:      "tutorial.md",
		FileHash:     "h-body",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "pattern syntax pattern syntax pattern syntax pattern syntax\n",
		CleanContent: "pattern syntax pattern syntax pattern syntax pattern syntax\n",
	})

	if err := rebuildCollectionIndex(t, s, c.ID); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}

	ranked, total, err := s.SearchRankedDocs(c.ID, "pattern syntax", 2)
	if err != nil {
		t.Fatalf("SearchRankedDocs: %v", err)
	}
	if total != 2 || len(ranked) != 2 {
		t.Fatalf("unexpected ranked result size: total=%d len=%d", total, len(ranked))
	}
	if ranked[0].Path != titlePath {
		t.Fatalf("expected title-matching doc first, got %q", ranked[0].Path)
	}

	rankedWithTerms, _, err := s.SearchRankedDocsWithTerms(c.ID, "pattern syntax", []string{"pattern", "syntax"}, 2, true)
	if err != nil {
		t.Fatalf("SearchRankedDocsWithTerms: %v", err)
	}

	var titleDoc RankedDoc
	found := false
	for _, doc := range rankedWithTerms {
		if doc.Path == titlePath {
			titleDoc = doc
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("title doc missing from ranked-with-terms results")
	}
	if titleDoc.MatchedTerms != 2 {
		t.Fatalf("expected title doc to cover 2 terms, got %d", titleDoc.MatchedTerms)
	}
	if titleDoc.Matches < 2 {
		t.Fatalf("expected title doc to have at least 2 term hits from title, got %d", titleDoc.Matches)
	}
}

func TestSearchSampleDocsUsesTitleWeightedBM25(t *testing.T) {
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

	titlePath := filepath.Join(root, "docs", "pattern-syntax.md")
	bodyPath := filepath.Join(root, "docs", "tutorial.md")

	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         titlePath,
		RelPath:      "pattern-syntax.md",
		FileHash:     "h-title",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    6,
		RawContent:   "---\ntitle: Pattern Syntax\n---\n\nReference overview only.\n",
		CleanContent: "Reference overview only.\n",
	})
	mustUpsertDoc(t, s, DocumentRecord{
		CollectionID: c.ID,
		Path:         bodyPath,
		RelPath:      "tutorial.md",
		FileHash:     "h-body",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "pattern syntax pattern syntax pattern syntax pattern syntax\n",
		CleanContent: "pattern syntax pattern syntax pattern syntax pattern syntax\n",
	})

	if err := rebuildCollectionIndex(t, s, c.ID); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}

	docs, total, err := s.SearchSampleDocs(c.ID, "pattern syntax", 2)
	if err != nil {
		t.Fatalf("SearchSampleDocs: %v", err)
	}
	if total != 2 || len(docs) != 2 {
		t.Fatalf("unexpected sample result size: total=%d len=%d", total, len(docs))
	}
	if docs[0].Path != titlePath {
		t.Fatalf("expected title-matching doc first in sample docs, got %q", docs[0].Path)
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

func collectionFTSColumns(s *Store, collectionID int64) ([]string, error) {
	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`PRAGMA table_info(%s)`, ftsIdent)
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
