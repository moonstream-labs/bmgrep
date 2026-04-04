// Package store provides SQLite-backed persistence for bmgrep.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/moonstream-labs/bmgrep/internal/frontmatter"

	_ "modernc.org/sqlite"
)

const (
	collectionFTSTablePrefix   = "docs_fts_c"
	collectionVocabTablePrefix = "docs_vocab_c"
	titleBM25Weight            = 10.0
	bodyBM25Weight             = 1.0

	SourceTypeDirectory = "dir"
	SourceTypeFile      = "file"
)

// Store wraps a SQLite handle and exposes typed operations.
type Store struct {
	db *sql.DB
}

// Collection is the canonical metadata for a documentation collection.
type Collection struct {
	ID             int64
	Name           string
	RootPath       string
	IgnoreFilePath string
}

// CollectionSummary is used for listing collections with document counts.
type CollectionSummary struct {
	Name      string
	RootPath  string
	Documents int64
	IsDefault bool
}

// ErrNoDefaultCollection indicates the database has no persistent
// default collection configured.
var ErrNoDefaultCollection = errors.New("no default collection set")

// CollectionSource defines one filesystem source for a collection.
// source_type is either "dir" (recursive markdown scan) or "file" (single .md file).
type CollectionSource struct {
	ID             int64
	CollectionID   int64
	SourceType     string
	SourcePath     string
	IgnoreFilePath string
	Enabled        bool
}

// DBSourceQuery controls source-catalog introspection across the active DB.
type DBSourceQuery struct {
	CollectionName string
	SourceType     string
	EnabledOnly    bool
	DisabledOnly   bool
	PathPrefix     string
	SortBy         string
	Desc           bool
	IncludeStats   bool
}

// DBSourceRow is a denormalized source record for DB-level introspection.
type DBSourceRow struct {
	SourceID        int64
	CollectionID    int64
	CollectionName  string
	SourceType      string
	SourcePath      string
	IgnoreFilePath  string
	Enabled         bool
	CreatedAt       string
	UpdatedAt       string
	IndexedDocs     int64
	LatestIndexedAt string
}

// DocumentRecord tracks on-disk metadata and index payload for one file.
type DocumentRecord struct {
	ID           int64
	CollectionID int64
	Path         string
	RelPath      string
	FileHash     string
	MTimeNS      int64
	SizeBytes    int64
	LineCount    int
	RawContent   string
	CleanContent string
}

// RankedDoc is the rank-mode metadata payload.
type RankedDoc struct {
	ID           int64
	Path         string
	LineCount    int
	Matches      int
	MatchedTerms int
}

// SampleDoc is the sample-mode candidate payload.
type SampleDoc struct {
	ID         int64
	Path       string
	LineCount  int
	RawContent string
}

// Open initializes a store and applies schema migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	stmts := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize sqlite pragma %q: %w", stmt, err)
		}
	}

	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close releases database resources.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			root_path TEXT NOT NULL,
			ignore_file_path TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
			path TEXT NOT NULL,
			rel_path TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			mtime_ns INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			line_count INTEGER NOT NULL,
			raw_content TEXT NOT NULL,
			clean_content TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS collection_sources (
			id INTEGER PRIMARY KEY,
			collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
			source_type TEXT NOT NULL CHECK(source_type IN ('dir', 'file')),
			source_path TEXT NOT NULL,
			ignore_file_path TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(collection_id, source_path)
		);`,
		`CREATE TABLE IF NOT EXISTS app_state (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			default_collection_id INTEGER REFERENCES collections(id) ON DELETE SET NULL
		);`,
		`INSERT INTO app_state(id, default_collection_id)
		 VALUES(1, NULL)
		 ON CONFLICT(id) DO NOTHING;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_collection_path ON documents(collection_id, path);`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection_rel_path ON documents(collection_id, rel_path);`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection_id ON documents(collection_id);`,
		`CREATE INDEX IF NOT EXISTS idx_collection_sources_collection_id ON collection_sources(collection_id);`,
	}

	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}

	if err := s.migrateCollectionSourceSchema(); err != nil {
		return err
	}
	if err := s.migrateCollectionSourcesFromLegacyRoot(); err != nil {
		return err
	}

	if err := s.migrateLegacyGlobalFTS(); err != nil {
		return err
	}
	if err := s.migrateCollectionFTSTwoColumn(); err != nil {
		return err
	}

	return nil
}

// GetDefaultCollection resolves the persistent default collection from app_state.
func (s *Store) GetDefaultCollection() (Collection, error) {
	var (
		id             sql.NullInt64
		name           sql.NullString
		rootPath       sql.NullString
		ignoreFilePath sql.NullString
	)
	err := s.db.QueryRow(`
		SELECT c.id, c.name, c.root_path, c.ignore_file_path
		FROM app_state a
		LEFT JOIN collections c ON c.id = a.default_collection_id
		WHERE a.id = 1
	`).Scan(&id, &name, &rootPath, &ignoreFilePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Collection{}, ErrNoDefaultCollection
		}
		return Collection{}, fmt.Errorf("get default collection: %w", err)
	}
	if !id.Valid {
		return Collection{}, ErrNoDefaultCollection
	}

	c := Collection{
		ID:             id.Int64,
		Name:           name.String,
		RootPath:       rootPath.String,
		IgnoreFilePath: ignoreFilePath.String,
	}
	return c, nil
}

// SetDefaultCollectionByName persists the default collection in app_state.
func (s *Store) SetDefaultCollectionByName(name string) error {
	collection, err := s.GetCollectionByName(name)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		UPDATE app_state
		SET default_collection_id = ?
		WHERE id = 1
	`, collection.ID)
	if err != nil {
		return fmt.Errorf("set default collection: %w", err)
	}

	return nil
}

