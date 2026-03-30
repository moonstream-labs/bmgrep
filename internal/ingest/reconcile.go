package ingest

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

// ReconcileStats summarizes how many files were changed during sync.
type ReconcileStats struct {
	Added   int
	Updated int
	Deleted int
}

// ReconcileCollection synchronizes SQLite state with on-disk markdown files.
//
// All database mutations are wrapped in a single transaction so the index
// is never left in a partially-updated state.
func ReconcileCollection(s *store.Store, c store.Collection) (ReconcileStats, error) {
	files, err := ScanMarkdownFiles(c.RootPath, c.IgnoreFilePath)
	if err != nil {
		return ReconcileStats{}, err
	}

	existing, err := s.ListDocumentsForCollection(c.ID)
	if err != nil {
		return ReconcileStats{}, err
	}

	// Pre-compute all file reads and diffs before opening the transaction
	// to minimize time spent holding the write lock.
	type pendingUpsert struct {
		doc   store.DocumentRecord
		isNew bool
	}
	var upserts []pendingUpsert
	seen := make(map[string]bool, len(files))

	for _, file := range files {
		seen[file.AbsPath] = true
		current, found := existing[file.AbsPath]

		if found && current.MTimeNS == file.MTimeNS && current.SizeBytes == file.SizeBytes {
			continue
		}

		rawBytes, err := os.ReadFile(file.AbsPath)
		if err != nil {
			return ReconcileStats{}, fmt.Errorf("read %s: %w", file.AbsPath, err)
		}

		raw := string(rawBytes)
		h := hash(rawBytes)

		if found && current.FileHash == h {
			current.RelPath = file.RelPath
			current.MTimeNS = file.MTimeNS
			current.SizeBytes = file.SizeBytes
			upserts = append(upserts, pendingUpsert{doc: current, isNew: false})
			continue
		}

		lineCount := countLines(raw)
		doc := store.DocumentRecord{
			CollectionID: c.ID,
			Path:         file.AbsPath,
			RelPath:      file.RelPath,
			FileHash:     h,
			MTimeNS:      file.MTimeNS,
			SizeBytes:    file.SizeBytes,
			LineCount:    lineCount,
			RawContent:   raw,
			CleanContent: ToCleanText(rawBytes),
		}
		upserts = append(upserts, pendingUpsert{doc: doc, isNew: !found})
	}

	var toDelete []string
	for path := range existing {
		if !seen[path] {
			toDelete = append(toDelete, path)
		}
	}

	indexExists, err := s.CollectionSearchIndexExists(c.ID)
	if err != nil {
		return ReconcileStats{}, err
	}
	needsIndexRebuild := !indexExists || len(upserts) > 0 || len(toDelete) > 0

	stats := ReconcileStats{}
	err = s.RunInTransaction(func(tx *sql.Tx) error {
		if err := s.EnsureCollectionSearchIndexTx(tx, c.ID); err != nil {
			return err
		}

		for _, u := range upserts {
			if err := s.UpsertDocument(tx, u.doc); err != nil {
				return err
			}
			if u.isNew {
				stats.Added++
			} else {
				stats.Updated++
			}
		}

		if err := s.DeleteDocumentsByPath(tx, c.ID, toDelete); err != nil {
			return err
		}

		if needsIndexRebuild {
			return s.RebuildCollectionSearchIndexTx(tx, c.ID)
		}

		return nil
	})
	if err != nil {
		return ReconcileStats{}, err
	}
	stats.Deleted = len(toDelete)

	return stats, nil
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		count++
	}
	return count
}
