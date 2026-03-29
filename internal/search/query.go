// Package search implements query normalization and excerpt sampling.
package search

import (
	"strings"
	"unicode"
)

// NormalizePlainQuery tokenizes user input into plain terms and returns:
// 1) ordered unique terms and 2) an FTS-safe query string using those terms.
//
// bmgrep intentionally does not expose raw FTS query syntax. This keeps agent
// behavior predictable and avoids accidental operator usage.
func NormalizePlainQuery(input string) ([]string, string) {
	terms := Tokenize(input)
	if len(terms) == 0 {
		return nil, ""
	}

	seen := make(map[string]bool, len(terms))
	unique := make([]string, 0, len(terms))
	for _, term := range terms {
		if !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}

	// FTS5 interprets space-separated terms as AND by default.
	return unique, strings.Join(unique, " ")
}

// Tokenize approximates unicode61 tokenization by extracting contiguous
// letter/digit runs and lowercasing them.
func Tokenize(input string) []string {
	var tokens []string
	var builder strings.Builder

	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String())
		builder.Reset()
	}

	for _, r := range strings.ToLower(input) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	return tokens
}