// ClearDefaultCollection removes the persistent default collection.
func (s *Store) ClearDefaultCollection() error {
	_, err := s.db.Exec(`UPDATE app_state SET default_collection_id = NULL WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("clear default collection: %w", err)
	}
	return nil
}

func (s *Store) migrateCollectionSourceSchema() error {
	var indexSQL string
	err := s.db.QueryRow(`
		SELECT sql
		FROM sqlite_master
		WHERE type = 'index'
		  AND name = 'idx_documents_collection_rel_path'
	`).Scan(&indexSQL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_documents_collection_rel_path ON documents(collection_id, rel_path);`); err != nil {
				return fmt.Errorf("create rel_path lookup index: %w", err)
			}
			return nil
		}
		return fmt.Errorf("inspect rel_path index: %w", err)
	}

	if !strings.Contains(strings.ToUpper(indexSQL), "UNIQUE") {
		return nil
	}

	// Older databases may still have a UNIQUE rel_path index, which prevents
	// aggregating similarly-named files from different source roots.
	if _, err := s.db.Exec(`DROP INDEX IF EXISTS idx_documents_collection_rel_path;`); err != nil {
		return fmt.Errorf("drop legacy rel_path unique index: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_documents_collection_rel_path ON documents(collection_id, rel_path);`); err != nil {
		return fmt.Errorf("create rel_path lookup index: %w", err)
	}
	return nil
}

func (s *Store) migrateCollectionSourcesFromLegacyRoot() error {
	_, err := s.db.Exec(`
		INSERT INTO collection_sources(collection_id, source_type, source_path, ignore_file_path, enabled)
		SELECT c.id, ?, c.root_path, c.ignore_file_path, 1
		FROM collections c
		WHERE NOT EXISTS (
			SELECT 1
			FROM collection_sources cs
			WHERE cs.collection_id = c.id
		)
	`, SourceTypeDirectory)
	if err != nil {
		return fmt.Errorf("backfill collection sources from legacy roots: %w", err)
	}

	return nil
}

func (s *Store) migrateLegacyGlobalFTS() error {
	legacy, err := s.hasLegacyGlobalFTSArtifacts()
	if err != nil {
		return err
	}
	if !legacy {
		return nil
	}

	return s.RunInTransaction(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id FROM collections ORDER BY id ASC`)
		if err != nil {
			return fmt.Errorf("list collections for legacy fts migration: %w", err)
		}
		defer rows.Close()

		var collectionIDs []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan collection id for legacy fts migration: %w", err)
			}
			collectionIDs = append(collectionIDs, id)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate collection ids for legacy fts migration: %w", err)
		}

		for _, id := range collectionIDs {
			if err := s.RebuildCollectionSearchIndexTx(tx, id); err != nil {
				return fmt.Errorf("rebuild collection index during legacy fts migration: %w", err)
			}
		}

		dropStatements := []string{
			`DROP TRIGGER IF EXISTS documents_ai;`,
			`DROP TRIGGER IF EXISTS documents_ad;`,
			`DROP TRIGGER IF EXISTS documents_au;`,
			`DROP TABLE IF EXISTS docs_vocab;`,
			`DROP TABLE IF EXISTS docs_fts;`,
		}
		for _, stmt := range dropStatements {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("drop legacy fts artifact: %w", err)
			}
		}

		return nil
	})
}

func (s *Store) migrateCollectionFTSTwoColumn() error {
	return s.RunInTransaction(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id FROM collections ORDER BY id ASC`)
		if err != nil {
			return fmt.Errorf("list collections for fts shard migration: %w", err)
		}
		defer rows.Close()

		var collectionIDs []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan collection id for fts shard migration: %w", err)
			}
			collectionIDs = append(collectionIDs, id)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate collection ids for fts shard migration: %w", err)
		}

		for _, collectionID := range collectionIDs {
			needsMigration, err := collectionFTSNeedsTwoColumnTx(tx, collectionID)
			if err != nil {
				return err
			}
			if needsMigration {
				if err := s.DropCollectionSearchIndexTx(tx, collectionID); err != nil {
					return fmt.Errorf("drop legacy collection fts shard for migration: %w", err)
				}
				if err := s.RebuildCollectionSearchIndexTx(tx, collectionID); err != nil {
					return fmt.Errorf("rebuild collection fts shard for migration: %w", err)
				}
				continue
			}

			if err := s.EnsureCollectionSearchIndexTx(tx, collectionID); err != nil {
				return err
			}
		}

		return nil
	})
}

func collectionFTSNeedsTwoColumnTx(tx *sql.Tx, collectionID int64) (bool, error) {
	ftsName, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return false, err
	}

	var exists int
	err = tx.QueryRow(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = ?
		LIMIT 1
	`, ftsName).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("check collection fts shard existence: %w", err)
	}

	pragma := fmt.Sprintf(`PRAGMA table_info(%s)`, ftsIdent)
	rows, err := tx.Query(pragma)
	if err != nil {
		return false, fmt.Errorf("inspect collection fts shard columns: %w", err)
	}
	defer rows.Close()

	hasTitle := false
	hasCleanContent := false
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKey); err != nil {
			return false, fmt.Errorf("scan collection fts shard column: %w", err)
		}
		switch name {
		case "title":
			hasTitle = true
		case "clean_content":
			hasCleanContent = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate collection fts shard columns: %w", err)
	}

	if !hasTitle || !hasCleanContent {
		return true, nil
	}

	return false, nil
}

func (s *Store) hasLegacyGlobalFTSArtifacts() (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE (type = 'table' AND name IN ('docs_fts', 'docs_vocab'))
		   OR (type = 'trigger' AND name IN ('documents_ai', 'documents_ad', 'documents_au'))
	`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check legacy fts artifacts: %w", err)
	}
	return count > 0, nil
}

