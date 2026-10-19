package mcptests

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

var jst = time.FixedZone("JST", 9*3600)

// at is a JST instant on Tuesday 2026-10-20, a trading day (day 24 is a
// Saturday, 12 the Sports Day holiday).
func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, jst) }

func stored(t *testing.T, ts time.Time) string {
	return testkit.Stored(t, ts.UTC().Format(time.RFC3339))
}

// detectorState is the app's side: the rows the detector wrote.
type detectorState struct {
	checkedAt time.Time // zero: the detector never ran
	alerts    map[string]time.Time
}

func (s detectorState) write(t *testing.T, w *sql.DB) {
	t.Helper()
	if !s.checkedAt.IsZero() {
		testkit.MustExec(t, w, `INSERT INTO health_alert_checks (id, checked_at) VALUES (1, ?)`, stored(t, s.checkedAt))
	}
	for code, since := range s.alerts {
		testkit.MustExec(t, w, `INSERT INTO health_alerts (code, since, checked_at) VALUES (?, ?, ?)`,
			code, stored(t, since), stored(t, s.checkedAt))
	}
}

func marketDataJob(t *testing.T, w *sql.DB, status string, finished time.Time) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at) VALUES ('market-data', '{}', ?, ?, ?)`,
		status, stored(t, finished), stored(t, finished))
}

type alert = struct{ code, since string }

func codes(got []query.Alert) []alert {
	out := []alert{}
	for _, a := range got {
		out = append(out, alert{a.Code, a.Since})
	}
	return out
}

func TestHealthAlerts_StoredAlertsAreShownOnlyInsideTheirWindowWhileTheAppChecks(t *testing.T) {
	kabu, idle := domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertMarketDataIdle
	both := func(now time.Time) detectorState {
		return detectorState{checkedAt: now.Add(-30 * time.Second),
			alerts: map[string]time.Time{kabu: at(20, 8, 45), idle: now.Add(-10 * time.Minute)}}
	}
	tests := []struct {
		name string
		now  time.Time
		want []string // alert codes
	}{
		{"before the 08:45 check nothing is shown", at(20, 8, 44), nil},
		{"kabu at 08:45, the idle alert is not a session alert yet", at(20, 8, 45), []string{kabu}},
		{"inside the morning session both show", at(20, 10, 0), []string{idle, kabu}},
		{"in the lunch break only kabu shows", at(20, 12, 0), []string{kabu}},
		{"afternoon session", at(20, 13, 0), []string{idle, kabu}},
		{"nothing at the close", at(20, 15, 30), nil},
		{"nothing on a Saturday", at(24, 10, 0), nil},
		{"nothing on a holiday", at(12, 10, 0), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.NewMigratedDB(t)
			both(tt.now).write(t, w)
			got, err := query.HealthAlerts(context.Background(), w, tt.now)
			if err != nil {
				t.Fatalf("HealthAlerts: %v", err)
			}
			var gotCodes []string
			for _, a := range got {
				gotCodes = append(gotCodes, a.Code)
			}
			slices.Sort(gotCodes)
			slices.Sort(tt.want)
			if !slices.Equal(gotCodes, tt.want) {
				t.Errorf("alerts at %s = %v, want %v", tt.now.Format("01-02 15:04"), gotCodes, tt.want)
			}
			if got == nil {
				t.Error("alerts is nil, want an empty list (JSON [])")
			}
		})
	}
}

func TestHealthAlerts_ReportsSinceInUTCAndClearsWhenTheAppStopsReportingIt(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	now := at(20, 9, 30)
	detectorState{checkedAt: now.Add(-time.Minute), alerts: map[string]time.Time{domain.HealthAlertKabuNotLoggedIn: at(20, 8, 45)}}.write(t, w)
	got, err := query.HealthAlerts(context.Background(), w, now)
	if want := []alert{{domain.HealthAlertKabuNotLoggedIn, "2026-10-19T23:45:00Z"}}; err != nil || !slices.Equal(codes(got), want) {
		t.Fatalf("alerts = %v, %v; want %v", codes(got), err, want)
	}

	// The detector's next check found kabu logged in: the row is gone, the
	// alert with it.
	testkit.MustExec(t, w, `DELETE FROM health_alerts`)
	got, err = query.HealthAlerts(context.Background(), w, now)
	if err != nil || len(got) != 0 {
		t.Fatalf("alerts after the condition cleared = %v, %v; want none", codes(got), err)
	}
}

func TestHealthAlerts_AppNotRunningInSessionDerivesIdleFromTheJobs(t *testing.T) {
	idle := domain.HealthAlertMarketDataIdle
	stale := func(now time.Time) detectorState {
		// A stored kabu alert from before the app stopped must not be trusted.
		return detectorState{checkedAt: now.Add(-10 * time.Minute),
			alerts: map[string]time.Time{domain.HealthAlertKabuNotLoggedIn: at(20, 8, 45)}}
	}
	tests := []struct {
		name      string
		now       time.Time
		state     func(now time.Time) detectorState
		lastJob   time.Time // zero: no job
		wantSince string    // "" = no alert
	}{
		{"idle since the last check", at(20, 10, 0), stale, at(20, 9, 0), "2026-10-20T00:50:00Z"},
		{"a later job moves the idle start", at(20, 10, 0), stale, at(20, 9, 56), ""},
		{"never checked, no job: idle since the session opened", at(20, 9, 5), func(time.Time) detectorState { return detectorState{} }, time.Time{}, "2026-10-20T00:00:00Z"},
		{"never checked, no job, session just opened", at(20, 9, 4), func(time.Time) detectorState { return detectorState{} }, time.Time{}, ""},
		{"yesterday's job does not count against today's open", at(20, 9, 3), func(time.Time) detectorState { return detectorState{} }, at(19, 15, 0), ""},
		{"afternoon: idle counted from the 12:30 reopening", at(20, 12, 34), func(time.Time) detectorState { return detectorState{} }, at(20, 11, 0), ""},
		{"afternoon: idle after five minutes", at(20, 12, 35), func(time.Time) detectorState { return detectorState{} }, at(20, 11, 0), "2026-10-20T03:30:00Z"},
		{"lunch break is no alarm", at(20, 12, 0), func(time.Time) detectorState { return detectorState{} }, time.Time{}, ""},
		{"before the open is no alarm", at(20, 8, 50), stale, time.Time{}, ""},
		{"non-trading day is no alarm", at(24, 10, 0), stale, time.Time{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.NewMigratedDB(t)
			tt.state(tt.now).write(t, w)
			if !tt.lastJob.IsZero() {
				marketDataJob(t, w, "succeeded", tt.lastJob)
			}
			got, err := query.HealthAlerts(context.Background(), w, tt.now)
			if err != nil {
				t.Fatalf("HealthAlerts: %v", err)
			}
			want := []alert{}
			if tt.wantSince != "" {
				want = append(want, alert{idle, tt.wantSince})
			}
			if !slices.Equal(codes(got), want) {
				t.Errorf("alerts = %v, want %v", codes(got), want)
			}
		})
	}
}

func TestHealthAlerts_AFailedJobIsStillMarketDataActivity(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	marketDataJob(t, w, "failed", at(20, 9, 58))
	got, err := query.HealthAlerts(context.Background(), w, at(20, 10, 0))
	if err != nil || len(got) != 0 {
		t.Fatalf("alerts = %v, %v; want none (a failed job is not silence)", codes(got), err)
	}
}

func TestCollectionStatus_ExposesAlertsAsFixedCodes(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	now := at(20, 10, 0)
	detectorState{checkedAt: now.Add(-30 * time.Second),
		alerts: map[string]time.Time{domain.HealthAlertKabuNotLoggedIn: at(20, 8, 45)}}.write(t, w)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, Now: func() time.Time { return now }})

	var d struct {
		Alerts []map[string]any `json:"alerts"`
	}
	srv.call(t, "collection_status", period).data(t, "collection_status", &d)
	// Exactly {code, since}: no free text rides along.
	if len(d.Alerts) != 1 || len(d.Alerts[0]) != 2 || d.Alerts[0]["code"] != "kabu_not_logged_in" || d.Alerts[0]["since"] != "2026-10-19T23:45:00Z" {
		t.Fatalf("alerts = %+v, want one {code: kabu_not_logged_in, since: 2026-10-19T23:45:00Z}", d.Alerts)
	}
}
