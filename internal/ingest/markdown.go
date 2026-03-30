// Package ingest handles filesystem scanning and markdown preprocessing.
package ingest

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

var (
	// Converts inline markdown links to visible link text.
	reInlineLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Converts image markdown to alt text only.
	reImageLink = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

	// Removes reference-style link definitions such as `[id]: https://...`.
	reReferenceDef = regexp.MustCompile(`^\s*\[[^\]]+\]:\s+\S+.*$`)

	// Removes HTML tags while preserving surrounding text.
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
)

// ToCleanText converts markdown to a cleaner BM25 indexing representation.
//
// This function intentionally avoids over-stripping. FTS5's unicode61 tokenizer
// already discards most markdown punctuation, so cleaning focuses on high-impact
// noise sources: links/URLs, reference definitions, HTML tags, frontmatter, and
// code-fence marker lines.
func ToCleanText(raw []byte) string {
	s := string(raw)
	s = stripFrontmatter(s)
	s = stripCodeFenceMarkers(s)

	var out bytes.Buffer
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := scanner.Text()

		if reReferenceDef.MatchString(line) {
			continue
		}

		line = reImageLink.ReplaceAllString(line, `$1`)
		line = reInlineLink.ReplaceAllString(line, `$1`)
		line = reHTMLTag.ReplaceAllString(line, "")

		out.WriteString(strings.TrimRight(line, " \t"))
		out.WriteByte('\n')
	}

	cleaned := strings.TrimSpace(out.String())
	if cleaned == "" {
		return ""
	}
	return cleaned + "\n"
}

// stripFrontmatter removes a YAML frontmatter block if it appears at file top.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return s
	}

	lines := splitLinesPreserveNewline(s)
	if len(lines) == 0 {
		return s
	}

	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" || trimmed == "..." {
			return strings.Join(lines[i+1:], "")
		}
	}

	return s
}

// stripCodeFenceMarkers removes fence marker lines while preserving code body.
//
// Behavior:
// - Supports both ``` and ~~~ fences.
// - Removes opening fence lines (including language info strings).
// - Removes matching closing fence lines.
// - Keeps all interior code lines untouched.
func stripCodeFenceMarkers(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	inFence := false
	fenceChar := byte(0)
	fenceLen := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inFence {
			if ch, n, ok := parseFenceStart(trimmed); ok {
				inFence = true
				fenceChar = ch
				fenceLen = n
				continue
			}
			out = append(out, line)
			continue
		}

		if isFenceClose(trimmed, fenceChar, fenceLen) {
			inFence = false
			fenceChar = 0
			fenceLen = 0
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

func parseFenceStart(trimmed string) (byte, int, bool) {
	if len(trimmed) < 3 {
		return 0, 0, false
	}
	if trimmed[0] != '`' && trimmed[0] != '~' {
		return 0, 0, false
	}

	ch := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}

	// Opening fence can include an optional language or metadata suffix.
	return ch, n, true
}

func isFenceClose(trimmed string, ch byte, minLen int) bool {
	if len(trimmed) < minLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != ch {
			return false
		}
	}
	return len(trimmed) >= minLen
}

func splitLinesPreserveNewline(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