func collectionFTSTableName(collectionID int64) (string, error) {
	if collectionID <= 0 {
		return "", fmt.Errorf("invalid collection id %d", collectionID)
	}
	return fmt.Sprintf("%s%d", collectionFTSTablePrefix, collectionID), nil
}

func collectionVocabTableName(collectionID int64) (string, error) {
	if collectionID <= 0 {
		return "", fmt.Errorf("invalid collection id %d", collectionID)
	}
	return fmt.Sprintf("%s%d", collectionVocabTablePrefix, collectionID), nil
}

func quoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty SQL identifier")
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		isLetter := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		isDigit := ch >= '0' && ch <= '9'
		if !isLetter && !isDigit && ch != '_' {
			return "", fmt.Errorf("invalid SQL identifier %q", name)
		}
		if i == 0 && isDigit {
			return "", fmt.Errorf("invalid SQL identifier %q", name)
		}
	}
	return `"` + name + `"`, nil
}

func collectionSearchIndexIdents(collectionID int64) (string, string, string, string, error) {
	ftsName, err := collectionFTSTableName(collectionID)
	if err != nil {
		return "", "", "", "", err
	}
	vocabName, err := collectionVocabTableName(collectionID)
	if err != nil {
		return "", "", "", "", err
	}
	ftsIdent, err := quoteIdent(ftsName)
	if err != nil {
		return "", "", "", "", err
	}
	vocabIdent, err := quoteIdent(vocabName)
	if err != nil {
		return "", "", "", "", err
	}

	return ftsName, ftsIdent, vocabName, vocabIdent, nil
}

// CollectionSearchIndexExists reports whether the collection-local FTS table exists.
func (s *Store) CollectionSearchIndexExists(collectionID int64) (bool, error) {
	ftsName, err := collectionFTSTableName(collectionID)
	if err != nil {
		return false, err
	}

	var exists int
	err = s.db.QueryRow(`
		SELECT 1
		FROM sqlite_master
		WHERE type = 'table'
		  AND name = ?
		LIMIT 1
	`, ftsName).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check collection index existence: %w", err)
	}

	return true, nil
}

// EnsureCollectionSearchIndexTx creates collection-local FTS and vocab tables.
func (s *Store) EnsureCollectionSearchIndexTx(tx *sql.Tx, collectionID int64) error {
	_, ftsIdent, _, vocabIdent, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return err
	}

	createFTS := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
			title,
			clean_content,
			tokenize='unicode61'
		)
	`, ftsIdent)
	if _, err := tx.Exec(createFTS); err != nil {
		return fmt.Errorf("create collection fts index: %w", err)
	}

	createVocab := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5vocab(%s, 'instance')
	`, vocabIdent, ftsIdent)
	if _, err := tx.Exec(createVocab); err != nil {
		return fmt.Errorf("create collection vocab index: %w", err)
	}

	return nil
}

// RebuildCollectionSearchIndexTx fully rebuilds collection-local FTS content
// from the canonical documents table.
func (s *Store) RebuildCollectionSearchIndexTx(tx *sql.Tx, collectionID int64) error {
	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return err
	}

	if err := s.EnsureCollectionSearchIndexTx(tx, collectionID); err != nil {
		return err
	}

	clearStmt := fmt.Sprintf(`DELETE FROM %s`, ftsIdent)
	if _, err := tx.Exec(clearStmt); err != nil {
		return fmt.Errorf("clear collection fts index: %w", err)
	}

	rows, err := tx.Query(`
		SELECT id, raw_content, clean_content
		FROM documents
		WHERE collection_id = ?
	`, collectionID)
	if err != nil {
		return fmt.Errorf("list documents for fts rebuild: %w", err)
	}
	defer rows.Close()

	insertStmt := fmt.Sprintf(`
		INSERT INTO %s(rowid, title, clean_content)
		VALUES(?, ?, ?)
	`, ftsIdent)
	stmt, err := tx.Prepare(insertStmt)
	if err != nil {
		return fmt.Errorf("prepare collection fts rebuild insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		var (
			id           int64
			rawContent   string
			cleanContent string
		)
		if err := rows.Scan(&id, &rawContent, &cleanContent); err != nil {
			return fmt.Errorf("scan document for fts rebuild: %w", err)
		}
		title := frontmatter.Extract(rawContent).Title
		if _, err := stmt.Exec(id, title, cleanContent); err != nil {
			return fmt.Errorf("insert collection fts row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate documents for fts rebuild: %w", err)
	}

	return nil
}

// DropCollectionSearchIndexTx deletes collection-local FTS and vocab tables.
func (s *Store) DropCollectionSearchIndexTx(tx *sql.Tx, collectionID int64) error {
	_, ftsIdent, _, vocabIdent, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return err
	}

	dropVocab := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, vocabIdent)
	if _, err := tx.Exec(dropVocab); err != nil {
		return fmt.Errorf("drop collection vocab index: %w", err)
	}

	dropFTS := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, ftsIdent)
	if _, err := tx.Exec(dropFTS); err != nil {
		return fmt.Errorf("drop collection fts index: %w", err)
	}

	return nil
}

