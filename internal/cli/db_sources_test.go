package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

func TestDBSourcesJSONOutput(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	root := t.TempDir()
	collection, err := s.CreateCollection("docs", filepath.Join(root, "docs"), filepath.Join(root, "docs", ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	fileSourcePath := filepath.Join(root, "single.md")
	if _, err := s.AddCollectionSource(collection.ID, store.SourceTypeFile, fileSourcePath, ""); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	app := &App{}
	flagDB := dbPath
	cmd := newDBCmd(app, &flagDB)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sources", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute db sources --json: %v", err)
	}

	var got dbSourcesJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal db sources json: %v\noutput=%q", err, stdout.String())
	}

	if got.CWD == "" {
		t.Fatalf("expected cwd to be populated")
	}
	if got.DBPath != dbPath {
		t.Fatalf("db_path mismatch: got %q want %q", got.DBPath, dbPath)
	}
	if got.Filters.Sort != "added" || !got.Filters.Desc {
		t.Fatalf("unexpected default filters: %+v", got.Filters)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(got.Sources))
	}
}

func TestDBSourcesJSONOutputWithStats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bmgrep.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	root := t.TempDir()
	dirRoot := filepath.Join(root, "docs")
	collection, err := s.CreateCollection("docs", dirRoot, filepath.Join(dirRoot, ".bmignore"))
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}

	fileSourcePath := filepath.Join(root, "single.md")
	if _, err := s.AddCollectionSource(collection.ID, store.SourceTypeFile, fileSourcePath, ""); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	upsertTestDoc(t, s, store.DocumentRecord{
		CollectionID: collection.ID,
		Path:         filepath.Join(dirRoot, "a.md"),
		RelPath:      "a.md",
		FileHash:     "h1",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "alpha\n",
		CleanContent: "alpha\n",
	})
	upsertTestDoc(t, s, store.DocumentRecord{
		CollectionID: collection.ID,
		Path:         fileSourcePath,
		RelPath:      "single.md",
		FileHash:     "h2",
		MTimeNS:      1,
		SizeBytes:    1,
		LineCount:    1,
		RawContent:   "beta\n",
		CleanContent: "beta\n",
	})

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	app := &App{}
	flagDB := dbPath
	cmd := newDBCmd(app, &flagDB)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sources", "--json", "--with-stats", "--collection", "docs", "--sort", "path", "--desc=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute db sources --json --with-stats: %v", err)
	}

	var got dbSourcesJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal db sources json: %v\noutput=%q", err, stdout.String())
	}

	if !got.Filters.WithStats {
		t.Fatalf("expected with_stats=true in filters")
	}
	if len(got.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(got.Sources))
	}

	for _, source := range got.Sources {
		if source.IndexedDocs == nil {
			t.Fatalf("expected indexed_docs field for source %+v", source)
		}
		if *source.IndexedDocs != 1 {
			t.Fatalf("expected indexed_docs=1, got %d for source %q", *source.IndexedDocs, source.Path)
		}
		if source.LatestIndexedAt == "" {
			t.Fatalf("expected latest_indexed_at for source %q", source.Path)
		}
	}
}
