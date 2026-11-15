// Package mask hides key-shaped strings in the free text of a research note
// before it is stored, so a note can never carry an API key, a token or a
// password into the database, a tool response, the audit log or the screen.
//
// It is deliberately blunt: a false positive costs a "[masked]" in a note, a
// false negative leaks a credential. It is also deliberately narrow about
// ordinary research text: GitHub URLs, file paths, snake_case identifiers,
// UUIDs and host names are left alone (see mask_words.go). The rules are pure
// and idempotent (masking masked text changes nothing), and the package
// imports only the standard library.
//
// Unicode format characters (category Cf: zero-width space and joiner, word
// joiner, BOM, soft hyphen, bidi marks) are removed first, so a credential
// cannot be split by an invisible character to dodge the rules.
package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Replacement is what every masked span becomes.
const Replacement = "[masked]"

var (
	// pemRe is a PEM private key block; an unterminated one is masked to the end.
	pemRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)
	// prefixedRe are the well-known credential prefixes: OpenAI / Anthropic
	// (sk-), Stripe (sk_live_ ...), GitHub (ghp_ gho_ ghu_ ghs_ ghr_ github_pat_),
	// Slack (xox[abprs]-), SendGrid (SG.<22>.<43>), AWS access key ids (AKIA /
	// ASIA) and Google API keys (AIza). The boundary is "not a letter or digit"
	// (not \b), so a prefix right after an underscore (foo_sk-...) still matches.
	prefixedRe = regexp.MustCompile(`(^|[^A-Za-z0-9])(?:sk-[A-Za-z0-9_-]{8,}|(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{8,}|gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}|(?:AKIA|ASIA)[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,})`)
	// jwtRe is a JWT: three base64url segments, the first two starting like a JSON object ("eyJ").
	jwtRe = regexp.MustCompile(`(^|[^A-Za-z0-9])eyJ[A-Za-z0-9_-]{3,}\.[A-Za-z0-9_-]{3,}\.[A-Za-z0-9_-]*`)
	// urlUserinfoRe is the password of https://user:password@host.
	urlUserinfoRe = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.\-]*://[^/\s:@]+:)[^/\s@]+@`)
	// bearerRe is a Bearer credential written without its header name; the scheme stays.
	bearerRe = regexp.MustCompile(`(?i)\b(bearer)([\p{Zs}\t]+)([A-Za-z0-9\-._~+/]+=*)`)
	// spacedRe is a token / secret / password written with a space and no
	// separator ("token abc12345xyz"); maskSpaced masks it only if the value is opaque.
	spacedRe = regexp.MustCompile("(?i)(^|[^A-Za-z0-9_.\\-])([A-Za-z0-9_.\\-]*(?:token|secret|password|passwd))([\\p{Zs}\\t]+)([^\\s\"'`,]{8,})")
	// dottedRe is three or more dot-separated base64url segments of 8+ characters
	// (a JWT without the eyJ header); maskDotted masks it only if every segment
	// has both a letter and a digit, so a host name like a.b-1.amazonaws.com stays.
	dottedRe = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]{8,}){2,}\b`)
)

// Text returns s with every key-shaped span replaced by Replacement.
func Text(s string) string {
	if s == "" {
		return s
	}
	s = strings.Map(dropFormat, s)
	s = pemRe.ReplaceAllLiteralString(s, Replacement)
	s = prefixedRe.ReplaceAllString(s, "${1}"+Replacement)
	s = jwtRe.ReplaceAllString(s, "${1}"+Replacement)
	s = urlUserinfoRe.ReplaceAllString(s, "${1}"+Replacement+"@")
	s = maskAssignments(s)
	s = bearerRe.ReplaceAllStringFunc(s, maskBearer)
	s = spacedRe.ReplaceAllStringFunc(s, maskSpaced)
	s = dottedRe.ReplaceAllStringFunc(s, maskDotted)
	return maskWords(s)
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

func dropFormat(r rune) rune {
	if unicode.Is(unicode.Cf, r) {
		return -1
	}
	return r
}

// maskBearer masks the token of "Bearer <token>". The capitalised scheme (the
// HTTP spelling) always masks; a lower-case "bearer" is also an English word,
// so it masks only when the token has a digit.
func maskBearer(m string) string {
	sub := bearerRe.FindStringSubmatch(m)
	if sub[1] == "bearer" && !hasDigit(sub[3]) {
		return m
	}
	return sub[1] + sub[2] + Replacement
}

// maskSpaced masks the value of "token abc12345xyz" when it is 8+ characters
// with a digit (an ordinary phrase such as "token budget" has neither).
func maskSpaced(m string) string {
	sub := spacedRe.FindStringSubmatch(m)
	if !hasDigit(sub[4]) {
		return m
	}
	return sub[1] + sub[2] + sub[3] + Replacement
}

func maskDotted(m string) string {
	for _, seg := range strings.Split(m, ".") {
		if !hasDigitAndLetter(seg) {
			return m
		}
	}
	return Replacement
}

func hasDigit(s string) bool { return strings.ContainsAny(s, "0123456789") }

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