// CreateCollection inserts a named collection and creates its initial
// directory source at rootPath.
func (s *Store) CreateCollection(name, rootPath, ignoreFilePath string) (Collection, error) {
	var out Collection
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`INSERT INTO collections(name, root_path, ignore_file_path) VALUES(?, ?, ?)`,
			name, rootPath, ignoreFilePath,
		)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return fmt.Errorf("collection %q already exists", name)
			}
			return fmt.Errorf("create collection: %w", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read created collection id: %w", err)
		}

		if err := s.EnsureCollectionSearchIndexTx(tx, id); err != nil {
			return err
		}
		if _, err := s.addCollectionSourceTx(tx, CollectionSource{
			CollectionID:   id,
			SourceType:     SourceTypeDirectory,
			SourcePath:     rootPath,
			IgnoreFilePath: ignoreFilePath,
			Enabled:        true,
		}); err != nil {
			return err
		}

		out = Collection{ID: id, Name: name, RootPath: rootPath, IgnoreFilePath: ignoreFilePath}
		return nil
	})
	if err != nil {
		return Collection{}, err
	}

	return out, nil
}

func (s *Store) addCollectionSourceTx(tx *sql.Tx, source CollectionSource) (CollectionSource, error) {
	if source.CollectionID <= 0 {
		return CollectionSource{}, fmt.Errorf("invalid collection id %d", source.CollectionID)
	}
	if source.SourceType != SourceTypeDirectory && source.SourceType != SourceTypeFile {
		return CollectionSource{}, fmt.Errorf("invalid source type %q", source.SourceType)
	}
	if strings.TrimSpace(source.SourcePath) == "" {
		return CollectionSource{}, fmt.Errorf("source path cannot be empty")
	}

	res, err := tx.Exec(
		`INSERT INTO collection_sources(collection_id, source_type, source_path, ignore_file_path, enabled, updated_at)
		 VALUES(?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		source.CollectionID,
		source.SourceType,
		source.SourcePath,
		nullIfEmpty(source.IgnoreFilePath),
		boolToInt(source.Enabled),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return CollectionSource{}, fmt.Errorf("source %q already exists in this collection", source.SourcePath)
		}
		return CollectionSource{}, fmt.Errorf("add collection source: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return CollectionSource{}, fmt.Errorf("read created source id: %w", err)
	}

	source.ID = id
	return source, nil
}

// AddCollectionSource attaches a file or directory source to a collection.
func (s *Store) AddCollectionSource(collectionID int64, sourceType, sourcePath, ignoreFilePath string) (CollectionSource, error) {
	var out CollectionSource
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		source, err := s.addCollectionSourceTx(tx, CollectionSource{
			CollectionID:   collectionID,
			SourceType:     sourceType,
			SourcePath:     sourcePath,
			IgnoreFilePath: ignoreFilePath,
			Enabled:        true,
		})
		if err != nil {
			return err
		}
		out = source
		return nil
	})
	if err != nil {
		return CollectionSource{}, err
	}

	return out, nil
}

// ListCollectionSources returns all configured sources for a collection.
func (s *Store) ListCollectionSources(collectionID int64) ([]CollectionSource, error) {
	rows, err := s.db.Query(`
		SELECT id, collection_id, source_type, source_path, COALESCE(ignore_file_path, ''), enabled
		FROM collection_sources
		WHERE collection_id = ?
		ORDER BY id ASC
	`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list collection sources: %w", err)
	}
	defer rows.Close()

	var out []CollectionSource
	for rows.Next() {
		var source CollectionSource
		var enabled int
		if err := rows.Scan(&source.ID, &source.CollectionID, &source.SourceType, &source.SourcePath, &source.IgnoreFilePath, &enabled); err != nil {
			return nil, fmt.Errorf("scan collection source: %w", err)
		}
		source.Enabled = enabled != 0
		out = append(out, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection sources: %w", err)
	}

	return out, nil
}

// PrimaryDirectorySource returns the earliest directory source for a collection.
func (s *Store) PrimaryDirectorySource(collectionID int64) (CollectionSource, error) {
	var source CollectionSource
	var enabled int
	err := s.db.QueryRow(`
		SELECT id, collection_id, source_type, source_path, COALESCE(ignore_file_path, ''), enabled
		FROM collection_sources
		WHERE collection_id = ?
		  AND source_type = ?
		  AND enabled = 1
		ORDER BY id ASC
		LIMIT 1
	`, collectionID, SourceTypeDirectory).Scan(
		&source.ID,
		&source.CollectionID,
		&source.SourceType,
		&source.SourcePath,
		&source.IgnoreFilePath,
		&enabled,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CollectionSource{}, fmt.Errorf("collection has no directory source")
		}
		return CollectionSource{}, fmt.Errorf("get primary directory source: %w", err)
	}
	source.Enabled = enabled != 0
	return source, nil
}

// RemoveCollectionSourceByID removes a source by numeric identifier.
func (s *Store) RemoveCollectionSourceByID(collectionID, sourceID int64) (CollectionSource, error) {
	var removed CollectionSource
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		var enabled int
		err := tx.QueryRow(`
			SELECT id, collection_id, source_type, source_path, COALESCE(ignore_file_path, ''), enabled
			FROM collection_sources
			WHERE id = ?
			  AND collection_id = ?
		`, sourceID, collectionID).Scan(
			&removed.ID,
			&removed.CollectionID,
			&removed.SourceType,
			&removed.SourcePath,
			&removed.IgnoreFilePath,
			&enabled,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("source %d not found in this collection", sourceID)
			}
			return fmt.Errorf("resolve collection source: %w", err)
		}
		removed.Enabled = enabled != 0

		res, err := tx.Exec(`DELETE FROM collection_sources WHERE id = ? AND collection_id = ?`, sourceID, collectionID)
		if err != nil {
			return fmt.Errorf("delete collection source: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("read collection source delete rows affected: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("source %d not found in this collection", sourceID)
		}

		return nil
	})
	if err != nil {
		return CollectionSource{}, err
	}
	return removed, nil
}

// RemoveCollectionSourceByPath removes a source by exact absolute path.
func (s *Store) RemoveCollectionSourceByPath(collectionID int64, sourcePath string) (CollectionSource, error) {
	var removed CollectionSource
	err := s.RunInTransaction(func(tx *sql.Tx) error {
		var enabled int
		err := tx.QueryRow(`
			SELECT id, collection_id, source_type, source_path, COALESCE(ignore_file_path, ''), enabled
			FROM collection_sources
			WHERE source_path = ?
			  AND collection_id = ?
		`, sourcePath, collectionID).Scan(
			&removed.ID,
			&removed.CollectionID,
			&removed.SourceType,
			&removed.SourcePath,
			&removed.IgnoreFilePath,
			&enabled,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("source %q not found in this collection", sourcePath)
			}
			return fmt.Errorf("resolve collection source: %w", err)
		}
		removed.Enabled = enabled != 0

		res, err := tx.Exec(`DELETE FROM collection_sources WHERE source_path = ? AND collection_id = ?`, sourcePath, collectionID)
		if err != nil {
			return fmt.Errorf("delete collection source: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("read collection source delete rows affected: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("source %q not found in this collection", sourcePath)
		}

		return nil
	})
	if err != nil {
		return CollectionSource{}, err
	}
	return removed, nil
}

// ListDBSources returns sources across all collections with optional filtering,
// sorting, and per-source indexed document stats.
func (s *Store) ListDBSources(q DBSourceQuery) ([]DBSourceRow, error) {
	if q.EnabledOnly && q.DisabledOnly {
		return nil, fmt.Errorf("enabled-only and disabled-only filters are mutually exclusive")
	}
	if q.SourceType != "" && q.SourceType != SourceTypeDirectory && q.SourceType != SourceTypeFile {
		return nil, fmt.Errorf("invalid source type %q", q.SourceType)
	}

	where := make([]string, 0, 6)
	args := make([]any, 0, 10)

	if strings.TrimSpace(q.CollectionName) != "" {
		where = append(where, "c.name = ?")
		args = append(args, strings.TrimSpace(q.CollectionName))
	}
	if strings.TrimSpace(q.SourceType) != "" {
		where = append(where, "cs.source_type = ?")
		args = append(args, strings.TrimSpace(q.SourceType))
	}
	if q.EnabledOnly {
		where = append(where, "cs.enabled = 1")
	}
	if q.DisabledOnly {
		where = append(where, "cs.enabled = 0")
	}
	if strings.TrimSpace(q.PathPrefix) != "" {
		prefix := strings.TrimSpace(q.PathPrefix)
		where = append(where, pathPrefixPredicateSQL("cs.source_path"))
		args = append(args, prefix, prefix, prefix, prefix)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	orderBy, err := dbSourceOrderClause(q.SortBy, q.Desc, q.IncludeStats)
	if err != nil {
		return nil, err
	}

	if !q.IncludeStats {
		query := fmt.Sprintf(`
			SELECT
				cs.id,
				cs.collection_id,
				c.name,
				cs.source_type,
				cs.source_path,
				COALESCE(cs.ignore_file_path, ''),
				cs.enabled,
				cs.created_at,
				cs.updated_at
			FROM collection_sources cs
			JOIN collections c ON c.id = cs.collection_id
			%s
			ORDER BY %s
		`, whereClause, orderBy)

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("list db sources: %w", err)
		}
		defer rows.Close()

		out := make([]DBSourceRow, 0)
		for rows.Next() {
			var (
				row     DBSourceRow
				enabled int
			)
			if err := rows.Scan(
				&row.SourceID,
				&row.CollectionID,
				&row.CollectionName,
				&row.SourceType,
				&row.SourcePath,
				&row.IgnoreFilePath,
				&enabled,
				&row.CreatedAt,
				&row.UpdatedAt,
			); err != nil {
				return nil, fmt.Errorf("scan db source row: %w", err)
			}
			row.Enabled = enabled != 0
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate db source rows: %w", err)
		}

		return out, nil
	}

	query := fmt.Sprintf(`
		WITH filtered_sources AS (
			SELECT
				cs.id,
				cs.collection_id,
				c.name AS collection_name,
				cs.source_type,
				cs.source_path,
				COALESCE(cs.ignore_file_path, '') AS ignore_file_path,
				cs.enabled,
				cs.created_at,
				cs.updated_at
			FROM collection_sources cs
			JOIN collections c ON c.id = cs.collection_id
			%s
		),
		source_document_map AS (
			SELECT
				fs.id AS source_id,
				d.id AS document_id,
				d.updated_at AS document_updated_at
			FROM filtered_sources fs
			LEFT JOIN documents d
				ON d.collection_id = fs.collection_id
				AND %s
		)
		SELECT
			fs.id,
			fs.collection_id,
			fs.collection_name,
			fs.source_type,
			fs.source_path,
			fs.ignore_file_path,
			fs.enabled,
			fs.created_at,
			fs.updated_at,
			COUNT(sdm.document_id) AS indexed_docs,
			COALESCE(MAX(sdm.document_updated_at), '') AS latest_indexed_at
		FROM filtered_sources fs
		LEFT JOIN source_document_map sdm ON sdm.source_id = fs.id
		GROUP BY
			fs.id,
			fs.collection_id,
			fs.collection_name,
			fs.source_type,
			fs.source_path,
			fs.ignore_file_path,
			fs.enabled,
			fs.created_at,
			fs.updated_at
		ORDER BY %s
	`, whereClause, sourceCoversDocumentPredicateSQL("d.path", "fs.source_path", "fs.source_type"), orderBy)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list db sources with stats: %w", err)
	}
	defer rows.Close()

	out := make([]DBSourceRow, 0)
	for rows.Next() {
		var (
			row     DBSourceRow
			enabled int
		)
		if err := rows.Scan(
			&row.SourceID,
			&row.CollectionID,
			&row.CollectionName,
			&row.SourceType,
			&row.SourcePath,
			&row.IgnoreFilePath,
			&enabled,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.IndexedDocs,
			&row.LatestIndexedAt,
		); err != nil {
			return nil, fmt.Errorf("scan db source row with stats: %w", err)
		}
		row.Enabled = enabled != 0
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate db source rows with stats: %w", err)
	}

	return out, nil
}

func pathPrefixPredicateSQL(column string) string {
	return fmt.Sprintf(`(%s = ? OR (LENGTH(%s) > LENGTH(?) AND SUBSTR(%s, 1, LENGTH(?) + 1) = ? || '/'))`, column, column, column)
}

func sourceCoversDocumentPredicateSQL(docPathColumn, sourcePathColumn, sourceTypeColumn string) string {
	return fmt.Sprintf(`(
		(%s = '%s' AND %s = %s)
		OR
		(
			%s = '%s'
			AND (
				%s = %s
				OR (
					LENGTH(%s) > LENGTH(%s)
					AND SUBSTR(%s, 1, LENGTH(%s) + 1) = %s || '/'
				)
			)
		)
	)`,
		sourceTypeColumn, SourceTypeFile, docPathColumn, sourcePathColumn,
		sourceTypeColumn, SourceTypeDirectory,
		docPathColumn, sourcePathColumn,
		docPathColumn, sourcePathColumn,
		docPathColumn, sourcePathColumn, sourcePathColumn,
	)
}

func dbSourceOrderClause(sortBy string, desc bool, withStats bool) (string, error) {
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy == "" {
		sortBy = "added"
	}

	direction := "ASC"
	if desc {
		direction = "DESC"
	}

	createdCol := "cs.created_at"
	updatedCol := "cs.updated_at"
	collectionCol := "c.name"
	pathCol := "cs.source_path"
	idCol := "cs.id"
	if withStats {
		createdCol = "fs.created_at"
		updatedCol = "fs.updated_at"
		collectionCol = "fs.collection_name"
		pathCol = "fs.source_path"
		idCol = "fs.id"
	}

	switch sortBy {
	case "added":
		return fmt.Sprintf("%s %s, %s ASC", createdCol, direction, idCol), nil
	case "updated":
		return fmt.Sprintf("%s %s, %s ASC", updatedCol, direction, idCol), nil
	case "collection":
		return fmt.Sprintf("%s %s, %s ASC", collectionCol, direction, idCol), nil
	case "path":
		return fmt.Sprintf("%s %s, %s ASC", pathCol, direction, idCol), nil
	default:
		return "", fmt.Errorf("invalid sort %q (expected: added, updated, collection, path)", sortBy)
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullIfEmpty(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

// ListCollections returns all collections with current document counts.
func (s *Store) ListCollections() ([]CollectionSummary, error) {
	rows, err := s.db.Query(`
		SELECT
			c.name,
			COALESCE(
				(
					SELECT cs.source_path
					FROM collection_sources cs
					WHERE cs.collection_id = c.id
					  AND cs.source_type = ?
					ORDER BY cs.id ASC
					LIMIT 1
				),
				c.root_path
			),
			COUNT(d.id),
			CASE
				WHEN c.id = (SELECT default_collection_id FROM app_state WHERE id = 1)
				THEN 1 ELSE 0
			END AS is_default
		FROM collections c
		LEFT JOIN documents d ON d.collection_id = c.id
		GROUP BY c.id, c.name, c.root_path
		ORDER BY c.name ASC`, SourceTypeDirectory)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()

	var out []CollectionSummary
	for rows.Next() {
		var r CollectionSummary
		var isDefault int
		if err := rows.Scan(&r.Name, &r.RootPath, &r.Documents, &isDefault); err != nil {
			return nil, fmt.Errorf("scan collection summary: %w", err)
		}
		r.IsDefault = isDefault != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection summaries: %w", err)
	}
	return out, nil
}

// GetCollectionByName resolves collection metadata by logical name.
func (s *Store) GetCollectionByName(name string) (Collection, error) {
	var c Collection
	err := s.db.QueryRow(
		`SELECT id, name, root_path, ignore_file_path FROM collections WHERE name = ?`,
		name,
	).Scan(&c.ID, &c.Name, &c.RootPath, &c.IgnoreFilePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Collection{}, fmt.Errorf("collection %q not found", name)
		}
		return Collection{}, fmt.Errorf("get collection: %w", err)
	}
	return c, nil
}

// RenameCollection updates only the collection name.
func (s *Store) RenameCollection(oldName, newName string) error {
	res, err := s.db.Exec(
		`UPDATE collections SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`,
		newName, oldName,
	)
	if err != nil {
		return fmt.Errorf("rename collection: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rename rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("collection %q not found", oldName)
	}
	return nil
}

// DeleteCollection removes the collection and all associated documents.
func (s *Store) DeleteCollection(name string) error {
	return s.RunInTransaction(func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRow(`SELECT id FROM collections WHERE name = ?`, name).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("collection %q not found", name)
			}
			return fmt.Errorf("resolve collection for delete: %w", err)
		}

		res, err := tx.Exec(`DELETE FROM collections WHERE name = ?`, name)
		if err != nil {
			return fmt.Errorf("delete collection: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("read delete rows affected: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("collection %q not found", name)
		}

		return s.DropCollectionSearchIndexTx(tx, id)
	})
}

// ListDocumentsForCollection returns existing docs keyed by absolute path.
func (s *Store) ListDocumentsForCollection(collectionID int64) (map[string]DocumentRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, collection_id, path, rel_path, file_hash, mtime_ns, size_bytes, line_count, raw_content, clean_content
		FROM documents
		WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	out := make(map[string]DocumentRecord)
	for rows.Next() {
		var d DocumentRecord
		if err := rows.Scan(
			&d.ID,
			&d.CollectionID,
			&d.Path,
			&d.RelPath,
			&d.FileHash,
			&d.MTimeNS,
			&d.SizeBytes,
			&d.LineCount,
			&d.RawContent,
			&d.CleanContent,
		); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		out[d.Path] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return out, nil
}

// RunInTransaction executes fn within a single SQLite transaction.
func (s *Store) RunInTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// UpsertDocument inserts or updates one document row within a transaction.
func (s *Store) UpsertDocument(tx *sql.Tx, doc DocumentRecord) error {
	_, err := tx.Exec(`
		INSERT INTO documents (
			collection_id, path, rel_path, file_hash, mtime_ns, size_bytes,
			line_count, raw_content, clean_content, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(collection_id, path) DO UPDATE SET
			rel_path = excluded.rel_path,
			file_hash = excluded.file_hash,
			mtime_ns = excluded.mtime_ns,
			size_bytes = excluded.size_bytes,
			line_count = excluded.line_count,
			raw_content = excluded.raw_content,
			clean_content = excluded.clean_content,
			updated_at = CURRENT_TIMESTAMP
	`, doc.CollectionID, doc.Path, doc.RelPath, doc.FileHash, doc.MTimeNS, doc.SizeBytes, doc.LineCount, doc.RawContent, doc.CleanContent)
	if err != nil {
		return fmt.Errorf("upsert document %s: %w", doc.Path, err)
	}
	return nil
}

// DeleteDocumentsByPath removes any documents matching the provided paths
// within a transaction.
func (s *Store) DeleteDocumentsByPath(tx *sql.Tx, collectionID int64, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	stmt, err := tx.Prepare(`DELETE FROM documents WHERE collection_id = ? AND path = ?`)
	if err != nil {
		return fmt.Errorf("prepare document delete: %w", err)
	}
	defer stmt.Close()

	for _, path := range paths {
		if _, err := stmt.Exec(collectionID, path); err != nil {
			return fmt.Errorf("delete document %s: %w", path, err)
		}
	}
	return nil
}

// SearchRankedDocs returns ranked documents and total candidate count.
func (s *Store) SearchRankedDocs(collectionID int64, ftsQuery string, limit int) ([]RankedDoc, int, error) {
	return s.SearchRankedDocsWithTerms(collectionID, ftsQuery, strings.Fields(ftsQuery), limit, false)
}

// SearchRankedDocsWithTerms returns ranked documents and total candidate count,
// using queryTerms for per-document hit and optional term-coverage statistics.
func (s *Store) SearchRankedDocsWithTerms(collectionID int64, ftsQuery string, queryTerms []string, limit int, includeCoverage bool) ([]RankedDoc, int, error) {
	total, err := s.countMatches(collectionID, ftsQuery)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT d.id, d.path, d.line_count
		FROM %s
		JOIN documents d ON d.id = %s.rowid
		WHERE d.collection_id = ?
		  AND %s MATCH ?
		ORDER BY bm25(%s, ?, ?) ASC
		LIMIT ?
	`, ftsIdent, ftsIdent, ftsIdent, ftsIdent)

	rows, err := s.db.Query(query, collectionID, ftsQuery, titleBM25Weight, bodyBM25Weight, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("run ranked search: %w", err)
	}
	defer rows.Close()

	var docs []RankedDoc
	var ids []int64
	for rows.Next() {
		var r RankedDoc
		if err := rows.Scan(&r.ID, &r.Path, &r.LineCount); err != nil {
			return nil, 0, fmt.Errorf("scan ranked doc: %w", err)
		}
		docs = append(docs, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate ranked docs: %w", err)
	}

	hits, err := s.termHitCounts(collectionID, ids, queryTerms)
	if err != nil {
		return nil, 0, err
	}
	for i := range docs {
		docs[i].Matches = hits[docs[i].ID]
	}

	if includeCoverage {
		coverage, err := s.termCoverageCounts(collectionID, ids, queryTerms)
		if err != nil {
			return nil, 0, err
		}
		for i := range docs {
			docs[i].MatchedTerms = coverage[docs[i].ID]
		}
	}

	return docs, total, nil
}

// SearchSampleDocs returns ranked documents with content payload for sampling.
func (s *Store) SearchSampleDocs(collectionID int64, ftsQuery string, limit int) ([]SampleDoc, int, error) {
	total, err := s.countMatches(collectionID, ftsQuery)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT d.id, d.path, d.line_count, d.raw_content
		FROM %s
		JOIN documents d ON d.id = %s.rowid
		WHERE d.collection_id = ?
		  AND %s MATCH ?
		ORDER BY bm25(%s, ?, ?) ASC
		LIMIT ?
	`, ftsIdent, ftsIdent, ftsIdent, ftsIdent)

	rows, err := s.db.Query(query, collectionID, ftsQuery, titleBM25Weight, bodyBM25Weight, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("run sample search: %w", err)
	}
	defer rows.Close()

	var docs []SampleDoc
	for rows.Next() {
		var r SampleDoc
		if err := rows.Scan(&r.ID, &r.Path, &r.LineCount, &r.RawContent); err != nil {
			return nil, 0, fmt.Errorf("scan sample doc: %w", err)
		}
		docs = append(docs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate sample docs: %w", err)
	}

	return docs, total, nil
}

// GetRawContentByDocIDs returns raw content keyed by document ID, scoped to a
// specific collection.
func (s *Store) GetRawContentByDocIDs(collectionID int64, docIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string)
	if len(docIDs) == 0 {
		return out, nil
	}

	docClause := placeholders(len(docIDs))
	query := fmt.Sprintf(`
		SELECT id, raw_content
		FROM documents
		WHERE collection_id = ?
		  AND id IN (%s)
	`, docClause)

	args := make([]any, 0, len(docIDs)+1)
	args = append(args, collectionID)
	for _, id := range docIDs {
		args = append(args, id)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query raw content by doc ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("scan raw content row: %w", err)
		}
		out[id] = raw
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate raw content rows: %w", err)
	}

	return out, nil
}

func (s *Store) countMatches(collectionID int64, ftsQuery string) (int, error) {
	_, ftsIdent, _, _, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return 0, err
	}

	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s
		JOIN documents d ON d.id = %s.rowid
		WHERE d.collection_id = ?
		  AND %s MATCH ?
	`, ftsIdent, ftsIdent, ftsIdent)

	var total int
	err = s.db.QueryRow(query, collectionID, ftsQuery).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("count matching documents: %w", err)
	}
	return total, nil
}

