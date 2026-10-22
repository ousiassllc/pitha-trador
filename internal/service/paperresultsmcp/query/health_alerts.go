package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
)

// Alert codes of Collection.Alerts (issue #802): fixed codes, never free text.
// health_today (#808) reads the same state through HealthAlerts.
const (
	AlertKabuNotLoggedIn = domain.HealthAlertKabuNotLoggedIn
	AlertKabuUnreachable = domain.HealthAlertKabuUnreachable
	AlertMarketDataIdle  = domain.HealthAlertMarketDataIdle
)

// Alert is one active health alert: a fixed code and when the condition began
// (UTC RFC3339).
type Alert struct {
	Code  string `json:"code"`
	Since string `json:"since"`
}

// HealthAlerts returns the alerts active at now, oldest first, never nil.
//
// The app's detector stores them (health_alerts) and stamps every check
// (health_alert_checks); this process only reads. While the last check is
// fresh the stored rows are returned, minus any outside the hours they watch
// (no alert on a non-trading day, before 08:45, after the close, nor
// market_data_idle in the lunch break). A stale or missing check means the app
// is not running, so its rows are not trusted and the only alert is
// market_data_idle, derived here from the market-data jobs: inside a trading
// session with nothing finished (and no check) for the idle threshold.
func HealthAlerts(ctx context.Context, q Q, now time.Time) ([]Alert, error) {
	checked, err := lastHealthCheck(ctx, q)
	if err != nil {
		return nil, err
	}
	if detectorRunning(checked, now) {
		return storedAlerts(ctx, q, now)
	}
	last, err := lastMarketDataFinish(ctx, q)
	if err != nil {
		return nil, err
	}
	if since, ok := healthalert.IdleSince(now, healthalert.Later(last, checked)); ok {
		return []Alert{{Code: AlertMarketDataIdle, Since: since.UTC().Format(time.RFC3339)}}, nil
	}
	return []Alert{}, nil
}

// detectorRunning reports whether the app's detector checked recently enough
// for its stored rows to be trusted (the app is running).
func detectorRunning(checked, now time.Time) bool {
	return !checked.IsZero() && now.Sub(checked) <= healthalert.HeartbeatStale
}

// lastHealthCheck is the detector's last check time, zero when it never ran.
func lastHealthCheck(ctx context.Context, q Q) (time.Time, error) {
	var stored string
	err := q.QueryRowContext(ctx, `SELECT checked_at FROM health_alert_checks WHERE id = 1`).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("query: health alert check: %w", err)
	}
	return sqlutil.ParseTime(stored)
}

// lastMarketDataFinish is the newest finish of a succeeded or failed
// market-data job, zero when none is retained.
func lastMarketDataFinish(ctx context.Context, q Q) (time.Time, error) {
	var stored sql.NullString
	err := q.QueryRowContext(ctx, `
SELECT MAX(finished) FROM (
	SELECT MAX(finished_at) AS finished FROM jobs WHERE queue = ? AND status = 'succeeded'
	UNION ALL
	SELECT MAX(finished_at) FROM jobs WHERE queue = ? AND status = 'failed'
)`, healthalert.MarketDataQueue, healthalert.MarketDataQueue).Scan(&stored)
	if err != nil {
		return time.Time{}, fmt.Errorf("query: last market-data job: %w", err)
	}
	if !stored.Valid {
		return time.Time{}, nil
	}
	return sqlutil.ParseTime(stored.String)
}

// storedAlerts reads the detector's rows and keeps those inside their window.
func storedAlerts(ctx context.Context, q Q, now time.Time) ([]Alert, error) {
	rows, err := q.QueryContext(ctx, `SELECT code, since FROM health_alerts ORDER BY since, code`)
	if err != nil {
		return nil, fmt.Errorf("query: health alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Alert{}
	for rows.Next() {
		var code, since string
		if err := rows.Scan(&code, &since); err != nil {
			return nil, fmt.Errorf("query: scan health alert: %w", err)
		}
		if healthalert.InWindow(code, now) {
			out = append(out, Alert{Code: code, Since: FormatOut(since)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: health alerts: %w", err)
	}
	return out, nil
}
