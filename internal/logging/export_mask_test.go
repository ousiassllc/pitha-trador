package logging

import (
	"strings"
	"testing"
)

// FR-ERRLOG-3 (b): string values are masked by pattern, case-insensitively
// and over both http and https Slack URLs.
func TestMaskString_PatternsCaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic auth header", "Authorization: Basic dXNlcjpwYXNz end", "Authorization: Basic [REDACTED] end"},
		{"basic auth lower-case", "authorization: basic dXNlcjpwYXNz==", "authorization: basic [REDACTED]"},
		{"authorization equals basic", "Authorization=Basic dXNlcjpwYXNz", "Authorization=[REDACTED] [REDACTED]"},
		{"lower-case bearer", "authorization: bearer abc.def", "authorization: bearer [REDACTED]"},
		{"secret query", `Get "https://x/y?secret=s1&q=1"`, `Get "https://x/y?secret=[REDACTED]&q=1"`},
		{"client_secret query", "https://x/y?client_secret=s2&q=1", "https://x/y?client_secret=[REDACTED]&q=1"},
		{"authorization query", "https://x/y?authorization=a3&q=1", "https://x/y?authorization=[REDACTED]&q=1"},
		{"upper-case query keys", "https://x/y?TOKEN=t&Api_Key=k&PASSWORD=p&Secret=s&q=1", "https://x/y?TOKEN=[REDACTED]&Api_Key=[REDACTED]&PASSWORD=[REDACTED]&Secret=[REDACTED]&q=1"},
		{"passwd query", "https://x/y?passwd=pw", "https://x/y?passwd=[REDACTED]"},
		{"http slack url", "post http://hooks.slack.com/services/T0/B0/xyz failed", "post [REDACTED] failed"},
		{"https slack url", "post https://hooks.slack.com/services/T0/B0/xyz failed", "post [REDACTED] failed"},
		{"upper-case slack url", "post HTTPS://HOOKS.SLACK.COM/services/T0/B0/xyz failed", "post [REDACTED] failed"},
		{"plain text untouched", "Basic auth is not configured; secret store is empty", "Basic auth is not configured; secret store is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskString(tt.in)
			if got != tt.want {
				t.Errorf("maskString(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, "dXNlcjpwYXNz") || strings.Contains(got, "xyz") {
				t.Errorf("secret survived: %q", got)
			}
		})
	}
}
