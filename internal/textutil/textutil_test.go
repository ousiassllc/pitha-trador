package textutil_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "abc", 3, "abc"},
		{"ascii cut", "abcdefgh", 6, "abc…"},
		{"multibyte boundary", "あいうえお", 10, "あい…"},
		{"tiny limit has no ellipsis", "abcdef", 2, "ab"},
		{"zero", "abc", 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := textutil.Truncate(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if len(got) > tc.max || !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q: over limit or invalid UTF-8", tc.in, tc.max, got)
			}
		})
	}
}

func TestExcerpt_CollapsesWhitespaceAndBoundsHugeInput(t *testing.T) {
	huge := "<html>\n\t<body>  " + strings.Repeat("障害ページ\n", 1<<18)
	got := textutil.Excerpt(huge, 64)
	if len(got) > 64 || !utf8.ValidString(got) {
		t.Fatalf("Excerpt len = %d valid=%v, want <= 64 valid UTF-8", len(got), utf8.ValidString(got))
	}
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("Excerpt(%q) still contains control whitespace", got)
	}
	if !strings.HasPrefix(got, "<html> <body> 障害ページ") {
		t.Fatalf("Excerpt = %q, want collapsed prefix", got)
	}
}
