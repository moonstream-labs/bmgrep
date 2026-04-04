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

func TestFormatRankOutputWithFallbackAndCoverage(t *testing.T) {
	out := FormatRankOutputWithOptions(
		[]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 10, Matches: 3, MatchedTerms: 1}},
		1,
		RankOutputOptions{
			Match:            MatchInfo{AutoFallback: true},
			ShowTermCoverage: true,
			QueryTermCount:   2,
		},
	)

	if !strings.Contains(out, "match: any-term fallback (auto; no all-term matches)") {
		t.Fatalf("missing fallback indicator: %q", out)
	}
	if !strings.Contains(out, "(10 lines, 3 matches, 1/2 terms)") {
		t.Fatalf("missing coverage suffix: %q", out)
	}
}

func TestFormatSampleOutputWithFallback(t *testing.T) {
	out := FormatSampleOutputWithOptions(
		[]SampleResult{{
			Path: "/tmp/a.md",
			Windows: []SampleWindow{{
				StartLine: 1,
				EndLine:   1,
				Lines:     []string{"alpha"},
			}},
		}},
		1,
		SampleOutputOptions{Match: MatchInfo{AutoFallback: true}},
	)

	if !strings.Contains(out, "match: any-term fallback (auto; no all-term matches)") {
		t.Fatalf("missing fallback indicator: %q", out)
	}
}

func TestFormatRankOutputWithDisplayPathTransform(t *testing.T) {
	out := FormatRankOutputWithOptions(
		[]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 10, Matches: 3}},
		1,
		RankOutputOptions{
			DisplayPath: func(p string) string {
				if p == "/tmp/a.md" {
					return "./a.md"
				}
				return p
			},
		},
	)

	if !strings.Contains(out, "[1] ./a.md (10 lines, 3 matches)") {
		t.Fatalf("expected transformed rank path: %q", out)
	}
}

func TestFormatSampleOutputWithDisplayPathTransform(t *testing.T) {
	out := FormatSampleOutputWithOptions(
		[]SampleResult{{
			Path:    "/tmp/a.md",
			Windows: []SampleWindow{{StartLine: 1, EndLine: 1, Lines: []string{"alpha"}}},
		}},
		1,
		SampleOutputOptions{
			DisplayPath: func(p string) string {
				if p == "/tmp/a.md" {
					return "./a.md"
				}
				return p
			},
		},
	)

	if !strings.Contains(out, "[1] ./a.md") {
		t.Fatalf("expected transformed sample path: %q", out)
	}
}

func TestFormatRankOutputWithMeta(t *testing.T) {
	out := FormatRankOutputWithOptions(
		[]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 10, Matches: 3}},
		1,
		RankOutputOptions{
			ShowMeta: true,
			MetaByPath: map[string]DocMeta{
				"/tmp/a.md": {
					Title:       "Authentication Middleware Guide",
					Description: "How to configure and customize authentication middleware.",
					Backlinks:   15,
				},
			},
		},
	)

	if !strings.Contains(out, "    title: Authentication Middleware Guide") {
		t.Fatalf("missing rank title metadata: %q", out)
	}
	if !strings.Contains(out, "    description: How to configure and customize authentication middleware.") {
		t.Fatalf("missing rank description metadata: %q", out)
	}
	if !strings.Contains(out, "    backlinks: 15") {
		t.Fatalf("missing rank backlinks metadata: %q", out)
	}
}

func TestFormatSampleOutputWithMetaShowsTitleOnly(t *testing.T) {
	out := FormatSampleOutputWithOptions(
		[]SampleResult{{
			Path:    "/tmp/a.md",
			Windows: []SampleWindow{{StartLine: 3, EndLine: 4, Lines: []string{"alpha", "beta"}}},
		}},
		1,
		SampleOutputOptions{
			ShowMeta: true,
			MetaByPath: map[string]DocMeta{
				"/tmp/a.md": {
					Title:       "Authentication Middleware Guide",
					Description: "This should not be shown in sample mode",
					Backlinks:   3,
				},
			},
		},
	)

	if !strings.Contains(out, "    title: Authentication Middleware Guide") {
		t.Fatalf("missing sample title metadata: %q", out)
	}
	if !strings.Contains(out, "    backlinks: 3") {
		t.Fatalf("missing sample backlinks metadata: %q", out)
	}
	if strings.Contains(out, "description:") {
		t.Fatalf("sample mode should not show description metadata: %q", out)
	}
}

func TestFormatMetaOmitsZeroBacklinks(t *testing.T) {
	out := FormatRankOutputWithOptions(
		[]store.RankedDoc{{Path: "/tmp/a.md", LineCount: 10, Matches: 3}},
		1,
		RankOutputOptions{
			ShowMeta: true,
			MetaByPath: map[string]DocMeta{
				"/tmp/a.md": {
					Title:     "Guide",
					Backlinks: 0,
				},
			},
		},
	)

	if strings.Contains(out, "backlinks:") {
		t.Fatalf("did not expect backlinks line for zero value: %q", out)
	}
}
