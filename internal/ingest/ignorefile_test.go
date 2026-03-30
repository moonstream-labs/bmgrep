package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadIgnorePatternsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bmgrepignore")
	if err := os.WriteFile(path, []byte("# comment only\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patterns, err := ReadIgnorePatterns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 0 {
		t.Fatalf("expected 0 patterns, got %d: %v", len(patterns), patterns)
	}
}

func TestReadIgnorePatternsMixed(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bmgrepignore")
	content := "# comment\narchive/**\n\n# another comment\n**/draft-*.md\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	patterns, err := ReadIgnorePatterns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d: %v", len(patterns), patterns)
	}
	if patterns[0] != "archive/**" || patterns[1] != "**/draft-*.md" {
		t.Fatalf("unexpected patterns: %v", patterns)
	}
}

func TestAppendIgnorePatterns(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bmgrepignore")
	if err := os.WriteFile(path, []byte("# header\nexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AppendIgnorePatterns(path, []string{"new-pattern", "", "  "}); err != nil {
		t.Fatal(err)
	}

	patterns, err := ReadIgnorePatterns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns (existing + new-pattern), got %d: %v", len(patterns), patterns)
	}
	if patterns[1] != "new-pattern" {
		t.Fatalf("expected 'new-pattern', got %q", patterns[1])
	}
}

func TestRemoveIgnorePatterns(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bmgrepignore")
	content := "# header\nkeep-me\nremove-me\nalso-keep\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveIgnorePatterns(path, []string{"remove-me"}); err != nil {
		t.Fatal(err)
	}

	patterns, err := ReadIgnorePatterns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns after removal, got %d: %v", len(patterns), patterns)
	}
	for _, p := range patterns {
		if p == "remove-me" {
			t.Fatal("remove-me should have been removed")
		}
	}
}

func TestRemoveIgnorePatternsPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bmgrepignore")
	content := "# important comment\npattern\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveIgnorePatterns(path, []string{"pattern"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "# important comment\n" {
		t.Fatalf("unexpected file content: %q", got)
	}
}
