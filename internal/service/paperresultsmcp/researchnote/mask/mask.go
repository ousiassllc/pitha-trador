// Package mask hides key-shaped strings in the free text of a research note
// before it is stored, so a note can never carry an API key, a token or a
// password into the database, a tool response, the audit log or the screen.
//
// It is deliberately blunt: a false positive (a long opaque-looking slug) costs
// a "[masked]" in a note, a false negative leaks a credential. The rules are
// pure and idempotent (masking masked text changes nothing), and the package
// imports only the standard library.
package mask

import (
	"regexp"
	"strings"
)

// Replacement is what every masked span becomes.
const Replacement = "[masked]"

var (
	// pemRe is a PEM private key block; an unterminated one is masked to the end.
	pemRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)
	// prefixedRe are the well-known credential prefixes: OpenAI / Anthropic
	// (sk-), GitHub (ghp_ gho_ ghu_ ghs_ ghr_ github_pat_), Slack (xox?-),
	// AWS access key ids (AKIA / ASIA), Google API keys (AIza), Stripe (sk_live_ ...).
	prefixedRe = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}|(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{8,}|gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{8,}|(?:AKIA|ASIA)[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,})`)
	// bearerRe is an Authorization scheme and its token; the scheme stays.
	bearerRe = regexp.MustCompile(`(?i)\b(Bearer|Basic|Digest|Negotiate)\s+[A-Za-z0-9\-._~+/]+=*`)
	// jwtRe is a JWT: three base64url segments, the first two starting like a JSON object ("eyJ").
	jwtRe = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{3,}\.[A-Za-z0-9_-]{3,}\.[A-Za-z0-9_-]*`)
	// urlUserinfoRe is the password of https://user:password@host.
	urlUserinfoRe = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.\-]*://[^/\s:@]+:)[^/\s@]+@`)
	// assignmentRe is a credential-named key with its value: api_key=..., "password": "...",
	// token: ..., Authorization: ... The key and the separator stay; the value
	// (and a scheme word in front of it) is masked.
	assignmentRe = regexp.MustCompile(`(?i)(\b[\w.\-]*(?:api[_\-]?key|secret|passw(?:or)?d|passphrase|pwd|token|credential|private[_\-]?key|authorization|auth[_\-]?id)[\w.\-]*["']?\s*[:=]\s*["']?)(?:(?:Bearer|Basic|Digest)\s+)?[^\s"',;&]+`)
	// dottedRe is three or more dot-separated base64url segments of 8+ characters
	// that include a digit (a JWT without the eyJ header; a plain dotted name has no digit).
	dottedRe = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]{8,}){2,}\b`)
	// runRe is a long run of the base64 / hex alphabet; looksOpaque decides if it is a credential.
	runRe = regexp.MustCompile(`[A-Za-z0-9+/=_-]{32,}`)
)

// Text returns s with every key-shaped span replaced by Replacement.
func Text(s string) string {
	if s == "" {
		return s
	}
	s = pemRe.ReplaceAllLiteralString(s, Replacement)
	s = prefixedRe.ReplaceAllLiteralString(s, Replacement)
	s = jwtRe.ReplaceAllLiteralString(s, Replacement)
	s = urlUserinfoRe.ReplaceAllString(s, "${1}"+Replacement+"@")
	s = bearerRe.ReplaceAllString(s, "${1} "+Replacement)
	s = assignmentRe.ReplaceAllString(s, "${1}"+Replacement)
	s = dottedRe.ReplaceAllStringFunc(s, func(m string) string {
		if hasDigitAndLetter(m) {
			return Replacement
		}
		return m
	})
	return runRe.ReplaceAllStringFunc(s, func(m string) string {
		if looksOpaque(m) {
			return Replacement
		}
		return m
	})
}

// Texts masks each element of in, returning a new slice.
func Texts(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = Text(s)
	}
	return out
}

// Contains reports whether Text would change s.
func Contains(s string) bool { return Text(s) != s }

func hasDigitAndLetter(s string) bool {
	digit, letter := false, false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter = true
		}
	}
	return digit && letter
}

// looksOpaque reports whether a 32+ character run is a credential rather than
// a word or a path: it mixes letters and digits, or mixes upper and lower case
// at 40+ characters (a base64 body). A run that is already a mask is kept.
func looksOpaque(run string) bool {
	if strings.Contains(run, Replacement) {
		return false
	}
	if hasDigitAndLetter(run) {
		return true
	}
	return len(run) >= 40 && strings.ToLower(run) != run && strings.ToUpper(run) != run
}
