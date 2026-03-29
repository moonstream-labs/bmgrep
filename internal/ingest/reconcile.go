package ingest

import (
	"crypto/sha256"
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
// This is intentionally called before every search to provide correctness
// without requiring a long-running watcher process.
func ReconcileCollection(s *store.Store, c store.Collection) (ReconcileStats, error) {
	files, err := ScanMarkdownFiles(c.RootPath, c.IgnoreFilePath)
	if err != nil {
		return ReconcileStats{}, err
	}

	existing, err := s.ListDocumentsForCollection(c.ID)
	if err != nil {
		return ReconcileStats{}, err
	}

	stats := ReconcileStats{}
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
		hash := hash(rawBytes)

		if found && current.FileHash == hash {
			// Content is unchanged but metadata drifted (mtime/size), so keep existing
			// text payload and only refresh metadata fields.
			current.RelPath = file.RelPath
			current.MTimeNS = file.MTimeNS
			current.SizeBytes = file.SizeBytes
			if err := s.UpsertDocument(current); err != nil {
				return ReconcileStats{}, err
			}
			continue
		}

		lineCount := countLines(raw)
		doc := store.DocumentRecord{
			CollectionID: c.ID,
			Path:         file.AbsPath,
			RelPath:      file.RelPath,
			FileHash:     hash,
			MTimeNS:      file.MTimeNS,
			SizeBytes:    file.SizeBytes,
			LineCount:    lineCount,
			RawContent:   raw,
			CleanContent: ToCleanText(rawBytes),
		}

		if err := s.UpsertDocument(doc); err != nil {
			return ReconcileStats{}, err
		}

		if found {
			stats.Updated++
		} else {
			stats.Added++
		}
	}

	var toDelete []string
	for path := range existing {
		if !seen[path] {
			toDelete = append(toDelete, path)
		}
	}
	if err := s.DeleteDocumentsByPath(c.ID, toDelete); err != nil {
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
