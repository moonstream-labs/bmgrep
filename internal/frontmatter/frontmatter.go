package frontmatter

import (
	"strconv"
	"strings"
)

// Meta is optional metadata extracted from top-of-file YAML frontmatter.
type Meta struct {
	Title       string
	Description string
	References  int
}

// Extract parses strict single-line YAML frontmatter fields from raw markdown
// content. It recognizes only top-of-file frontmatter delimited by ---/---
// (or ---/...) and extracts title, description, and references.
func Extract(raw string) Meta {
	if raw == "" {
		return Meta{}
	}

	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return Meta{}
	}

	if strings.TrimSpace(trimCR(lines[0])) != "---" {
		return Meta{}
	}

	meta := Meta{}
	foundClose := false

	for i := 1; i < len(lines); i++ {
		line := trimCR(lines[i])
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" || trimmed == "..." {
			foundClose = true
			break
		}

		key, value, ok := parseFrontmatterLine(line)
		if !ok {
			continue
		}

		value = parseFrontmatterValue(value)
		switch key {
		case "title":
			meta.Title = value
		case "description":
			meta.Description = value
		case "references":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				meta.References = n
			}
		}
	}

	if !foundClose {
		return Meta{}
	}

	return meta
}

func parseFrontmatterLine(line string) (string, string, bool) {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", false
	}

	value := strings.TrimSpace(line[idx+1:])
	return key, value, true
}

func parseFrontmatterValue(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		inner := value[1 : len(value)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		return inner
	}
	return value
}

func trimCR(line string) string {
	return strings.TrimSuffix(line, "\r")
}