// termHitCounts returns total token hits by document for query terms.
func (s *Store) termHitCounts(collectionID int64, docIDs []int64, terms []string) (map[int64]int, error) {
	out := make(map[int64]int)
	if len(docIDs) == 0 {
		return out, nil
	}

	if len(terms) == 0 {
		return out, nil
	}

	docClause := placeholders(len(docIDs))
	termClause := placeholders(len(terms))

	_, _, _, vocabIdent, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT doc, COUNT(*)
		FROM %s
		WHERE doc IN (%s)
		  AND term IN (%s)
		GROUP BY doc
	`, vocabIdent, docClause, termClause)

	args := make([]any, 0, len(docIDs)+len(terms))
	for _, id := range docIDs {
		args = append(args, id)
	}
	for _, term := range terms {
		args = append(args, term)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query token hit counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var docID int64
		var hits int
		if err := rows.Scan(&docID, &hits); err != nil {
			return nil, fmt.Errorf("scan token hit count: %w", err)
		}
		out[docID] = hits
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token hit counts: %w", err)
	}

	return out, nil
}

// termCoverageCounts returns distinct matched query terms by document.
func (s *Store) termCoverageCounts(collectionID int64, docIDs []int64, terms []string) (map[int64]int, error) {
	out := make(map[int64]int)
	if len(docIDs) == 0 {
		return out, nil
	}
	if len(terms) == 0 {
		return out, nil
	}

	docClause := placeholders(len(docIDs))
	termClause := placeholders(len(terms))

	_, _, _, vocabIdent, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT doc, COUNT(DISTINCT term)
		FROM %s
		WHERE doc IN (%s)
		  AND term IN (%s)
		GROUP BY doc
	`, vocabIdent, docClause, termClause)

	args := make([]any, 0, len(docIDs)+len(terms))
	for _, id := range docIDs {
		args = append(args, id)
	}
	for _, term := range terms {
		args = append(args, term)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query term coverage counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var docID int64
		var matchedTerms int
		if err := rows.Scan(&docID, &matchedTerms); err != nil {
			return nil, fmt.Errorf("scan term coverage count: %w", err)
		}
		out[docID] = matchedTerms
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate term coverage counts: %w", err)
	}

	return out, nil
}

