package mask

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// assignRe finds a key and its separator: an identifier-shaped key (api_key,
// "password", \"token\", X-Api-Key ...) followed by : = ： ＝ (an optional
// closing quote, plain or JSON-escaped, may sit between), or one of the
// Japanese key words followed by those or は. valueSpan then finds the value.
var assignRe = regexp.MustCompile("(?i)(?:([A-Za-z0-9_.\\-]+)(\\\\?[\"'`]?[\\p{Zs}\\t]*[:=：＝][\\p{Zs}\\t]*)|(パスワード|トークン|秘密鍵|シークレット|秘密|API ?キー|認証)([\\p{Zs}\\t]*(?:[:=：＝]|は)[\\p{Zs}\\t]*))")

var (
	// schemeRe is an authorization scheme in front of a value (token: Bearer abc).
	schemeRe = regexp.MustCompile(`(?i)^(?:Bearer|Basic|Digest|Token|ApiKey)[\p{Zs}\t]+`)
	// queryParamRe is the start of the next query parameter (&name=): an
	// unquoted value ends there so ?token=zzz&b=2 keeps b=2.
	queryParamRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*=`)
)

// Key words, compared with the key lower-cased and stripped of every
// character but letters and digits (api_key, ApiKey and API-KEY all become
// "apikey"). max_tokens ("maxtokens") and token_count ("tokencount") match
// none of them, so they stay readable.
var (
	// keyEndings: the key ends with one of these.
	keyEndings = []string{"token", "secret", "secrets", "password", "passwords", "passwd", "passphrase", "credential", "credentials", "authorization", "cookie", "cookies", "signature"}
	// keyParts: the key contains one of these (secret_key_base, openai_api_key_2).
	keyParts = []string{"apikey", "accesskey", "secretkey", "privatekey"}
	// keySegments: short words that count only as a whole last segment (or the
	// last two joined): pass_rate or bypass must not match.
	keySegments = map[string]bool{"pass": true, "pw": true, "pwd": true, "auth": true, "session": true, "sessionid": true, "authid": true}
)

// maskAssignments masks the value of every credential-named key=value.
func maskAssignments(s string) string {
	var b strings.Builder
	pos := 0
	for pos < len(s) {
		loc := assignRe.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += pos
			}
		}
		from := loc[1]
		start, end := from, from
		if loc[2] >= 0 { // identifier key
			if key := s[loc[2]:loc[3]]; isCredentialKey(key) {
				start, end = valueSpan(s, loc[2], from, strings.HasSuffix(normalize(key), "authorization"))
			}
		} else { // Japanese key word
			start, end = valueSpan(s, loc[6], from, false)
			if strings.Contains(s[loc[8]:loc[9]], "は") && !opaque(s[start:end]) {
				start, end = from, from
			}
		}
		if end == start {
			b.WriteString(s[pos:from])
			pos = from
			continue
		}
		b.WriteString(s[pos:start])
		b.WriteString(Replacement)
		pos = end
	}
	b.WriteString(s[pos:])
	return b.String()
}

// opaque: 8+ characters with a digit, the shape of a secret written after "は".
func opaque(v string) bool { return len(v) >= 8 && hasDigit(v) }

// valueSpan returns the span of the value that starts at from (the key starts
// at keyStart). A value that opens with a quote (" ' ` or the JSON-escaped
// \" \') runs to the closing quote on the same line, spaces included.
// Otherwise an Authorization value runs to the end of the line (see authEnd),
// and any other value to the next white space, quote, comma or 、。 (an
// optional scheme word and the token after it count as one value). An empty
// value has an empty span.
func valueSpan(s string, keyStart, from int, toEOL bool) (int, int) {
	v := s[from:]
	lineEnd := len(s)
	if i := strings.IndexAny(v, "\r\n"); i >= 0 {
		lineEnd = from + i
	}
	switch {
	case strings.HasPrefix(v, `\"`) || strings.HasPrefix(v, `\'`):
		end := lineEnd
		if i := strings.Index(s[from+2:lineEnd], v[:2]); i >= 0 {
			end = from + 2 + i
		}
		return from + 2, end
	case v != "" && strings.ContainsRune("\"'`", rune(v[0])):
		end := lineEnd
		for i := from + 1; i < lineEnd; i++ {
			if s[i] == '\\' {
				i++
			} else if s[i] == v[0] {
				end = i
				break
			}
		}
		return from + 1, end
	case toEOL:
		return from, authEnd(s, keyStart, from, lineEnd)
	}
	end := valueEnd(s, from)
	if m := schemeRe.FindString(v); m != "" {
		if next := valueEnd(s, from+len(m)); next > from+len(m) {
			end = next
		}
	}
	return from, end
}

// authEnd is the end of an unquoted Authorization value: the end of the line,
// or an earlier quote that closes a quote opened before the key on the same line
// (curl -H 'Authorization: Bearer x' ...), so the rest of the command survives.
func authEnd(s string, keyStart, from, lineEnd int) int {
	before := s[strings.LastIndexAny(s[:keyStart], "\r\n")+1 : keyStart]
	end := lineEnd
	for _, q := range []string{`"`, `'`} {
		if strings.Count(before, q)%2 == 1 {
			if i := strings.Index(s[from:end], q); i >= 0 {
				end = from + i
			}
		}
	}
	return end
}

// valueEnd is the end of the unquoted value that starts at from.
func valueEnd(s string, from int) int {
	for i := from; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) || strings.ContainsRune("\"'`,、。", r) || (r == '&' && queryParamRe.MatchString(s[i+1:])) {
			return i
		}
		i += size
	}
	return len(s)
}

// isCredentialKey reports whether key (api_key, X-Api-Key, db.password,
// sessionId ...) names a credential.
func isCredentialKey(key string) bool {
	norm := normalize(key)
	for _, w := range keyEndings {
		if strings.HasSuffix(norm, w) {
			return true
		}
	}
	for _, w := range keyParts {
		if strings.Contains(norm, w) {
			return true
		}
	}
	segs := splitKey(key)
	last := segs[len(segs)-1]
	if keySegments[last] {
		return true
	}
	return len(segs) > 1 && keySegments[segs[len(segs)-2]+last]
}

// normalize lower-cases key and keeps only its letters and digits.
func normalize(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return -1
	}, key)
}

// splitKey splits key into lower-case words at every non-alphanumeric
// character and at a lower-to-upper camelCase boundary (sessionId -> session, id).
func splitKey(key string) []string {
	var segs []string
	start := 0
	for i := 1; i <= len(key); i++ {
		boundary := i == len(key) || !isAlnum(key[i]) || (key[i] >= 'A' && key[i] <= 'Z' && !isUpper(key[i-1]))
		if !boundary {
			continue
		}
		if seg := normalize(key[start:i]); seg != "" {
			segs = append(segs, seg)
		}
		start = i
	}
	if len(segs) == 0 {
		return []string{""}
	}
	return segs
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
