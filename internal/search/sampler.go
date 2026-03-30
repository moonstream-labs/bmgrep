package search

import (
	"sort"
	"strings"
)

// SampleWindow is one excerpt window selected for output.
type SampleWindow struct {
	StartLine int
	EndLine   int
	Lines     []string
	Score     float64
	Coverage  int
}

// ExtractTopWindows returns up to sampleCount non-overlapping windows with the
// highest IDF-weighted query-term density in the document.
// The weights map provides IDF values per term for corpus-aware scoring.
func ExtractTopWindows(raw string, terms []string, weights map[string]float64, linesPerWindow, sampleCount int) []SampleWindow {
	if raw == "" || len(terms) == 0 || linesPerWindow <= 0 || sampleCount <= 0 {
		return nil
	}

	lines := splitDocumentLines(raw)
	if len(lines) == 0 {
		return nil
	}

	if linesPerWindow > len(lines) {
		linesPerWindow = len(lines)
	}

	termIndex := make(map[string]int, len(terms))
	termWeights := make([]float64, len(terms))
	for i, term := range terms {
		termIndex[term] = i
		termWeights[i] = weights[term]
	}

	// lineScores captures weighted token density per line for fast window scores.
	lineScores := make([]float64, len(lines))
	// termPrefix[termIdx][lineIdx] stores prefix sums of per-line term hits,
	// allowing O(len(terms)) coverage checks for any candidate window.
	termPrefix := make([][]int, len(terms))
	for i := range termPrefix {
		termPrefix[i] = make([]int, len(lines)+1)
	}

	for i, line := range lines {
		counts := make([]int, len(terms))
		for _, tok := range Tokenize(line) {
			idx, ok := termIndex[tok]
			if !ok {
				continue
			}
			counts[idx]++
			lineScores[i] += termWeights[idx]
		}
		for termIdx := range terms {
			termPrefix[termIdx][i+1] = termPrefix[termIdx][i] + counts[termIdx]
		}
	}

	// Prefix sums for weighted scores across lines.
	prefixScores := make([]float64, len(lines)+1)
	for i := range lines {
		prefixScores[i+1] = prefixScores[i] + lineScores[i]
	}

	var candidates []SampleWindow
	for start := 0; start+linesPerWindow <= len(lines); start++ {
		end := start + linesPerWindow // exclusive
		score := prefixScores[end] - prefixScores[start]
		if score == 0 {
			continue
		}

		coverage := 0
		for termIdx := range terms {
			if termPrefix[termIdx][end]-termPrefix[termIdx][start] > 0 {
				coverage++
			}
		}

		candidates = append(candidates, SampleWindow{
			StartLine: start + 1,
			EndLine:   end,
			Score:     score,
			Coverage:  coverage,
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Higher score first, then broader query-term coverage, then earlier window.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Coverage != candidates[j].Coverage {
			return candidates[i].Coverage > candidates[j].Coverage
		}
		return candidates[i].StartLine < candidates[j].StartLine
	})

	var selected []SampleWindow
	for _, c := range candidates {
		if overlapsAny(c, selected) {
			continue
		}
		selected = append(selected, c)
		if len(selected) == sampleCount {
			break
		}
	}

	// Present windows in document order regardless of score ranking.
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].StartLine < selected[j].StartLine
	})

	for i := range selected {
		start := selected[i].StartLine - 1
		end := selected[i].EndLine
		windowLines := make([]string, end-start)
		copy(windowLines, lines[start:end])
		selected[i].Lines = windowLines
	}

	return selected
}

func overlapsAny(candidate SampleWindow, selected []SampleWindow) bool {
	for _, s := range selected {
		if candidate.StartLine <= s.EndLine && s.StartLine <= candidate.EndLine {
			return true
		}
	}
	return false
}

func splitDocumentLines(raw string) []string {
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
