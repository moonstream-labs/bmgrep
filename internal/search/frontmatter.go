package search

import "strings"

// DocMeta is optional metadata extracted from top-of-file YAML frontmatter.
type DocMeta struct {
	Title       string
	Description string
}

// ExtractFrontmatter parses strict single-line YAML frontmatter fields from raw
// markdown content. It recognizes only top-of-file frontmatter delimited by
// ---/--- (or ---/...) and currently extracts only title and description.
func ExtractFrontmatter(raw string) DocMeta {
	if raw == "" {
		return DocMeta{}
	}

	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return DocMeta{}
	}

	if strings.TrimSpace(trimCR(lines[0])) != "---" {
		return DocMeta{}
	}

	meta := DocMeta{}
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
		}
	}

	if !foundClose {
		return DocMeta{}
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