// TermIDFWeights returns BM25-style IDF weights for the given query terms
// within a collection. IDF = log((N - df + 0.5) / (df + 0.5) + 1).
func (s *Store) TermIDFWeights(collectionID int64, terms []string) (map[string]float64, error) {
	weights := make(map[string]float64, len(terms))
	if len(terms) == 0 {
		return weights, nil
	}

	var totalDocs float64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE collection_id = ?`, collectionID).Scan(&totalDocs)
	if err != nil {
		return nil, fmt.Errorf("count documents: %w", err)
	}
	if totalDocs == 0 {
		for _, t := range terms {
			weights[t] = 1.0
		}
		return weights, nil
	}

	_, _, _, vocabIdent, err := collectionSearchIndexIdents(collectionID)
	if err != nil {
		return nil, err
	}

	termClause := placeholders(len(terms))
	query := fmt.Sprintf(`
		SELECT term, COUNT(DISTINCT doc)
		FROM %s
		WHERE term IN (%s)
		GROUP BY term
	`, vocabIdent, termClause)

	args := make([]any, 0, len(terms))
	for _, t := range terms {
		args = append(args, t)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query term doc frequencies: %w", err)
	}
	defer rows.Close()

	dfMap := make(map[string]float64, len(terms))
	for rows.Next() {
		var term string
		var df float64
		if err := rows.Scan(&term, &df); err != nil {
			return nil, fmt.Errorf("scan term doc frequency: %w", err)
		}
		dfMap[term] = df
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate term doc frequencies: %w", err)
	}

	for _, t := range terms {
		df := dfMap[t]
		weights[t] = math.Log((totalDocs-df+0.5)/(df+0.5) + 1)
	}

	return weights, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
