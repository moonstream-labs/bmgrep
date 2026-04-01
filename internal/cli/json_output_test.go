package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

func TestCollectionListJSONOutputEmpty(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	app := &App{Store: s}
	cmd := newCollectionCmd(app)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"list", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute collection list --json: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr output: %q", stderr.String())
	}

	var got collectionListJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal list json: %v\noutput=%q", err, stdout.String())
	}
	if got.Collections == nil {
		t.Fatalf("expected collections to be [], got null")
	}
	if len(got.Collections) != 0 {
		t.Fatalf("expected 0 collections, got %d", len(got.Collections))
	}
}

func TestCollectionListJSONOutputValues(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	root := t.TempDir()
	alphaRoot := filepath.Join(root, "alpha")
	alphaIgnore := filepath.Join(alphaRoot, ".bmignore")
	betaRoot := filepath.Join(root, "beta")
	betaIgnore := filepath.Join(betaRoot, ".bmignore")

	alpha, err := s.CreateCollection("alpha", alphaRoot, alphaIgnore)
	if err != nil {
		t.Fatalf("create alpha collection: %v", err)
	}
	beta, err := s.CreateCollection("beta", betaRoot, betaIgnore)
	if err != nil {
		t.Fatalf("create beta collection: %v", err)
	}
	if err := s.SetDefaultCollectionByName(beta.Name); err != nil {
		t.Fatalf("set default collection: %v", err)
	}

	upsertTestDoc(t, s, store.DocumentRecord{
		CollectionID: alpha.ID,
		Path:         filepath.Join(alphaRoot, "a.md"),
		RelPath:      "a.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	upsertTestDoc(t, s, store.DocumentRecord{
		CollectionID: beta.ID,
		Path:         filepath.Join(betaRoot, "b.md"),
		RelPath:      "b.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "beta\n",
		CleanContent: "beta\n",
	})
	upsertTestDoc(t, s, store.DocumentRecord{
		CollectionID: beta.ID,
		Path:         filepath.Join(betaRoot, "c.md"),
		RelPath:      "c.md",
		FileHash:     "h3",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "gamma\n",
		CleanContent: "gamma\n",
	})

	app := &App{Store: s}
	cmd := newCollectionCmd(app)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute collection list --json: %v", err)
	}

	var got collectionListJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal list json: %v\noutput=%q", err, stdout.String())
	}
	if len(got.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(got.Collections))
	}

	byName := make(map[string]collectionSummaryJSON, len(got.Collections))
	for _, collection := range got.Collections {
		byName[collection.Name] = collection
	}

	alphaJSON, ok := byName["alpha"]
	if !ok {
		t.Fatalf("missing alpha collection in json output")
	}
	if alphaJSON.RootPath != alphaRoot {
		t.Fatalf("alpha root_path mismatch: got %q want %q", alphaJSON.RootPath, alphaRoot)
	}
	if alphaJSON.DocumentCount != 1 {
		t.Fatalf("alpha document_count mismatch: got %d want 1", alphaJSON.DocumentCount)
	}
	if alphaJSON.IsDefault {
		t.Fatalf("alpha should not be default")
	}

	betaJSON, ok := byName["beta"]
	if !ok {
		t.Fatalf("missing beta collection in json output")
	}
	if betaJSON.RootPath != betaRoot {
		t.Fatalf("beta root_path mismatch: got %q want %q", betaJSON.RootPath, betaRoot)
	}
	if betaJSON.DocumentCount != 2 {
		t.Fatalf("beta document_count mismatch: got %d want 2", betaJSON.DocumentCount)
	}
	if !betaJSON.IsDefault {
		t.Fatalf("beta should be default")
	}
}

func TestCollectionSourcesJSONOutput(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	root := t.TempDir()
	rootPath := filepath.Join(root, "docs")
	ignorePath := filepath.Join(rootPath, ".bmignore")
	collection, err := s.CreateCollection("docs", rootPath, ignorePath)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	fileSourcePath := filepath.Join(root, "note.md")
	if _, err := s.AddCollectionSource(collection.ID, store.SourceTypeFile, fileSourcePath, ""); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	app := &App{Store: s}
	cmd := newCollectionCmd(app)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sources", "--json", "docs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute collection sources --json: %v", err)
	}

	var got collectionSourcesJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal sources json: %v\noutput=%q", err, stdout.String())
	}
	if got.Collection != "docs" {
		t.Fatalf("collection mismatch: got %q want docs", got.Collection)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(got.Sources))
	}

	first := got.Sources[0]
	if first.Type != store.SourceTypeDirectory {
		t.Fatalf("first source type mismatch: got %q want %q", first.Type, store.SourceTypeDirectory)
	}
	if first.Path != rootPath {
		t.Fatalf("first source path mismatch: got %q want %q", first.Path, rootPath)
	}
	if first.IgnoreFile != ignorePath {
		t.Fatalf("first source ignore_file mismatch: got %q want %q", first.IgnoreFile, ignorePath)
	}
	if !first.Enabled {
		t.Fatalf("first source expected enabled=true")
	}

	second := got.Sources[1]
	if second.Type != store.SourceTypeFile {
		t.Fatalf("second source type mismatch: got %q want %q", second.Type, store.SourceTypeFile)
	}
	if second.Path != fileSourcePath {
		t.Fatalf("second source path mismatch: got %q want %q", second.Path, fileSourcePath)
	}
	if second.IgnoreFile != "" {
		t.Fatalf("second source ignore_file mismatch: got %q want empty", second.IgnoreFile)
	}
	if !second.Enabled {
		t.Fatalf("second source expected enabled=true")
	}
}

func TestCollectionSourcesJSONOutputEmpty(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	root := t.TempDir()
	rootPath := filepath.Join(root, "docs")
	ignorePath := filepath.Join(rootPath, ".bmignore")
	collection, err := s.CreateCollection("docs", rootPath, ignorePath)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	sources, err := s.ListCollectionSources(collection.ID)
	if err != nil {
		t.Fatalf("list collection sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 initial source, got %d", len(sources))
	}
	if _, err := s.RemoveCollectionSourceByID(collection.ID, sources[0].ID); err != nil {
		t.Fatalf("remove initial source: %v", err)
	}

	app := &App{Store: s}
	cmd := newCollectionCmd(app)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sources", "--json", "docs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute collection sources --json: %v", err)
	}

	var got collectionSourcesJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal sources json: %v\noutput=%q", err, stdout.String())
	}
	if got.Sources == nil {
		t.Fatalf("expected sources to be [], got null")
	}
	if len(got.Sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(got.Sources))
	}
}

func TestDBCurrentJSONOutput(t *testing.T) {
	flagDB := filepath.Join(t.TempDir(), "override.db")
	app := &App{}
	cmd := newDBCmd(app, &flagDB)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"current", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute db current --json: %v", err)
	}

	var got dbCurrentJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal db current json: %v\noutput=%q", err, stdout.String())
	}
	if got.DBPath != flagDB {
		t.Fatalf("db_path mismatch: got %q want %q", got.DBPath, flagDB)
	}
	if got.DBSource != dbSourceFlag {
		t.Fatalf("db_source mismatch: got %q want %q", got.DBSource, dbSourceFlag)
	}
	if got.Workspace != "" {
		t.Fatalf("workspace mismatch: got %q want empty", got.Workspace)
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

func upsertTestDoc(t *testing.T, s *store.Store, doc store.DocumentRecord) {
	t.Helper()
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		return s.UpsertDocument(tx, doc)
	})
	if err != nil {
		t.Fatalf("upsert document: %v", err)
	}
}
