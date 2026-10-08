package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// sensitiveKeyParts are the (lower-case) substrings that mark an attribute
// key as secret (FR-ERRLOG-3 (a)).
var sensitiveKeyParts = []string{
	"api_key", "apikey", "api-key", "password", "passwd", "token", "secret", "authorization", "webhook",
	// 立花証券 e支店API: 認証ID・秘密鍵・第二暗証番号・仮想URL.
	"authid", "auth_id", "auth-id",
	"private_key", "privatekey", "private-key",
	"second_password", "secondpassword", "second-password", "second_pwd", "secondpwd",
	"virtual_url", "virtualurl", "virtual-url",
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	// "second" alone (第二暗証番号) and the sUrl* virtual URLs (sUrlRequest,
	// sUrlMaster, sUrlPrice, sUrlEvent, sUrlEventWebSocket) are matched
	// exactly / by prefix: a substring "second" would also hide "seconds"
	// and the like.
	if key == "second" || strings.HasPrefix(key, "surl") {
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// String-value patterns (FR-ERRLOG-3 (b)); url.Error puts the whole request
// URL into the logged error string.
var (
	slackWebhookRe = regexp.MustCompile(`(?i)https?://hooks\.slack\.com/services/[A-Za-z0-9/_\-]+`)
	bearerRe       = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	// basicAuthRe covers "Authorization: Basic <base64>" (and the other
	// non-Bearer schemes); the scheme stays, like Bearer's.
	basicAuthRe = regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*(?:basic|digest|negotiate)\s+)[^\s&"'#,;]+`)
	queryRe     = regexp.MustCompile(`(?i)((?:token|api[_-]?key|password|passwd|secret|authorization)=)[^&\s"'#]+`)

	// 立花証券 e支店API (FR-ERRLOG-3 (b)). tachibanaKeyPattern names the
	// credential fields: sAuthId / auth_id, private_key, 第二暗証番号
	// (sSecondPassword), and the sUrl* / virtual_url virtual URLs.
	tachibanaKeyPattern = `(?:auth[_-]?id|private[_-]?key|second[_-]?(?:password|pwd)|surl[a-z]*|virtual[_-]?url)`
	// tachibanaQueryRe: key=value in a URL query, "second=" included.
	tachibanaQueryRe = regexp.MustCompile(`(?i)((?:` + tachibanaKeyPattern + `|\bsecond)=)[^&\s"'#]+`)
	// tachibanaJSONRe: the JSON form that appears when a request body or
	// query is echoed into an error string, plain ("sAuthId":"v"),
	// backslash-escaped (\"sAuthId\":\"v\") or percent-encoded
	// (%22sAuthId%22%3A%22v%22). Group 1 keeps the key and quotes, group 2
	// the closing quote.
	tachibanaJSONRe = regexp.MustCompile(`(?is)(` + tachibanaKeyPattern + `(?:\\?"|%22)\s*(?::|%3A)\s*(?:\\?"|%22)).*?(\\?"|%22)`)
	// tachibanaVirtualURLRe: a virtual URL issued by login (request / master
	// / price / event / event-websocket) carries the session token in its
	// path, so the whole URL is the secret. The login URL ".../auth/" is
	// short and public, hence the 16-character floor on the token path.
	tachibanaVirtualURLRe = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[a-z0-9.\-]*e-shiten\.jp/e_api_v[0-9a-z]+/[a-z0-9_\-%=.~+/]{16,}[^\s"'<>\\]*`)
	// pemPrivateKeyRe: a PEM private key body (an unterminated one is
	// masked to the end of the string).
	pemPrivateKeyRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)
)

func maskString(s string) string {
	s = slackWebhookRe.ReplaceAllLiteralString(s, redacted)
	s = tachibanaVirtualURLRe.ReplaceAllLiteralString(s, redacted)
	s = pemPrivateKeyRe.ReplaceAllLiteralString(s, redacted)
	s = tachibanaJSONRe.ReplaceAllString(s, "${1}"+redacted+"${2}")
	s = tachibanaQueryRe.ReplaceAllString(s, "${1}"+redacted)
	s = bearerRe.ReplaceAllString(s, "${1}"+redacted)
	s = basicAuthRe.ReplaceAllString(s, "${1}"+redacted)
	return queryRe.ReplaceAllString(s, "${1}"+redacted)
}

// maskJSON rewrites one JSON document with sensitive values replaced,
// keeping key order and number text as logged.
func maskJSON(line []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var out bytes.Buffer
	if err := maskValue(dec, &out); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("logging: trailing data after JSON record")
	}
	return out.Bytes(), nil
}

func maskValue(dec *json.Decoder, out *bytes.Buffer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			return maskObject(dec, out)
		}
		return maskArray(dec, out)
	case string:
		return writeJSONString(out, maskString(v))
	case json.Number:
		out.WriteString(v.String())
	case bool:
		fmt.Fprintf(out, "%t", v)
	case nil:
		out.WriteString("null")
	}
	return nil
}

func maskObject(dec *json.Decoder, out *bytes.Buffer) error {
	out.WriteByte('{')
	for first := true; dec.More(); first = false {
		if !first {
			out.WriteByte(',')
		}
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		if err := writeJSONString(out, key); err != nil {
			return err
		}
		out.WriteByte(':')
		if isSensitiveKey(key) {
			var skipped json.RawMessage
			if err := dec.Decode(&skipped); err != nil {
				return err
			}
			out.WriteString(`"` + redacted + `"`)
			continue
		}
		if err := maskValue(dec, out); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return err
	}
	out.WriteByte('}')
	return nil
}

func maskArray(dec *json.Decoder, out *bytes.Buffer) error {
	out.WriteByte('[')
	for first := true; dec.More(); first = false {
		if !first {
			out.WriteByte(',')
		}
		if err := maskValue(dec, out); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // closing ']'
		return err
	}
	out.WriteByte(']')
	return nil
}

// writeJSONString writes s as a JSON string without HTML escaping, as slog's
// own handler does.
func writeJSONString(out *bytes.Buffer, s string) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	out.Truncate(out.Len() - 1) // Encode appends '\n'
	return nil
}
