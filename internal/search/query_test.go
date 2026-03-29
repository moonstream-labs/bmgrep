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
