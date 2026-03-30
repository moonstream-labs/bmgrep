package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moonstream-labs/bmgrep/internal/paths"
)

const (
	workspaceDirName = ".bmgrep"
	workspaceDBName  = "bmgrep.db"

	dbSourceFlag      = "flag"
	dbSourceEnv       = "env"
	dbSourceWorkspace = "workspace"
	dbSourceDefault   = "default"
)

type resolvedDBPath struct {
	DBPath    string
	DBSource  string
	Workspace string
}

func resolveDBRuntimePath(explicit string) (resolvedDBPath, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return resolvedDBPath{}, fmt.Errorf("get working directory: %w", err)
	}
	return resolveDBRuntimePathFrom(cwd, explicit)
}

func resolveDBRuntimePathFrom(cwd, explicit string) (resolvedDBPath, error) {
	if dbFlag := strings.TrimSpace(explicit); dbFlag != "" {
		path, err := paths.ExpandPath(dbFlag)
		if err != nil {
			return resolvedDBPath{}, err
		}
		return resolvedDBPath{DBPath: path, DBSource: dbSourceFlag}, nil
	}

	if env := strings.TrimSpace(os.Getenv("BMGREP_DB")); env != "" {
		path, err := paths.ExpandPath(env)
		if err != nil {
			return resolvedDBPath{}, err
		}
		return resolvedDBPath{DBPath: path, DBSource: dbSourceEnv}, nil
	}

	workspaceRoot, found, err := findWorkspaceRoot(cwd)
	if err != nil {
		return resolvedDBPath{}, err
	}
	if found {
		workspaceDB := workspaceDBPath(workspaceRoot)
		if fileExists(workspaceDB) {
			return resolvedDBPath{DBPath: workspaceDB, DBSource: dbSourceWorkspace, Workspace: workspaceRoot}, nil
		}
	}

	defaultDBPath, err := paths.DefaultDBPath()
	if err != nil {
		return resolvedDBPath{}, err
	}
	return resolvedDBPath{DBPath: defaultDBPath, DBSource: dbSourceDefault}, nil
}

func findWorkspaceRoot(startDir string) (string, bool, error) {
	start, err := paths.ExpandPath(startDir)
	if err != nil {
		return "", false, err
	}

	current := filepath.Clean(start)
	for {
		candidate := workspaceDirPath(current)
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

func workspaceDirPath(root string) string {
	return filepath.Join(root, workspaceDirName)
}

func workspaceDBPath(root string) string {
	return filepath.Join(workspaceDirPath(root), workspaceDBName)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
