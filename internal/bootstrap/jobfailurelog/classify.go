package jobfailurelog

import "regexp"

// A failed job stores only its error text (jobs.last_error), so the error
// code is read back from the text the failing package wrote. Each rule maps
// one stable message shape to a code; the first matching rule wins.
var codeRules = []struct {
	re   *regexp.Regexp
	code string // $1, $2 are replaced by the capture groups
}{
	// marketdata.APIError: "marketdata: kabu station api error (http 400, code 4001007): ..."
	{regexp.MustCompile(`kabu station api error \(http \d+, code (\d+)\)`), "kabu_$1"},
	// tachibana.APIError / HTTPStatusError.
	{regexp.MustCompile(`tachibana: p_errno=(-?\d+)`), "tachibana_errno_$1"},
	{regexp.MustCompile(`tachibana: sResultCode=(\d+)`), "tachibana_result_$1"},
	{regexp.MustCompile(`tachibana: HTTP status (\d+)`), "tachibana_http_$1"},
	// jev.APIError and assist.APIError.
	{regexp.MustCompile(`(jev|assist): api error \(status (\d+)\)`), "${1}_http_$2"},
	// orphans.Fail and scheduler.safeHandle.
	{regexp.MustCompile(`^orphaned: still running`), "orphaned"},
	{regexp.MustCompile(`^handler panic:`), "handler_panic"},
	// broker sentinels.
	{regexp.MustCompile(`broker: no session established`), "broker_no_session"},
	{regexp.MustCompile(`broker: request rate limited`), "broker_rate_limited"},
	// Causes that carry no package-specific code.
	{regexp.MustCompile(`(?i)database is locked|SQLITE_BUSY|SQLITE_LOCKED`), "db_locked"},
	{regexp.MustCompile(`(?i)context deadline exceeded|i/o timeout|timed out|\btimeout\b`), "timeout"},
	{regexp.MustCompile(`(?i)connection refused|connection reset|no such host|unexpected EOF|\bEOF\b`), "network"},
}

// codeUnclassified is the code of a failure no rule matched and whose text
// has no "pkg: " prefix to name its origin.
const codeUnclassified = "unclassified"

// pkgPrefixRe is the "pkg: " the repository's error strings start with.
var pkgPrefixRe = regexp.MustCompile(`^([a-z][a-z0-9_]{0,23}): `)

// Classify returns the error code of one jobs.last_error text: a stable,
// bounded identifier (kabu_4001007, tachibana_errno_-2, jev_http_500,
// timeout, ...). Text matching no rule is "unclassified:<pkg>" when it
// starts with a "pkg: " prefix and "unclassified" otherwise, so the origin is
// still visible. The code is built from the matched pattern, never copied
// from free text, so it cannot carry a secret.
func Classify(lastError string) string {
	if lastError == "" {
		return "no_error_text"
	}
	for _, rule := range codeRules {
		if m := rule.re.FindStringSubmatchIndex(lastError); m != nil {
			return string(rule.re.ExpandString(nil, rule.code, lastError, m))
		}
	}
	if m := pkgPrefixRe.FindStringSubmatch(lastError); m != nil {
		return codeUnclassified + ":" + m[1]
	}
	return codeUnclassified
}
