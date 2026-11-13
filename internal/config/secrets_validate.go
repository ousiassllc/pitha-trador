package config

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// ErrEmptySecretValue is returned by NormalizeSecretValue when the value is
// empty after trimming. Removal is the explicit Delete, never a blank Save.
var ErrEmptySecretValue = errors.New("config: empty secret value")

// InvalidSecretValueError is a value NormalizeSecretValue rejects. Message is
// a fixed, user-facing Japanese sentence (never a wrapped lower-level error),
// so a handler can show it to the user as is (issue #961).
type InvalidSecretValueError struct{ Message string }

func (e *InvalidSecretValueError) Error() string { return e.Message }

// urlSecretKeys are the keys whose value must be an absolute http/https URL
// with a host (issue #235). The other allowed keys are opaque credentials.
var urlSecretKeys = []string{
	KeyJevBaseURL, KeySlackWebhookURL, KeyLunaBaseURL, KeyNewsFeedURL, KeySolBaseURL, KeyOpusBaseURL, KeyOpenRouterBaseURL,
}

// IsURLSecretKey reports whether key holds a URL rather than a credential.
func IsURLSecretKey(key string) bool {
	for _, k := range urlSecretKeys {
		if k == key {
			return true
		}
	}
	return false
}

// NormalizeSecretValue trims surrounding whitespace from a Settings value
// and validates it for key (issue #235): URL keys must parse as http/https
// with a non-empty host; every other key must not contain control
// characters (newlines/tabs pasted into an API key or password would make
// every later request fail with "invalid header field value"). It returns
// the value to store. An empty result is ErrEmptySecretValue; any other
// rejection is an *InvalidSecretValueError, whose message is safe to show the
// operator (it never echoes the value).
func NormalizeSecretValue(key, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrEmptySecretValue
	}
	if key == KeyNewsFeedEnabled {
		return normalizeSwitch(value)
	}
	if IsURLSecretKey(key) {
		if err := validateSecretURL(value); err != nil {
			return "", err
		}
		return value, nil
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return "", &InvalidSecretValueError{"改行・制御文字を含む値は保存できません。"}
	}
	return value, nil
}

// normalizeSwitch accepts "on"/"off" in any case and returns it lower-cased.
func normalizeSwitch(value string) (string, error) {
	switch v := strings.ToLower(value); v {
	case SwitchOn, SwitchOff:
		return v, nil
	}
	return "", &InvalidSecretValueError{"on または off を入力してください。"}
}

func validateSecretURL(value string) error {
	u, err := url.Parse(value)
	if err != nil {
		return &InvalidSecretValueError{"URL の形式が正しくありません。"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &InvalidSecretValueError{"URL は http:// または https:// で始めてください。"}
	}
	if u.Hostname() == "" {
		return &InvalidSecretValueError{"URL にホスト名を含めてください。"}
	}
	return nil
}
