package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath resolves ~ prefixes and returns an absolute path.
func ExpandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}

// DefaultDBPath returns the canonical SQLite database location for bmgrep.
func DefaultDBPath() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "bmgrep", "bmgrep.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "bmgrep", "bmgrep.db"), nil
}
