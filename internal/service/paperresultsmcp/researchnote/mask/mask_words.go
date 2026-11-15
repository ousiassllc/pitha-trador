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

func maskWords(s string) string { return wordRe.ReplaceAllStringFunc(s, maskWord) }

// maskWord masks a 32+ character word when it is a credential rather than text:
//   - no separator: the run is opaque (looksOpaque);
//   - with separators: a piece of 32+ opaque characters is masked on its own, so a
//     URL or a path with a long token in it keeps its readable parts; and a word
//     that is not a URL and has a random-looking piece (mixed case with a digit)
//     or a "+" in mixed-case text is masked whole (base64 / base64url tokens).
//
// A UUID, a GitHub URL, a file path, a snake_case name and a host name have
// short, lower-case pieces and stay as they are.
func maskWord(w string) string {
	if !strings.ContainsAny(w, separators) {
		if looksOpaque(w) {
			return Replacement
		}
		return w
	}
	var out strings.Builder
	changed, randomish := false, false
	start := 0
	for i := 0; i <= len(w); i++ {
		if i < len(w) && !strings.ContainsRune(separators, rune(w[i])) {
			continue
		}
		if seg := w[start:i]; len(seg) >= 32 && looksOpaque(seg) {
			out.WriteString(Replacement)
			changed = true
		} else {
			out.WriteString(seg)
			randomish = randomish || isRandomish(seg)
		}
		if i < len(w) {
			out.WriteByte(w[i])
		}
		start = i + 1
	}
	if changed {
		return out.String()
	}
	urlLike := strings.Contains(w, "://") || strings.HasPrefix(w, "/")
	if !urlLike && looksOpaque(w) && (randomish || (strings.Contains(w, "+") && isMixedCase(w))) {
		return Replacement
	}
	return w
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
