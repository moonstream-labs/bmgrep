package ingest

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	files, err := scanCollectionSources(s, c)
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

func scanCollectionSources(s *store.Store, c store.Collection) ([]FileInfo, error) {
	sources, err := s.ListCollectionSources(c.ID)
	if err != nil {
		return nil, err
	}

	if len(sources) == 0 && strings.TrimSpace(c.RootPath) != "" {
		// Legacy fallback; normally migrateCollectionSourcesFromLegacyRoot
		// ensures every collection has at least one directory source.
		sources = append(sources, store.CollectionSource{
			CollectionID:   c.ID,
			SourceType:     store.SourceTypeDirectory,
			SourcePath:     c.RootPath,
			IgnoreFilePath: c.IgnoreFilePath,
			Enabled:        true,
		})
	}

	seen := make(map[string]bool)
	out := make([]FileInfo, 0)

	for _, source := range sources {
		if !source.Enabled {
			continue
		}

		switch source.SourceType {
		case store.SourceTypeDirectory:
			ignorePath := DirectoryIgnoreFilePath(source.SourcePath)
			dirFiles, err := ScanMarkdownFiles(source.SourcePath, ignorePath)
			if err != nil {
				if isSourceMissing(err) {
					continue
				}
				return nil, err
			}
			for _, f := range dirFiles {
				if seen[f.AbsPath] {
					continue
				}
				seen[f.AbsPath] = true
				out = append(out, f)
			}

		case store.SourceTypeFile:
			if strings.ToLower(filepath.Ext(source.SourcePath)) != ".md" {
				continue
			}
			f, err := ScanMarkdownFile(source.SourcePath)
			if err != nil {
				if isSourceMissing(err) {
					continue
				}
				return nil, err
			}
			f.RelPath = filepath.Base(f.AbsPath)
			if seen[f.AbsPath] {
				continue
			}
			seen[f.AbsPath] = true
			out = append(out, f)

		default:
			return nil, fmt.Errorf("unknown source type %q", source.SourceType)
		}
	}

	return out, nil
}

func isSourceMissing(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist)
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
