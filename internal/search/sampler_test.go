package search

import "testing"

func uniformWeights(terms []string) map[string]float64 {
	w := make(map[string]float64, len(terms))
	for _, t := range terms {
		w[t] = 1.0
	}
	return w
}

func TestExtractTopWindowsNonOverlapping(t *testing.T) {
	raw := "alpha beta\n" +
		"noise\n" +
		"alpha alpha\n" +
		"noise\n" +
		"beta alpha\n" +
		"noise\n"

	terms := []string{"alpha", "beta"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 2, 2)

	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}

	if windows[0].StartLine == windows[1].StartLine {
		t.Fatalf("windows should be distinct: %+v", windows)
	}

	if windows[0].StartLine <= windows[1].EndLine && windows[1].StartLine <= windows[0].EndLine {
		t.Fatalf("windows overlap unexpectedly: %+v", windows)
	}
}

func TestExtractTopWindowsReturnsEmptyWhenNoMatches(t *testing.T) {
	raw := "one\ntwo\nthree\n"
	terms := []string{"alpha"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 2, 2)
	if len(windows) != 0 {
		t.Fatalf("expected no windows, got %d", len(windows))
	}
}

func TestExtractTopWindowsLargerThanDoc(t *testing.T) {
	raw := "alpha beta\ngamma\n"
	terms := []string{"alpha"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 10, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window clamped to doc size, got %d", len(windows))
	}
	if len(windows[0].Lines) != 2 {
		t.Fatalf("expected 2 lines (full doc), got %d", len(windows[0].Lines))
	}
}

func TestExtractTopWindowsSingleLine(t *testing.T) {
	raw := "alpha beta\n"
	terms := []string{"alpha"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 1 || windows[0].EndLine != 1 {
		t.Fatalf("unexpected line range: %d-%d", windows[0].StartLine, windows[0].EndLine)
	}
}

func TestExtractTopWindowsDocumentOrder(t *testing.T) {
	// Line 5 has 2 matches, line 1 has 1 match. With 2 samples of 1 line,
	// the higher-scoring window should still appear second in output since
	// we sort by document position.
	raw := "alpha\nnoise\nnoise\nnoise\nalpha alpha\n"
	terms := []string{"alpha"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 2)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	if windows[0].StartLine >= windows[1].StartLine {
		t.Fatalf("windows not in document order: %d >= %d", windows[0].StartLine, windows[1].StartLine)
	}
}

func TestExtractTopWindowsCoverageTiebreaker(t *testing.T) {
	// Both windows have score 2.0 with uniform weights, but window at line 1
	// covers both terms while window at line 3 only covers "alpha" (twice).
	raw := "alpha beta\nnoise\nalpha alpha\nnoise\n"
	terms := []string{"alpha", "beta"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 2)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	// First selected by score+coverage should be line 1 (coverage=2),
	// but output is in document order, so line 1 comes first anyway.
	if windows[0].StartLine != 1 {
		t.Fatalf("expected first window at line 1 (higher coverage), got line %d", windows[0].StartLine)
	}
}

func TestExtractTopWindowsIDFWeighting(t *testing.T) {
	// "rare" has high IDF weight (5.0), "common" has low (0.1).
	// Line 1 has 3x "common" (score = 0.3), line 3 has 1x "rare" (score = 5.0).
	// With IDF, line 3 should be the top window.
	raw := "common common common\nnoise\nrare\nnoise\n"
	terms := []string{"common", "rare"}
	weights := map[string]float64{"common": 0.1, "rare": 5.0}
	windows := ExtractTopWindows(raw, terms, weights, 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 3 {
		t.Fatalf("expected IDF-weighted top window at line 3, got line %d", windows[0].StartLine)
	}
}

func TestExtractTopWindowsIgnoresLinkURLTokensForScoring(t *testing.T) {
	raw := "See [single meta variable](https://ast-grep.github.io/guide/pattern-syntax.html#meta-variable).\n" +
		"noise\n" +
		"Pattern syntax appears in prose here.\n"

	terms := []string{"pattern", "syntax"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 3 {
		t.Fatalf("expected prose line (3) to win over URL-derived tokens, got line %d", windows[0].StartLine)
	}
}

func TestExtractTopWindowsPreservesRawDisplayLines(t *testing.T) {
	raw := "[Pattern syntax guide](https://example.com/guide/pattern-syntax.html)\n" +
		"noise\n"

	terms := []string{"pattern", "syntax"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 1 {
		t.Fatalf("expected first line window, got line %d", windows[0].StartLine)
	}
	if windows[0].Lines[0] != "[Pattern syntax guide](https://example.com/guide/pattern-syntax.html)" {
		t.Fatalf("expected raw markdown line preserved, got %q", windows[0].Lines[0])
	}
}

func TestExtractTopWindowsIgnoresFrontmatterForScoring(t *testing.T) {
	raw := "---\n" +
		"title: pattern syntax\n" +
		"description: pattern syntax details\n" +
		"---\n" +
		"noise\n" +
		"Pattern syntax in prose.\n"

	terms := []string{"pattern", "syntax"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 6 {
		t.Fatalf("expected prose line (6), got line %d", windows[0].StartLine)
	}
}

func TestExtractTopWindowsIgnoresFenceMarkerLinesForScoring(t *testing.T) {
	raw := "```pattern syntax\n" +
		"code body\n" +
		"```\n" +
		"Pattern syntax in prose.\n"

	terms := []string{"pattern", "syntax"}
	windows := ExtractTopWindows(raw, terms, uniformWeights(terms), 1, 1)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].StartLine != 4 {
		t.Fatalf("expected prose line (4), got line %d", windows[0].StartLine)
	}
}
