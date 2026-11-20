package mask

import (
	"regexp"
	"strings"
)

// wordRe is a run of 32+ characters of the base64 / hex alphabet plus the
// separators of URLs, paths and identifiers. maskWord decides what it is.
var wordRe = regexp.MustCompile(`[A-Za-z0-9+/=_.:~%\-]{32,}`)

// separators split a word into the human-readable pieces of a URL, a path or
// an identifier (ousiassllc/pitha-trador, jev_evaluation_horizon, host.name).
const separators = "/._-:~%"

// wordSpans are the credentials among the 32+ character words of s.
func wordSpans(s string) []span {
	var out []span
	for _, loc := range wordRe.FindAllStringIndex(s, -1) {
		for _, sp := range maskWord(s[loc[0]:loc[1]]) {
			out = append(out, span{loc[0] + sp[0], loc[0] + sp[1]})
		}
	}
	return out
}

// maskWord returns the spans of a 32+ character word that are a credential
// rather than text:
//   - no separator: the whole run is opaque (looksOpaque);
//   - with separators: a piece of 32+ opaque characters is masked on its own, so a
//     URL or a path with a long token in it keeps its readable parts; and a word
//     that is not a URL and has a random-looking piece (mixed case with a digit)
//     or a "+" in mixed-case text is masked whole (base64 / base64url tokens).
//
// A UUID, a GitHub URL, a file path, a snake_case name and a host name have
// short, lower-case pieces and stay as they are.
func maskWord(w string) []span {
	whole := []span{{0, len(w)}}
	if !strings.ContainsAny(w, separators) {
		if looksOpaque(w) {
			return whole
		}
		return nil
	}
	var pieces []span
	randomish := false
	start := 0
	for i := 0; i <= len(w); i++ {
		if i < len(w) && !strings.ContainsRune(separators, rune(w[i])) {
			continue
		}
		if seg := w[start:i]; len(seg) >= 32 && looksOpaque(seg) {
			pieces = append(pieces, span{start, i})
		} else {
			randomish = randomish || isRandomish(seg)
		}
		start = i + 1
	}
	if len(pieces) > 0 {
		return pieces
	}
	urlLike := strings.Contains(w, "://") || strings.HasPrefix(w, "/")
	if !urlLike && looksOpaque(w) && (randomish || (strings.Contains(w, "+") && isMixedCase(w))) {
		return whole
	}
	return nil
}

// looksOpaque reports whether a 32+ character run is a credential rather than
// a word: it mixes letters and digits, or mixes upper and lower case at 40+
// characters (a base64 body).
func looksOpaque(run string) bool {
	return hasDigitAndLetter(run) || (len(run) >= 40 && isMixedCase(run))
}

func isMixedCase(s string) bool { return strings.ToLower(s) != s && strings.ToUpper(s) != s }

// isRandomish: a piece of 8+ characters with upper case, lower case and a digit.
func isRandomish(seg string) bool {
	return len(seg) >= 8 && isMixedCase(seg) && hasDigit(seg)
}
