package dbprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/moonstream-labs/bmgrep/internal/paths"
)

const (
	WorkspaceDirName   = ".bmgrep"
	WorkspaceDBName    = "bmgrep.db"
	WorkspaceConfig    = "config.yaml"
	RegistryFileName   = "databases.yaml"
	ScopeLocal         = "local"
	ScopeGlobal        = "global"
	SourceFlag         = "flag"
	SourceEnv          = "env"
	SourceLocalActive  = "local-registry-active"
	SourceWorkspace    = "workspace"
	SourceGlobalActive = "global-registry-active"
	SourceDefault      = "default"
)

// DatabaseEntry defines a named database/config profile.
type DatabaseEntry struct {
	Name          string `yaml:"name,omitempty"`
	DBPath        string `yaml:"db_path"`
	ConfigPath    string `yaml:"config_path,omitempty"`
	WorkspaceRoot string `yaml:"workspace_root,omitempty"`
	Scope         string `yaml:"scope,omitempty"`
	CreatedAt     string `yaml:"created_at,omitempty"`
	LastUsedAt    string `yaml:"last_used_at,omitempty"`
}

// Registry stores local or global known database profiles.
type Registry struct {
	Active    string          `yaml:"active,omitempty"`
	Databases []DatabaseEntry `yaml:"databases,omitempty"`
}

// ResolvedPaths captures runtime db/config resolution.
type ResolvedPaths struct {
	DBPath       string
	ConfigPath   string
	DBSource     string
	ConfigSource string
	Workspace    string
}

func WorkspacePath(root string) string {
	return filepath.Join(root, WorkspaceDirName)
}

func WorkspaceDBPath(root string) string {
	return filepath.Join(WorkspacePath(root), WorkspaceDBName)
}

func WorkspaceConfigPath(root string) string {
	return filepath.Join(WorkspacePath(root), WorkspaceConfig)
}

func LocalRegistryPath(root string) string {
	return filepath.Join(WorkspacePath(root), RegistryFileName)
}

func GlobalRegistryPath() (string, error) {
	cfgPath, err := paths.DefaultConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), RegistryFileName), nil
}

func FindWorkspaceRoot(startDir string) (string, bool, error) {
	start, err := paths.ExpandPath(startDir)
	if err != nil {
		return "", false, err
	}

	current := filepath.Clean(start)
	for {
		candidate := WorkspacePath(current)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return current, true, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", false, fmt.Errorf("stat workspace path: %w", err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return &reg, nil
}

func SaveRegistry(path string, reg *Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}

	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

func ResolvePaths(explicitConfig, explicitDB string) (ResolvedPaths, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ResolvedPaths{}, fmt.Errorf("get working directory: %w", err)
	}
	return ResolvePathsFrom(cwd, explicitConfig, explicitDB)
}

func ResolvePathsFrom(cwd, explicitConfig, explicitDB string) (ResolvedPaths, error) {
	var out ResolvedPaths

	workspaceRoot, foundWorkspace, err := FindWorkspaceRoot(cwd)
	if err != nil {
		return out, err
	}
	if foundWorkspace {
		out.Workspace = workspaceRoot
	}

	var localReg *Registry
	if foundWorkspace {
		localReg, err = LoadRegistry(LocalRegistryPath(workspaceRoot))
		if err != nil {
			return out, err
		}
	}

	globalRegPath, err := GlobalRegistryPath()
	if err != nil {
		return out, err
	}
	globalReg, err := LoadRegistry(globalRegPath)
	if err != nil {
		return out, err
	}

	if strings.TrimSpace(explicitDB) != "" {
		out.DBPath, err = paths.ExpandPath(explicitDB)
		if err != nil {
			return out, err
		}
		out.DBSource = SourceFlag
	} else if env := strings.TrimSpace(os.Getenv("BMGREP_DB")); env != "" {
		out.DBPath, err = paths.ExpandPath(env)
		if err != nil {
			return out, err
		}
		out.DBSource = SourceEnv
	} else if entry, ok, err := activeEntry(localReg); err != nil {
		return out, err
	} else if ok {
		out.DBPath = entry.DBPath
		out.DBSource = SourceLocalActive
	} else if foundWorkspace && fileExists(WorkspaceDBPath(workspaceRoot)) {
		out.DBPath = WorkspaceDBPath(workspaceRoot)
		out.DBSource = SourceWorkspace
	} else if entry, ok, err := activeEntry(globalReg); err != nil {
		return out, err
	} else if ok {
		out.DBPath = entry.DBPath
		out.DBSource = SourceGlobalActive
	} else {
		out.DBPath, err = paths.DefaultDBPath()
		if err != nil {
			return out, err
		}
		out.DBSource = SourceDefault
	}

	if strings.TrimSpace(explicitConfig) != "" {
		out.ConfigPath, err = paths.ExpandPath(explicitConfig)
		if err != nil {
			return out, err
		}
		out.ConfigSource = SourceFlag
	} else if env := strings.TrimSpace(os.Getenv("BMGREP_CONFIG")); env != "" {
		out.ConfigPath, err = paths.ExpandPath(env)
		if err != nil {
			return out, err
		}
		out.ConfigSource = SourceEnv
	} else if entry, ok, err := activeEntry(localReg); err != nil {
		return out, err
	} else if ok && strings.TrimSpace(entry.ConfigPath) != "" {
		out.ConfigPath = entry.ConfigPath
		out.ConfigSource = SourceLocalActive
	} else if foundWorkspace && fileExists(WorkspaceConfigPath(workspaceRoot)) {
		out.ConfigPath = WorkspaceConfigPath(workspaceRoot)
		out.ConfigSource = SourceWorkspace
	} else if entry, ok, err := activeEntry(globalReg); err != nil {
		return out, err
	} else if ok && strings.TrimSpace(entry.ConfigPath) != "" {
		out.ConfigPath = entry.ConfigPath
		out.ConfigSource = SourceGlobalActive
	} else {
		out.ConfigPath, err = paths.DefaultConfigPath()
		if err != nil {
			return out, err
		}
		out.ConfigSource = SourceDefault
	}

	return out, nil
}

