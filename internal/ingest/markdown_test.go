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

func TestToCleanTextTildeFences(t *testing.T) {
	raw := []byte("~~~python\nprint('hello')\n~~~\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "~~~") {
		t.Fatalf("tilde fences not stripped: %q", clean)
	}
	if !strings.Contains(clean, "print('hello')") {
		t.Fatalf("tilde fence content removed: %q", clean)
	}
	if strings.Contains(clean, "python") {
		t.Fatalf("language identifier not stripped: %q", clean)
	}
}

func TestToCleanTextNestedFences(t *testing.T) {
	raw := []byte("````\n```python\ncode\n```\n````\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "````") {
		t.Fatalf("outer fence markers not stripped: %q", clean)
	}
	// Inner triple backticks are content within the 4-backtick fence
	if !strings.Contains(clean, "```python") {
		t.Fatalf("inner fence content was incorrectly stripped: %q", clean)
	}
}

func TestToCleanTextFrontmatterDotCloser(t *testing.T) {
	raw := []byte("---\ntitle: Test\n...\nBody text\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "title:") {
		t.Fatalf("frontmatter with ... closer not stripped: %q", clean)
	}
	if !strings.Contains(clean, "Body text") {
		t.Fatalf("body after frontmatter missing: %q", clean)
	}
}

func TestToCleanTextEmptyFrontmatter(t *testing.T) {
	raw := []byte("---\n---\nContent\n")
	clean := ToCleanText(raw)
	if !strings.Contains(clean, "Content") {
		t.Fatalf("content after empty frontmatter missing: %q", clean)
	}
}

func TestToCleanTextImageEmptyAlt(t *testing.T) {
	raw := []byte("![](https://example.com/img.png)\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "https://") {
		t.Fatalf("image URL not stripped: %q", clean)
	}
}

func TestToCleanTextHTMLSelfClosing(t *testing.T) {
	raw := []byte("text<br/>more<img src=\"x\"/>end\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "<br/>") || strings.Contains(clean, "<img") {
		t.Fatalf("self-closing HTML tags not stripped: %q", clean)
	}
	if !strings.Contains(clean, "text") || !strings.Contains(clean, "more") || !strings.Contains(clean, "end") {
		t.Fatalf("text content around HTML tags missing: %q", clean)
	}
}

func TestToCleanTextRefDefWithTitle(t *testing.T) {
	raw := []byte("[id]: https://example.com \"Title\"\nKeep this\n")
	clean := ToCleanText(raw)
	if strings.Contains(clean, "[id]:") {
		t.Fatalf("reference def with title not stripped: %q", clean)
	}
	if !strings.Contains(clean, "Keep this") {
		t.Fatalf("non-ref content missing: %q", clean)
	}
}

func TestToCleanTextNoFrontmatter(t *testing.T) {
	raw := []byte("# Title\nBody\n")
	clean := ToCleanText(raw)
	if !strings.Contains(clean, "# Title") || !strings.Contains(clean, "Body") {
		t.Fatalf("content without frontmatter incorrectly modified: %q", clean)
	}
}
