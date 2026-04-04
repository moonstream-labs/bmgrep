package cli

import (
	"path/filepath"
	"testing"
)

func TestDisplayPathUnderCWD(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "tmp", "workspace")
	path := filepath.Join(cwd, "docs", "guide.md")
	got := displayPath(path, cwd, false)
	if got != "./docs/guide.md" {
		t.Fatalf("displayPath under cwd: got %q", got)
	}
}

func TestDisplayPathOutsideCWDStaysAbsolute(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "tmp", "workspace")
	path := filepath.Join(string(filepath.Separator), "opt", "docs", "guide.md")
	got := displayPath(path, cwd, false)
	if got != path {
		t.Fatalf("displayPath outside cwd should stay absolute: got %q want %q", got, path)
	}
}

func TestDisplayPathSkipsRootCWD(t *testing.T) {
	path := filepath.Join(string(filepath.Separator), "home", "user", "docs", "guide.md")
	got := displayPath(path, string(filepath.Separator), false)
	if got != path {
		t.Fatalf("displayPath at root cwd should stay absolute: got %q want %q", got, path)
	}
}

func TestDisplayPathForceAbsolute(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "tmp", "workspace")
	path := filepath.Join(cwd, "docs", "guide.md")
	got := displayPath(path, cwd, true)
	if got != path {
		t.Fatalf("force absolute should bypass transform: got %q want %q", got, path)
	}
}

func TestDisplayPathNonAbsolutePassthrough(t *testing.T) {
	got := displayPath("docs/guide.md", filepath.Join(string(filepath.Separator), "tmp", "workspace"), false)
	if got != "docs/guide.md" {
		t.Fatalf("non-absolute path should pass through: got %q", got)
	}
}

func TestParseAbsolutePathsEnv(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on", "t", "Y"}
	for _, v := range truthy {
		if !parseAbsolutePathsEnv(v) {
			t.Fatalf("expected %q to be truthy", v)
		}
	}

	falsy := []string{"", "0", "false", "no", "off", "random"}
	for _, v := range falsy {
		if parseAbsolutePathsEnv(v) {
			t.Fatalf("expected %q to be falsy", v)
		}
	}
}
