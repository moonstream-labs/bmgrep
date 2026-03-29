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
