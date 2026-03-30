package ingest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

const IgnoreFileName = ".bmignore"

// DirectoryIgnoreFilePath returns the canonical ignore file path
// for a directory source root.
func DirectoryIgnoreFilePath(rootPath string) string {
	return filepath.Join(rootPath, IgnoreFileName)
}

// FileInfo represents one filesystem file candidate for indexing.
type FileInfo struct {
	AbsPath   string
	RelPath   string
	MTimeNS   int64
	SizeBytes int64
}

// ResolveSourcePath canonicalizes a filesystem source path.
// It expands symlinks where possible and always returns an absolute path.
func ResolveSourcePath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %s: %w", real, err)
	}
	return abs, nil
}

// EnsureIgnoreFile creates .bmignore if it does not already exist.
func EnsureIgnoreFile(rootPath string) (string, error) {
	path := DirectoryIgnoreFilePath(rootPath)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat ignore file: %w", err)
	}

	content := []byte("# bmgrep ignore patterns\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("create ignore file: %w", err)
	}
	return path, nil
}

// ScanMarkdownFiles walks rootPath and returns non-ignored .md files.
func ScanMarkdownFiles(rootPath, ignoreFilePath string) ([]FileInfo, error) {
	matcher, err := loadMatcher(ignoreFilePath)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	err = filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == rootPath {
			return nil
		}

		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return fmt.Errorf("build relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)

		if matcher != nil && matcher.MatchesPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("read file info %s: %w", path, err)
		}

		abs, err := ResolveSourcePath(path)
		if err != nil {
			return err
		}

		files = append(files, FileInfo{
			AbsPath:   abs,
			RelPath:   rel,
			MTimeNS:   info.ModTime().UnixNano(),
			SizeBytes: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan markdown files: %w", err)
	}

	return files, nil
}

// ScanMarkdownFile returns metadata for a single markdown file path.
func ScanMarkdownFile(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat file %s: %w", path, err)
	}
	if info.IsDir() {
		return FileInfo{}, fmt.Errorf("path %s is a directory", path)
	}
	if strings.ToLower(filepath.Ext(path)) != ".md" {
		return FileInfo{}, fmt.Errorf("path %s is not a markdown file", path)
	}

	abs, err := ResolveSourcePath(path)
	if err != nil {
		return FileInfo{}, err
	}

	return FileInfo{
		AbsPath:   abs,
		RelPath:   filepath.Base(path),
		MTimeNS:   info.ModTime().UnixNano(),
		SizeBytes: info.Size(),
	}, nil
}

func loadMatcher(ignoreFilePath string) (*ignore.GitIgnore, error) {
	if ignoreFilePath == "" {
		return nil, nil
	}
	if _, err := os.Stat(ignoreFilePath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ignore file: %w", err)
	}

	matcher, err := ignore.CompileIgnoreFile(ignoreFilePath)
	if err != nil {
		return nil, fmt.Errorf("parse ignore file: %w", err)
	}
	return matcher, nil
}
