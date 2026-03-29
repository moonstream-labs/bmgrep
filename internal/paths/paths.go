package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultConfigPath returns the canonical config location for bmgrep.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "bmgrep", "config.yaml"), nil
}

// DefaultDBPath returns the canonical SQLite database location for bmgrep.
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "bmgrep", "bmgrep.db"), nil
}
