package search

import "testing"

func TestExtractFrontmatterStandard(t *testing.T) {
	raw := "---\n" +
		"title: Quick Start | ast-grep\n" +
		"description: Learn how to install ast-grep and use it quickly.\n" +
		"source_url: https://ast-grep.github.io/guide/quick-start.html\n" +
		"---\n\n" +
		"# Body\n"

	meta := ExtractFrontmatter(raw)
	if meta.Title != "Quick Start | ast-grep" {
		t.Fatalf("title mismatch: got %q", meta.Title)
	}
	if meta.Description != "Learn how to install ast-grep and use it quickly." {
		t.Fatalf("description mismatch: got %q", meta.Description)
	}
}

func TestExtractFrontmatterQuotedValues(t *testing.T) {
	raw := "---\n" +
		"title: \"Quick Start: \\\"Getting Things Done\\\"\"\n" +
		"description: \"Install and refactor code safely\"\n" +
		"---\n\n" +
		"content\n"

	meta := ExtractFrontmatter(raw)
	if meta.Title != `Quick Start: "Getting Things Done"` {
		t.Fatalf("quoted title mismatch: got %q", meta.Title)
	}
	if meta.Description != "Install and refactor code safely" {
		t.Fatalf("quoted description mismatch: got %q", meta.Description)
	}
}

func TestExtractFrontmatterDotCloser(t *testing.T) {
	raw := "---\n" +
		"title: Dot closer\n" +
		"description: Uses YAML dot closer\n" +
		"...\n" +
		"body\n"

	meta := ExtractFrontmatter(raw)
	if meta.Title != "Dot closer" {
		t.Fatalf("title mismatch: got %q", meta.Title)
	}
	if meta.Description != "Uses YAML dot closer" {
		t.Fatalf("description mismatch: got %q", meta.Description)
	}
}

func TestExtractFrontmatterCRLF(t *testing.T) {
	raw := "---\r\n" +
		"title: Windows\r\n" +
		"description: Line endings\r\n" +
		"---\r\n" +
		"content\r\n"

	meta := ExtractFrontmatter(raw)
	if meta.Title != "Windows" {
		t.Fatalf("title mismatch: got %q", meta.Title)
	}
	if meta.Description != "Line endings" {
		t.Fatalf("description mismatch: got %q", meta.Description)
	}
}

func TestExtractFrontmatterNoFrontmatter(t *testing.T) {
	meta := ExtractFrontmatter("# Header\nno frontmatter\n")
	if meta != (DocMeta{}) {
		t.Fatalf("expected empty meta, got %+v", meta)
	}
}

func TestExtractFrontmatterMissingCloserReturnsEmpty(t *testing.T) {
	raw := "---\n" +
		"title: Incomplete\n" +
		"description: Missing closer\n" +
		"body without closing delimiter\n"

	meta := ExtractFrontmatter(raw)
	if meta != (DocMeta{}) {
		t.Fatalf("expected empty meta when frontmatter is unterminated, got %+v", meta)
	}
}

func TestExtractFrontmatterReferences(t *testing.T) {
	raw := "---\n" +
		"title: Pattern Syntax\n" +
		"references: 15\n" +
		"---\n\n" +
		"content\n"

	meta := ExtractFrontmatter(raw)
	if meta.Title != "Pattern Syntax" {
		t.Fatalf("title mismatch: got %q", meta.Title)
	}
	if meta.References != 15 {
		t.Fatalf("references mismatch: got %d want 15", meta.References)
	}
}

func TestExtractFrontmatterReferencesQuotedValue(t *testing.T) {
	raw := "---\n" +
		"references: \"7\"\n" +
		"---\n\n" +
		"content\n"

	meta := ExtractFrontmatter(raw)
	if meta.References != 7 {
		t.Fatalf("quoted references mismatch: got %d want 7", meta.References)
	}
}

func TestExtractFrontmatterReferencesInvalidOrNonPositive(t *testing.T) {
	cases := []string{
		"---\nreferences: many\n---\n",
		"---\nreferences: 0\n---\n",
		"---\nreferences: -2\n---\n",
	}

	for _, raw := range cases {
		meta := ExtractFrontmatter(raw)
		if meta.References != 0 {
			t.Fatalf("expected references=0 for %q, got %d", raw, meta.References)
		}
	}
}
