package kabutests

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// recentJobs says market-data finished a moment ago, so only kabu is at fault.
type recentJobs struct{ now func() time.Time }

func (j recentJobs) LastFinishedAt(context.Context, string) (time.Time, bool, error) {
	return j.now().Add(-time.Minute), true, nil
}

// issue #863, end to end: the app's Detector sees kabuステーション refusing the
// connection (token state: unreachable since 10:00), stores kabu_unreachable
// after five minutes in health_alerts, and the Paper results MCP - a separate
// read-only process on the same DB - shows it in collection_status.alerts and
// health_today (attention_codes, market_data.kabu_login). The requests and
// responses are logged with -v as the evidence of the issue.
func TestKabuUnreachableAlertReachesTheMCP(t *testing.T) {
	now := at(20, 10, 0)
	path, w := testkit.HealthyDay(t, now)
	unreachableSince := at(20, 10, 0)
	detector := healthalert.New(healthalert.Config{
		Store: system.NewHealthAlertRepository(w),
		Jobs:  recentJobs{now: func() time.Time { return now }},
		Token: func() healthalert.TokenState { return healthalert.TokenState{UnreachableSince: unreachableSince} },
		Now:   func() time.Time { return now },
	})
	check := func(at time.Time) {
		t.Helper()
		now = at
		if err := detector.Check(t.Context()); err != nil {
			t.Fatalf("Check at %v: %v", at, err)
		}
	}
	rows := func() []string {
		t.Helper()
		r, err := w.QueryContext(t.Context(), `SELECT code, since, checked_at FROM health_alerts ORDER BY code`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		var out []string
		for r.Next() {
			var code, since, checked string
			if err := r.Scan(&code, &since, &checked); err != nil {
				t.Fatal(err)
			}
			out = append(out, code+" | "+since+" | "+checked)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	check(at(20, 10, 4))
	if got := rows(); len(got) != 0 {
		t.Fatalf("health_alerts after 4 minutes = %v, want empty", got)
	}
	check(at(20, 10, 5))
	got := rows()
	t.Logf("SELECT code, since, checked_at FROM health_alerts  (10:05 JST):\n  %v", got)
	if len(got) != 1 || got[0][:len("kabu_unreachable")] != "kabu_unreachable" {
		t.Fatalf("health_alerts = %v, want one kabu_unreachable row", got)
	}

	url := serve(t, path, func() time.Time { return now })
	period := map[string]any{"from": "2026-10-01", "to": "2026-10-20"}

	var h query.Health
	r := call(t, url, "health_today", map[string]any{})
	t.Logf("REQUEST  %s\nRESPONSE %s", r.request, r.response)
	if err := json.Unmarshal(r.data, &h); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.AttentionCodes, "kabu_unreachable") || slices.Contains(h.AttentionCodes, "kabu_not_logged_in") || !h.NeedsAttention {
		t.Errorf("attention_codes = %v, want kabu_unreachable only of the kabu codes", h.AttentionCodes)
	}
	if h.MarketData.KabuLogin != "unreachable" || !h.MarketData.AppRunning {
		t.Errorf("market_data = %+v, want kabu_login unreachable with the app running", h.MarketData)
	}

	alerts := func() []map[string]any {
		t.Helper()
		var c struct {
			Alerts []map[string]any `json:"alerts"`
		}
		r := call(t, url, "collection_status", period)
		t.Logf("REQUEST  %s\nRESPONSE %s", r.request, r.response)
		if err := json.Unmarshal(r.data, &c); err != nil {
			t.Fatal(err)
		}
		return c.Alerts
	}
	// Exactly {code, since}, since being the start of the refusal.
	if a := alerts(); len(a) != 1 || len(a[0]) != 2 || a[0]["code"] != domain.HealthAlertKabuUnreachable || a[0]["since"] != "2026-10-20T01:00:00Z" {
		t.Fatalf("alerts = %+v, want one {code: kabu_unreachable, since: 2026-10-20T01:00:00Z}", a)
	}

	// After the close, nothing is shown even though the row is still stored.
	now = at(20, 15, 30)
	if a := alerts(); len(a) != 0 {
		t.Errorf("alerts after the close = %+v, want none", a)
	}
}
