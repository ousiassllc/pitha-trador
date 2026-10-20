package testkit

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Fixtures of health_today (issue #808). Days are October 2026 in JST: the
// 20th is a Tuesday and a trading day, the 24th a Saturday, the 12th the
// Sports Day holiday.

// At is a JST instant on the given day of October 2026.
func At(day, h, m int) time.Time {
	return time.Date(2026, 10, day, h, m, 0, 0, marketcalendar.JST)
}

// StoredAt is ts as the application stores a time column.
func StoredAt(t *testing.T, ts time.Time) string {
	t.Helper()
	return Stored(t, ts.UTC().Format(time.RFC3339))
}

// Setting writes a runtime_settings row.
func Setting(t *testing.T, w *sql.DB, key, value string) {
	t.Helper()
	MustExec(t, w, `INSERT OR REPLACE INTO runtime_settings (key, value, updated_at) VALUES (?, ?, ?)`,
		key, value, Stored(t, "2026-10-01T00:00:00Z"))
}

// HealthyDay returns a migrated database in which nothing needs attention at
// now: a model adopted the day before, a Paper position opened at 09:30 on the
// 20th (a trading day only), a market snapshot on every trading day of the
// seven days before now, and the app's detector reporting with no alert.
func HealthyDay(t *testing.T, now time.Time) (string, *sql.DB) {
	t.Helper()
	path, w := NewMigratedDB(t)
	MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	MustExec(t, w, `INSERT INTO metalabel_models (id, version, algorithm, label_version, status, reject_reason,
		trained_at, train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
		feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at)
		VALUES (1, 'v1', 'logreg', 2, 'active', '', ?, ?, ?, ?, ?, 400, 100, '[]', '{}', 0.6, 3.5, '[]', ?)`,
		StoredAt(t, At(19, 9, 0)), StoredAt(t, At(1, 0, 0)), StoredAt(t, At(10, 0, 0)), StoredAt(t, At(10, 0, 0)),
		StoredAt(t, At(15, 0, 0)), StoredAt(t, At(19, 9, 0)))
	if marketcalendar.TSE.IsTradingDay(now) {
		OpenPosition(t, w, At(20, 9, 30))
	}
	today := now.In(marketcalendar.JST)
	for back := 1; back <= 7; back++ {
		day := time.Date(today.Year(), today.Month(), today.Day()-back, 10, 0, 0, 0, marketcalendar.JST)
		if marketcalendar.TSE.IsTradingDay(day) {
			MustExec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume,
				turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, 1, 1, 1, 1, 1, 1, 1, '{}')`,
				StoredAt(t, day))
		}
	}
	DetectorChecked(t, w, now.Add(-30*time.Second))
	return path, w
}

// OpenPosition inserts an open Paper position opened at opened.
func OpenPosition(t *testing.T, w *sql.DB, opened time.Time) {
	t.Helper()
	MustExec(t, w, `INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity,
		entry_price, current_price, opened_at) VALUES (1, 99, '7203', 'LONG', 100, 1000, 1000, ?)`, StoredAt(t, opened))
}

// DetectorChecked sets the app's last health check (issue #802).
func DetectorChecked(t *testing.T, w *sql.DB, checked time.Time) {
	t.Helper()
	MustExec(t, w, `INSERT OR REPLACE INTO health_alert_checks (id, checked_at) VALUES (1, ?)`, StoredAt(t, checked))
}

// HealthAlert stores an active health alert the way the detector does.
func HealthAlert(t *testing.T, w *sql.DB, code string, since, checked time.Time) {
	t.Helper()
	MustExec(t, w, `INSERT INTO health_alerts (code, since, checked_at) VALUES (?, ?, ?)`,
		code, StoredAt(t, since), StoredAt(t, checked))
}

// MarketDataJob stores a finished market-data job.
func MarketDataJob(t *testing.T, w *sql.DB, status string, finished time.Time) {
	t.Helper()
	MustExec(t, w, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at) VALUES ('market-data', '{}', ?, ?, ?)`,
		status, StoredAt(t, finished), StoredAt(t, finished))
}

// DailyGuardHalt latches the daily entry halt of day (YYYY-MM-DD) at 13:00 JST
// of the 20th, with message as the operator-facing text.
func DailyGuardHalt(t *testing.T, w *sql.DB, day, reason, message string) {
	t.Helper()
	Setting(t, w, "system.daily_guard", `{"day":"`+day+`","peak_pnl_jpy":0,"halt_reason":"`+reason+
		`","halt_message":"`+message+`","halted_at":"2026-10-20T04:00:00Z"}`)
}

// KillEvent stores a Kill Switch event triggered at 09:00 JST of the 20th,
// resolved an hour later when resolved.
func KillEvent(t *testing.T, w *sql.DB, id int, reason string, resolved bool) {
	t.Helper()
	MustExec(t, w, `INSERT INTO kill_switch_events (id, triggered_at, reason, detail_json) VALUES (?, ?, ?, '{}')`,
		id, StoredAt(t, At(20, 9, 0)), reason)
	if resolved {
		MustExec(t, w, `INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by) VALUES (?, ?, 'manual')`,
			id, StoredAt(t, At(20, 10, 0)))
	}
}

// RetrainSkip stores a skipped nightly retraining (87 usable of 130 rows).
func RetrainSkip(t *testing.T, w *sql.DB, skipped time.Time, code string) {
	t.Helper()
	MustExec(t, w, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at)
		VALUES (?, ?, 87, 130, ?)`, StoredAt(t, skipped), code, StoredAt(t, skipped))
}
