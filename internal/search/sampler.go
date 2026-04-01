package search

import (
	"regexp"
	"sort"
	"strings"
)

var (
	reScoringInlineLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reScoringImageLink    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reScoringReferenceDef = regexp.MustCompile(`^\s*\[[^\]]+\]:\s+\S+.*$`)
	reScoringHTMLTag      = regexp.MustCompile(`<[^>]+>`)
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

	rawLines := splitDocumentLines(raw)
	if len(rawLines) == 0 {
		return nil
	}

	if linesPerWindow > len(rawLines) {
		linesPerWindow = len(rawLines)
	}

	termIndex := make(map[string]int, len(terms))
	termWeights := make([]float64, len(terms))
	for i, term := range terms {
		termIndex[term] = i
		termWeights[i] = weights[term]
	}

	// lineScores captures weighted token density per line for fast window scores.
	lineScores := make([]float64, len(rawLines))
	// termPrefix[termIdx][lineIdx] stores prefix sums of per-line term hits,
	// allowing O(len(terms)) coverage checks for any candidate window.
	termPrefix := make([][]int, len(terms))
	for i := range termPrefix {
		termPrefix[i] = make([]int, len(rawLines)+1)
	}

	frontmatterEnd := frontmatterCloserLine(rawLines)
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0

	for i, line := range rawLines {
		scoringLine := line
		if frontmatterEnd >= 0 && i <= frontmatterEnd {
			scoringLine = ""
		} else {
			scoringLine = cleanLineForScoring(scoringLine, &inFence, &fenceChar, &fenceLen)
		}

		counts := make([]int, len(terms))
		for _, tok := range Tokenize(scoringLine) {
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
	prefixScores := make([]float64, len(rawLines)+1)
	for i := range rawLines {
		prefixScores[i+1] = prefixScores[i] + lineScores[i]
	}

	var candidates []SampleWindow
	for start := 0; start+linesPerWindow <= len(rawLines); start++ {
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
		copy(windowLines, rawLines[start:end])
		selected[i].Lines = windowLines
	}

	return selected
}

func frontmatterCloserLine(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	if trimCR(lines[0]) != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(trimCR(lines[i]))
		if trimmed == "---" || trimmed == "..." {
			return i
		}
	}
	return -1
}

func cleanLineForScoring(line string, inFence *bool, fenceChar *byte, fenceLen *int) string {
	trimmed := strings.TrimSpace(line)

	if !*inFence {
		if ch, n, ok := parseFenceStart(trimmed); ok {
			*inFence = true
			*fenceChar = ch
			*fenceLen = n
			return ""
		}
	} else {
		if isFenceClose(trimmed, *fenceChar, *fenceLen) {
			*inFence = false
			*fenceChar = 0
			*fenceLen = 0
			return ""
		}
	}

	if reScoringReferenceDef.MatchString(line) {
		return ""
	}

	line = reScoringImageLink.ReplaceAllString(line, `$1`)
	line = reScoringInlineLink.ReplaceAllString(line, `$1`)
	line = reScoringHTMLTag.ReplaceAllString(line, "")
	return strings.TrimRight(line, " \t")
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