func activeEntry(reg *Registry) (DatabaseEntry, bool, error) {
	if reg == nil || strings.TrimSpace(reg.Active) == "" || len(reg.Databases) == 0 {
		return DatabaseEntry{}, false, nil
	}

	target := strings.TrimSpace(reg.Active)
	for _, db := range reg.Databases {
		if db.Name != "" && db.Name == target {
			norm, err := normalizeEntry(db)
			if err != nil {
				return DatabaseEntry{}, false, err
			}
			return norm, true, nil
		}
	}
	for _, db := range reg.Databases {
		norm, err := normalizeEntry(db)
		if err != nil {
			return DatabaseEntry{}, false, err
		}
		if norm.DBPath == target {
			return norm, true, nil
		}
	}
	return DatabaseEntry{}, false, nil
}

func normalizeEntry(entry DatabaseEntry) (DatabaseEntry, error) {
	if strings.TrimSpace(entry.DBPath) == "" {
		return DatabaseEntry{}, fmt.Errorf("registry entry missing db_path")
	}

	dbPath, err := paths.ExpandPath(entry.DBPath)
	if err != nil {
		return DatabaseEntry{}, err
	}
	entry.DBPath = dbPath

	if strings.TrimSpace(entry.ConfigPath) != "" {
		configPath, err := paths.ExpandPath(entry.ConfigPath)
		if err != nil {
			return DatabaseEntry{}, err
		}
		entry.ConfigPath = configPath
	}

	if strings.TrimSpace(entry.WorkspaceRoot) != "" {
		root, err := paths.ExpandPath(entry.WorkspaceRoot)
		if err != nil {
			return DatabaseEntry{}, err
		}
		entry.WorkspaceRoot = root
	}

	return entry, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func RegisterDatabase(reg *Registry, entry DatabaseEntry) (DatabaseEntry, error) {
	if reg == nil {
		return DatabaseEntry{}, fmt.Errorf("nil registry")
	}

	norm, err := normalizeEntry(entry)
	if err != nil {
		return DatabaseEntry{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range reg.Databases {
		existing, err := normalizeEntry(reg.Databases[i])
		if err != nil {
			return DatabaseEntry{}, err
		}
		if norm.Name != "" && existing.Name != "" && norm.Name == existing.Name && norm.DBPath != existing.DBPath {
			return DatabaseEntry{}, fmt.Errorf("database name %q already used for %s", norm.Name, existing.DBPath)
		}
		if norm.DBPath == existing.DBPath {
			norm.CreatedAt = existing.CreatedAt
			if norm.CreatedAt == "" {
				norm.CreatedAt = now
			}
			norm.LastUsedAt = existing.LastUsedAt
			reg.Databases[i] = norm
			return norm, nil
		}
	}

	if norm.CreatedAt == "" {
		norm.CreatedAt = now
	}
	reg.Databases = append(reg.Databases, norm)
	return norm, nil
}

func UseDatabase(reg *Registry, target string) (DatabaseEntry, error) {
	if reg == nil {
		return DatabaseEntry{}, fmt.Errorf("nil registry")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return DatabaseEntry{}, fmt.Errorf("database target cannot be empty")
	}

	for i := range reg.Databases {
		db, err := normalizeEntry(reg.Databases[i])
		if err != nil {
			return DatabaseEntry{}, err
		}
		if db.Name == target || db.DBPath == target {
			db.LastUsedAt = time.Now().UTC().Format(time.RFC3339)
			reg.Databases[i] = db
			if db.Name != "" {
				reg.Active = db.Name
			} else {
				reg.Active = db.DBPath
			}
			return db, nil
		}
	}

	expanded, err := paths.ExpandPath(target)
	if err == nil {
		for i := range reg.Databases {
			db, err := normalizeEntry(reg.Databases[i])
			if err != nil {
				return DatabaseEntry{}, err
			}
			if db.DBPath == expanded {
				db.LastUsedAt = time.Now().UTC().Format(time.RFC3339)
				reg.Databases[i] = db
				if db.Name != "" {
					reg.Active = db.Name
				} else {
					reg.Active = db.DBPath
				}
				return db, nil
			}
		}
	}

	return DatabaseEntry{}, fmt.Errorf("database %q not found", target)
}

func UnregisterDatabase(reg *Registry, target string) (DatabaseEntry, bool, error) {
	if reg == nil {
		return DatabaseEntry{}, false, fmt.Errorf("nil registry")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return DatabaseEntry{}, false, fmt.Errorf("database target cannot be empty")
	}

	expanded, _ := paths.ExpandPath(target)

	for i := range reg.Databases {
		db, err := normalizeEntry(reg.Databases[i])
		if err != nil {
			return DatabaseEntry{}, false, err
		}
		if db.Name == target || db.DBPath == target || (expanded != "" && db.DBPath == expanded) {
			reg.Databases = append(reg.Databases[:i], reg.Databases[i+1:]...)
			if reg.Active == db.Name || reg.Active == db.DBPath || reg.Active == target || reg.Active == expanded {
				reg.Active = ""
			}
			return db, true, nil
		}
	}

	return DatabaseEntry{}, false, nil
}
