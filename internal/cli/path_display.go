package cli

import (
	"os"
	"path/filepath"
	"strings"
)

func displayPath(path, cwd string, forceAbsolute bool) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if forceAbsolute {
		return path
	}
	if cwd == "" || cwd == string(os.PathSeparator) {
		return path
	}
	if !filepath.IsAbs(path) {
		return path
	}

	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return path
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return path
	}

	if rel == "." {
		return "./"
	}

	return "./" + rel
}

func parseAbsolutePathsEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func (a *App) displayPath(path string) string {
	if a == nil {
		return path
	}
	return displayPath(path, a.cwd(), a.AbsolutePaths)
}

func (a *App) cwd() string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.CWD) != "" {
		return a.CWD
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
