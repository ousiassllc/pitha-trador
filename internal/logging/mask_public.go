package logging

import "encoding/json"

// The data export (internal/service/dataexport) reuses the log export's
// masking rules (FR-ERRLOG-3) so a secret is hidden the same way in every
// file leaving the application.

// IsSensitiveKey reports whether an attribute / column / JSON object key
// names a secret (token, password, API key, 第二暗証番号, 立花証券 credentials,
// ...), whose value must never be exported.
func IsSensitiveKey(key string) bool { return isSensitiveKey(key) }

// RedactedValue is the placeholder that replaces a masked value.
const RedactedValue = redacted

// MaskString returns s with the secrets embedded in free text (Bearer
// tokens, query-string credentials, webhook / virtual URLs, PEM private
// keys, ...) replaced by RedactedValue.
func MaskString(s string) string { return maskString(s) }

// MaskJSONText masks one JSON document held in a string: values under
// sensitive keys and secrets inside string values are replaced, key order
// and number text are kept. Text that is not a single valid JSON document
// is masked as plain text by MaskString instead, so it is never passed
// through unmasked.
func MaskJSONText(s string) string {
	if !json.Valid([]byte(s)) {
		return maskString(s)
	}
	masked, err := maskJSON([]byte(s))
	if err != nil {
		return maskString(s)
	}
	return string(masked)
}
