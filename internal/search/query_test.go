package search

import "testing"

func TestNormalizePlainQuery(t *testing.T) {
	terms, fts := NormalizePlainQuery("  Authentication middleware middleware??  ")

	if len(terms) != 2 {
		t.Fatalf("expected 2 unique terms, got %d (%v)", len(terms), terms)
	}
	if terms[0] != "authentication" || terms[1] != "middleware" {
		t.Fatalf("unexpected terms: %v", terms)
	}
	if fts != "authentication middleware" {
		t.Fatalf("unexpected fts query: %q", fts)
	}
}

func TestTokenizeUnicode(t *testing.T) {
	tokens := Tokenize("Café HTTP/2 middleware-v2")
	want := []string{"café", "http", "2", "middleware", "v2"}

	if len(tokens) != len(want) {
		t.Fatalf("expected %d tokens, got %d (%v)", len(want), len(tokens), tokens)
	}
	for i := range want {
		if tokens[i] != want[i] {
			t.Fatalf("token %d mismatch: got %q, want %q", i, tokens[i], want[i])
		}
	}
}

func TestNormalizePlainQueryEmpty(t *testing.T) {
	terms, fts := NormalizePlainQuery("")
	if len(terms) != 0 {
		t.Fatalf("expected no terms for empty input, got %v", terms)
	}
	if fts != "" {
		t.Fatalf("expected empty fts query, got %q", fts)
	}
}

func TestNormalizePlainQueryAllPunctuation(t *testing.T) {
	terms, fts := NormalizePlainQuery("!@#$%^&*()")
	if len(terms) != 0 {
		t.Fatalf("expected no terms for all-punctuation input, got %v", terms)
	}
	if fts != "" {
		t.Fatalf("expected empty fts query, got %q", fts)
	}
}

func TestNormalizePlainQueryFTSOperatorWords(t *testing.T) {
	// FTS5 operators are case-sensitive uppercase. Our normalizer lowercases
	// everything, so "NOT", "OR", "AND", "NEAR" become safe plain terms.
	terms, fts := NormalizePlainQuery("NOT OR AND NEAR")
	if len(terms) != 4 {
		t.Fatalf("expected 4 terms, got %d (%v)", len(terms), terms)
	}
	if terms[0] != "not" || terms[1] != "or" || terms[2] != "and" || terms[3] != "near" {
		t.Fatalf("FTS operator words not lowercased: %v", terms)
	}
	if fts != "not or and near" {
		t.Fatalf("unexpected fts query: %q", fts)
	}
}

func TestBuildFTSQueryAll(t *testing.T) {
	got := BuildFTSQuery([]string{"skillsbench", "decomposition"}, MatchAll)
	if got != "skillsbench decomposition" {
		t.Fatalf("unexpected all-term query: %q", got)
	}
}

func TestBuildFTSQueryAny(t *testing.T) {
	got := BuildFTSQuery([]string{"skillsbench", "decomposition"}, MatchAny)
	if got != "skillsbench OR decomposition" {
		t.Fatalf("unexpected any-term query: %q", got)
	}
}

func TestBuildFTSQueryAutoUsesAllSemantics(t *testing.T) {
	got := BuildFTSQuery([]string{"skillsbench", "decomposition"}, MatchAuto)
	if got != "skillsbench decomposition" {
		t.Fatalf("unexpected auto query: %q", got)
	}
}

func TestParseMatchMode(t *testing.T) {
	cases := []struct {
		in   string
		want MatchMode
	}{
		{in: "all", want: MatchAll},
		{in: "any", want: MatchAny},
		{in: "auto", want: MatchAuto},
		{in: "AUTO", want: MatchAuto},
	}

	for _, tc := range cases {
		got, err := ParseMatchMode(tc.in)
		if err != nil {
			t.Fatalf("ParseMatchMode(%q) returned err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMatchMode(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseMatchModeInvalid(t *testing.T) {
	if _, err := ParseMatchMode("maybe"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}
