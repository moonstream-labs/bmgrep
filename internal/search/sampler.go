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

	// lineTermCounts[lineIdx][termIdx] is the number of occurrences for that term
	// within the line. We also keep a weighted total per line for fast scoring.
	lineTermCounts := make([][]int, len(lines))
	lineScores := make([]float64, len(lines))

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
		lineTermCounts[i] = counts
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
		for term := range terms {
			hits := 0
			for i := start; i < end; i++ {
				hits += lineTermCounts[i][term]
			}
			if hits > 0 {
				coverage++
			}
		}

		windowLines := make([]string, linesPerWindow)
		copy(windowLines, lines[start:end])

		candidates = append(candidates, SampleWindow{
			StartLine: start + 1,
			EndLine:   end,
			Lines:     windowLines,
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
