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
		fmt.Fprintf(&b, "[%d] %s (%d lines, %d matches)\n", i+1, d.Path, d.LineCount, d.Matches)
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
