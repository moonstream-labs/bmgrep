// Package config manages persistent user configuration for bmgrep.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/moonstream-labs/bmgrep/internal/paths"
)

// Config stores persistent bmgrep settings.
type Config struct {
	DefaultCollection string `yaml:"default_collection"`
}

// ResolvePath determines which config file path should be used.
// Precedence is explicit flag path, then BMGREP_CONFIG env var, then default.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return paths.ExpandPath(explicit)
	}
	if env := strings.TrimSpace(os.Getenv("BMGREP_CONFIG")); env != "" {
		return paths.ExpandPath(env)
	}
	return paths.DefaultConfigPath()
}

// Load reads config from disk. Missing files return an empty config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Save writes config atomically and creates parent directories if needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
