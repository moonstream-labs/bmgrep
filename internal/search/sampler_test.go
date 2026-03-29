package search

import "testing"

func TestExtractTopWindowsNonOverlapping(t *testing.T) {
	raw := "alpha beta\n" +
		"noise\n" +
		"alpha alpha\n" +
		"noise\n" +
		"beta alpha\n" +
		"noise\n"

	terms := []string{"alpha", "beta"}
	windows := ExtractTopWindows(raw, terms, 2, 2)

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
	windows := ExtractTopWindows(raw, terms, 2, 2)
	if len(windows) != 0 {
		t.Fatalf("expected no windows, got %d", len(windows))
	}
}
