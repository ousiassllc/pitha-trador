package jobfailurelog

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// MaxMessageBytes caps a representative message in the log.
const MaxMessageBytes = 200

var (
	// urlRe: any scheme://... (request URLs carry tokens and virtual-URL
	// secrets).
	urlRe = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s"'<>]*`)
	// jsonBlobRe: a JSON object or array echoed from a response or request
	// body (board, price and order data); an unterminated one runs to the end.
	jsonBlobRe = regexp.MustCompile(`(?s)\{.*?(?:\}|$)|\[\s*[{"].*?(?:\]|$)`)
	// longDigitsRe: 5+ digit runs (order IDs, account numbers, prices,
	// volumes, nanosecond fractions). The error code is reported separately
	// (Classify), so the message does not need them.
	longDigitsRe = regexp.MustCompile(`\d{5,}`)
	// tokenRe: 24+ characters of an opaque credential alphabet containing a
	// digit or a mixed-case run (API keys, session tokens, base64 blobs). A
	// plain long word/path stays readable.
	tokenRe = regexp.MustCompile(`[A-Za-z0-9_\-+/=]{24,}`)
)

const (
	maskedURL    = "<url>"
	maskedJSON   = "<data>"
	maskedDigits = "<num>"
	maskedToken  = "<token>"
)

// maskMessage returns the single-line, secret-free, length-capped form of a
// jobs.last_error text for the log: known secret shapes are masked by
// logging.MaskString, then URLs, embedded JSON bodies, long digit runs and
// opaque tokens are replaced, whitespace is collapsed and the result is cut
// to MaxMessageBytes. Raw board / price data is not expected in error text;
// the replacements make sure it cannot reach the log if it appears anyway.
func maskMessage(s string) string {
	// Bound the work first: last_error is at most a few KiB, but the regexps
	// below must not be fed an unbounded string.
	s = textutil.Excerpt(s, 4*MaxMessageBytes)
	s = logging.MaskString(s)
	s = urlRe.ReplaceAllLiteralString(s, maskedURL)
	s = jsonBlobRe.ReplaceAllLiteralString(s, maskedJSON)
	s = longDigitsRe.ReplaceAllLiteralString(s, maskedDigits)
	s = tokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		if strings.ContainsAny(tok, "0123456789") || tok != strings.ToLower(tok) {
			return maskedToken
		}
		return tok
	})
	return textutil.Excerpt(s, MaxMessageBytes)
}
