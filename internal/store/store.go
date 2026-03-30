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

	_ "modernc.org/sqlite"
)

const (
	collectionFTSTablePrefix   = "docs_fts_c"
	collectionVocabTablePrefix = "docs_vocab_c"
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
	ID        int64
	Path      string
	LineCount int
	Matches   int
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_collection_path ON documents(collection_id, path);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_collection_rel_path ON documents(collection_id, rel_path);`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection_id ON documents(collection_id);`,
	}

	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}

	if err := s.migrateLegacyGlobalFTS(); err != nil {
		return err
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

	rebuildStmt := fmt.Sprintf(`
		INSERT INTO %s(rowid, clean_content)
		SELECT id, clean_content
		FROM documents
		WHERE collection_id = ?
	`, ftsIdent)
	if _, err := tx.Exec(rebuildStmt, collectionID); err != nil {
		return fmt.Errorf("rebuild collection fts index: %w", err)
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

// CreateCollection inserts a named collection rooted at rootPath.
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

		out = Collection{ID: id, Name: name, RootPath: rootPath, IgnoreFilePath: ignoreFilePath}
		return nil
	})
	if err != nil {
		return Collection{}, err
	}

	return out, nil
}

// ListCollections returns all collections with current document counts.
func (s *Store) ListCollections() ([]CollectionSummary, error) {
	rows, err := s.db.Query(`
		SELECT c.name, c.root_path, COUNT(d.id)
		FROM collections c
		LEFT JOIN documents d ON d.collection_id = c.id
		GROUP BY c.id, c.name, c.root_path
		ORDER BY c.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()

	var out []CollectionSummary
	for rows.Next() {
		var r CollectionSummary
		if err := rows.Scan(&r.Name, &r.RootPath, &r.Documents); err != nil {
			return nil, fmt.Errorf("scan collection summary: %w", err)
		}
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
		ORDER BY bm25(%s) ASC
		LIMIT ?
	`, ftsIdent, ftsIdent, ftsIdent, ftsIdent)

	rows, err := s.db.Query(query, collectionID, ftsQuery, limit)
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

	hits, err := s.termHitCounts(collectionID, ids, ftsQuery)
	if err != nil {
		return nil, 0, err
	}
	for i := range docs {
		docs[i].Matches = hits[docs[i].ID]
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
		ORDER BY bm25(%s) ASC
		LIMIT ?
	`, ftsIdent, ftsIdent, ftsIdent, ftsIdent)

	rows, err := s.db.Query(query, collectionID, ftsQuery, limit)
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
// The query is split by spaces because bmgrep normalizes it to plain terms.
func (s *Store) termHitCounts(collectionID int64, docIDs []int64, normalizedQuery string) (map[int64]int, error) {
	out := make(map[int64]int)
	if len(docIDs) == 0 {
		return out, nil
	}

	terms := strings.Fields(normalizedQuery)
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
