package jobfailurelog_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jobfailurelog"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"kabu code", "marketdata: kabu station api error (http 400, code 4001007): login", "kabu_4001007"},
		{"kabu code without message", "marketdata: kabu station api error (http 200, code 100001)", "kabu_100001"},
		{"wrapped kabu", "market-data 7203: fetch board: marketdata: kabu station api error (http 429, code 4001006)", "kabu_4001006"},
		{"tachibana errno", "tachibana: p_errno=-2: busy", "tachibana_errno_-2"},
		{"tachibana result", "tachibana: sResultCode=11001: rejected", "tachibana_result_11001"},
		{"tachibana http", "tachibana: HTTP status 503", "tachibana_http_503"},
		{"jev http", "jev: api error (status 500): upstream", "jev_http_500"},
		{"orphan", "orphaned: still running after 10m0s", "orphaned"},
		{"panic", "handler panic: runtime error: index out of range", "handler_panic"},
		{"no session", "fetch: broker: no session established yet", "broker_no_session"},
		{"rate limited", "x: broker: request rate limited", "broker_rate_limited"},
		{"db locked", "repository: insert: database is locked", "db_locked"},
		{"timeout", `Get "http://localhost:18080/board": context deadline exceeded`, "timeout"},
		{"network", "dial tcp 127.0.0.1:18080: connect: connection refused", "network"},
		{"unclassified with package", "calibration: label outcome: weird", "unclassified:calibration"},
		{"unclassified without package", "weird", "unclassified"},
		{"empty", "", "no_error_text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := jobfailurelog.Classify(tc.in); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The code is built from the matched pattern only: a secret in the text
// around it never reaches the code.
func TestClassify_CodeCarriesNoFreeText(t *testing.T) {
	got := jobfailurelog.Classify("Authorization: Bearer SECRETTOKEN-abc kabu station api error (http 401, code 4001007)")
	if got != "kabu_4001007" {
		t.Errorf("Classify = %q, want kabu_4001007", got)
	}
	got = jobfailurelog.Classify("sAuthId=SECRET: not a pkg prefix")
	if got != "unclassified" {
		t.Errorf("Classify = %q, want unclassified (no secret-bearing code)", got)
	}
}
