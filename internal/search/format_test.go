package search

import (
	"strings"
	"testing"

	"github.com/moonstream-labs/bmgrep/internal/store"
)

func TestFormatRankOutput(t *testing.T) {
	out := FormatRankOutput([]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 10, Matches: 3}}, 1)
	if !strings.Contains(out, "results: 1 of 1") {
		t.Fatalf("missing results header: %q", out)
	}
	if !strings.Contains(out, "[1] /tmp/a.md (10 lines, 3 matches)") {
		t.Fatalf("unexpected rank line: %q", out)
	}
}

func TestFormatSampleOutput(t *testing.T) {
	out := FormatSampleOutput([]SampleResult{{
		Path: "/tmp/a.md",
		Windows: []SampleWindow{{
			StartLine: 3,
			EndLine:   4,
			Lines:     []string{"alpha", "beta"},
		}},
	}}, 4)

	if !strings.Contains(out, "results: 1 of 4") {
		t.Fatalf("missing results header: %q", out)
	}
	if !strings.Contains(out, "[1] /tmp/a.md") {
		t.Fatalf("missing document header: %q", out)
	}
	if !strings.Contains(out, "3-4:") {
		t.Fatalf("missing range header: %q", out)
	}
	if !strings.Contains(out, "     3\talpha") {
		t.Fatalf("missing cat -n style line: %q", out)
	}
}

func TestFormatRankOutputSingular(t *testing.T) {
	out := FormatRankOutput([]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 1, Matches: 1}}, 1)
	if !strings.Contains(out, "1 line, 1 match)") {
		t.Fatalf("expected singular 'line' and 'match': %q", out)
	}
}

func TestFormatRankOutputCommaFormatting(t *testing.T) {
	out := FormatRankOutput([]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 1509, Matches: 12345}}, 1)
	if !strings.Contains(out, "1,509 lines") {
		t.Fatalf("expected comma-formatted line count: %q", out)
	}
	if !strings.Contains(out, "12,345 matches") {
		t.Fatalf("expected comma-formatted match count: %q", out)
	}
}

func TestFormatRankOutputZeroResults(t *testing.T) {
	out := FormatRankOutput(nil, 0)
	if !strings.Contains(out, "results: 0 of 0") {
		t.Fatalf("expected zero results header: %q", out)
	}
}

func TestFormatSampleOutputMultipleDocs(t *testing.T) {
	out := FormatSampleOutput([]SampleResult{
		{Path: "/tmp/a.md", Windows: []SampleWindow{{StartLine: 1, EndLine: 1, Lines: []string{"alpha"}}}},
		{Path: "/tmp/b.md", Windows: []SampleWindow{{StartLine: 5, EndLine: 5, Lines: []string{"beta"}}}},
	}, 10)

	if !strings.Contains(out, "[1] /tmp/a.md") {
		t.Fatalf("missing first doc: %q", out)
	}
	if !strings.Contains(out, "[2] /tmp/b.md") {
		t.Fatalf("missing second doc: %q", out)
	}
	// Verify blank line between documents
	if !strings.Contains(out, "\n\n[2]") {
		t.Fatalf("expected blank line between documents: %q", out)
	}
}

func TestFormatSampleOutputZeroResults(t *testing.T) {
	out := FormatSampleOutput(nil, 0)
	if !strings.Contains(out, "results: 0 of 0") {
		t.Fatalf("expected zero results header: %q", out)
	}
}
