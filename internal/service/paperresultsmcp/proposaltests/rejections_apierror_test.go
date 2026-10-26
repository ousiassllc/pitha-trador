package proposaltests

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Issue #912: risk_rejections splits api_error by the kind of failure the policy
// recorded after the head; a row without a recorded kind (written before the
// kind existed) and any other text stay in the plain api_error bucket, and no
// error text leaves the database.
func TestRiskRejections_SplitsApiErrorByKind(t *testing.T) {
	srv, w := proposalServer(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for i, reason := range []string{
		"api_error: timeout (jev: trader call failed: " + testkit.SentinelDetail + ")",
		"api_error: timeout (jev: trader call failed)",
		"api_error: http_5xx (jev: api error (status 500))",
		"api_error: persist_failed (disk full)",
		"api_error: " + testkit.SentinelDetail + " (a kind this version does not know)",
		"api_error: jev: trader evaluation failed", // recorded before the kind existed
		"jev_none: confidence=0.8125 regime=RANGE",
		"jev_none",
	} {
		testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version,
			risk_passed, reject_reason) VALUES (1, '7203', ?, 'NONE', 'p1', 0, ?)`,
			testkit.Stored(t, fmt.Sprintf("2026-10-02T00:%02d:00Z", i)), reason)
	}

	var d struct {
		OtherRej  int `json:"other_reject_count"`
		Breakdown []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"other_reject_breakdown"`
	}
	res := srv.call(t, "risk_rejections", map[string]any{"from": "2026-10-01", "to": "2026-10-10"})
	res.data(t, "risk_rejections", &d)
	var got []string
	sum := 0
	for _, b := range d.Breakdown {
		got = append(got, fmt.Sprintf("%s=%d", b.Reason, b.Count))
		sum += b.Count
	}
	// A "jev_none" row with detail and a bare one are one bucket, so are the
	// unrecognised kind and the row without a kind.
	want := []string{"api_error=2", "jev_none=2", "api_error:timeout=2", "api_error:http_5xx=1", "api_error:persist_failed=1"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("other_reject_breakdown = %v, want %v", got, want)
	}
	if sum != d.OtherRej || sum != 8 {
		t.Fatalf("breakdown sums to %d, other_reject_count = %d, want 8", sum, d.OtherRej)
	}
	if strings.Contains(string(res.body), testkit.SentinelDetail) {
		t.Fatal("error text of a reject reason leaked into the response")
	}
}
