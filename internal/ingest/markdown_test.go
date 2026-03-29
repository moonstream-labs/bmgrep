package ingest

import (
	"strings"
	"testing"
)

func TestToCleanText(t *testing.T) {
	raw := []byte("---\n" +
		"title: Demo\n" +
		"---\n\n" +
		"# Header\n\n" +
		"See [Config Guide](https://docs.example.com/setup/config).\n\n" +
		"![diagram](https://img.example.com/diag.png)\n\n" +
		"```go\n" +
		"fmt.Println(\"hello\")\n" +
		"```\n\n" +
		"[ref]: https://example.com/ref\n\n" +
		"<div>inline html</div>\n")

	clean := ToCleanText(raw)

	if strings.Contains(clean, "https://") {
		t.Fatalf("clean text still includes URL target: %q", clean)
	}
	if strings.Contains(clean, "```") {
		t.Fatalf("clean text still includes code fence markers: %q", clean)
	}
	if !strings.Contains(clean, `fmt.Println("hello")`) {
		t.Fatalf("clean text removed fenced code content: %q", clean)
	}
	if strings.Contains(clean, "[ref]:") {
		t.Fatalf("clean text still includes reference definitions: %q", clean)
	}
	if strings.Contains(clean, "<div>") || strings.Contains(clean, "</div>") {
		t.Fatalf("clean text still includes html tags: %q", clean)
	}
}
