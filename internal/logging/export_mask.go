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
var sensitiveKeyParts = []string{"api_key", "apikey", "api-key", "password", "passwd", "token", "secret", "authorization", "webhook"}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
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
	slackWebhookRe = regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/_\-]+`)
	bearerRe       = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	queryRe        = regexp.MustCompile(`(?i)((?:token|api[_-]?key|password)=)[^&\s"'#]+`)
)

func maskString(s string) string {
	s = slackWebhookRe.ReplaceAllLiteralString(s, redacted)
	s = bearerRe.ReplaceAllString(s, "${1}"+redacted)
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
