package search

import (
	"strings"

	"github.com/moonstream-labs/bmgrep/internal/frontmatter"
)

// DocMeta is optional metadata extracted from top-of-file YAML frontmatter.
type DocMeta = frontmatter.Meta

// ExtractFrontmatter parses strict single-line YAML frontmatter fields from raw
// markdown content and extracts title, description, and backlinks.
func ExtractFrontmatter(raw string) DocMeta {
	return frontmatter.Extract(raw)
}

func trimCR(line string) string {
	return strings.TrimSuffix(line, "\r")
}
