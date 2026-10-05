// Package textutil bounds the size of text taken from untrusted or
// unbounded sources (external API response bodies, error chains) before it
// is stored in a database column or written to a log.
package textutil

import (
	"strings"
	"unicode/utf8"
)

// ErrorBodyExcerptBytes caps how much of an external API's non-200 response
// body an error message may carry.
const ErrorBodyExcerptBytes = 256

// ellipsis marks a truncated string. It is 3 bytes in UTF-8.
const ellipsis = "…"

// Truncate returns s unchanged when it fits in maxBytes bytes. Otherwise it
// returns a prefix ending in "…" whose total length is at most maxBytes,
// cut on a UTF-8 rune boundary. A maxBytes too small for the ellipsis
// yields a plain prefix without it.
func Truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return ""
	}
	suffix := ellipsis
	if maxBytes < len(suffix) {
		suffix = ""
	}
	cut := maxBytes - len(suffix)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// Excerpt collapses every whitespace run in s (newlines included) to a
// single space, trims it and Truncates the result to maxBytes, so that a
// multi-line HTML error page becomes a short single-line fragment safe to
// embed in an error message.
func Excerpt(s string, maxBytes int) string {
	// Fields allocates per word; bound the input first so a multi-MiB body
	// is not scanned in full. 4x leaves room for whitespace collapsing.
	if limit := maxBytes * 4; limit > 0 && len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return Truncate(strings.Join(strings.Fields(s), " "), maxBytes)
}
