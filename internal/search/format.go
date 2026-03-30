package search

import (
	"fmt"
	"strings"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

// FormatRankOutput renders rank-mode output using the documented contract.
func FormatRankOutput(docs []store.RankedDoc, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "results: %d of %d\n\n", len(docs), total)
	for i, d := range docs {
		fmt.Fprintf(&b, "[%d] %s (%s %s, %s %s)\n",
			i+1, d.Path,
			commaFormat(d.LineCount), pluralize(d.LineCount, "line", "lines"),
			commaFormat(d.Matches), pluralize(d.Matches, "match", "matches"),
		)
	}
	return b.String()
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func commaFormat(n int) string {
	if n < 0 {
		return "-" + commaFormat(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		b.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// SampleResult is a document plus its extracted sample windows.
type SampleResult struct {
	Path    string
	Windows []SampleWindow
}

// FormatSampleOutput renders sample-mode output with cat -n style excerpts.
func FormatSampleOutput(results []SampleResult, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "results: %d of %d\n\n", len(results), total)

	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%d] %s\n", i+1, r.Path)

		for _, w := range r.Windows {
			fmt.Fprintf(&b, "%d-%d:\n", w.StartLine, w.EndLine)
			for offset, line := range w.Lines {
				lineNo := w.StartLine + offset
				fmt.Fprintf(&b, "%6d\t%s\n", lineNo, line)
			}
		}
	}

	return b.String()
}
