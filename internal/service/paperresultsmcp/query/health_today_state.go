package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/dailyhalt"
)

// runtime_settings keys of the risk engine (internal/service/risk) health_today
// reads besides the daily guard thresholds and the bootstrap switch.
const keySystemKilled = "system.killed"

// maxKillReasons caps the unresolved Kill Switch events listed.
const maxKillReasons = 20

// ReadableSettingKeys are the only runtime_settings keys this package reads
// (health_today). runtime_settings can hold secret paths, so a key outside
// this list is refused before any SQL runs.
func ReadableSettingKeys() []string {
	return append(dailyguardsettings.Keys(), keySystemKilled, dailyhalt.Key, paperbootstrapsettings.Key)
}

// settingsReader is a read-only runtime_settings view limited to
// ReadableSettingKeys. It implements the Store interfaces of dailyguardsettings
// and paperbootstrapsettings, so their parsing rules are the app's own.
type settingsReader struct{ q Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !slices.Contains(ReadableSettingKeys(), key) {
		return "", false, fmt.Errorf("query: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query: runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

func healthPaperTrades(ctx context.Context, q Q, today Range) (HealthPaperTrades, error) {
	from, to := today.bounds()
	var out HealthPaperTrades
	err := q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(opened_at >= ? AND opened_at < ?), 0), COALESCE(SUM(closed_at >= ? AND closed_at < ?), 0)
FROM positions
WHERE (opened_at >= ? AND opened_at < ?) OR (closed_at >= ? AND closed_at < ?)`,
		from, to, from, to, from, to, from, to).Scan(&out.OpenedCount, &out.ClosedCount)
	if err != nil {
		return HealthPaperTrades{}, fmt.Errorf("query: today's paper trades: %w", err)
	}
	return out, nil
}

func healthMetaModel(ctx context.Context, q Q) (HealthMetaModel, error) {
	var version, trainedAt string
	err := q.QueryRowContext(ctx, `
SELECT version, trained_at FROM metalabel_models
WHERE status = 'active' ORDER BY trained_at DESC, id DESC LIMIT 1`).Scan(&version, &trainedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return HealthMetaModel{}, nil
	}
	if err != nil {
		return HealthMetaModel{}, fmt.Errorf("query: active meta model: %w", err)
	}
	at := FormatOut(trainedAt)
	return HealthMetaModel{Adopted: true, Version: &version, TrainedAt: &at}, nil
}

func healthBootstrap(ctx context.Context, q Q, today Range, modelAdopted bool) (HealthBootstrap, error) {
	enabled, err := paperbootstrapsettings.Source{Store: settingsReader{q}}.PaperBootstrapEnabled(ctx)
	if err != nil {
		return HealthBootstrap{}, err
	}
	use, err := PaperBootstrapUse(ctx, q, today)
	if err != nil {
		return HealthBootstrap{}, err
	}
	return HealthBootstrap{Enabled: enabled, Effective: enabled && !modelAdopted, SignalsToday: use.Signals}, nil
}

// healthMarketData reuses HealthAlerts, so the alerts (and their window rules)
// are those of collection_status and the in-app banner; the two statuses only
// restate them.
func healthMarketData(ctx context.Context, q Q, now time.Time) (HealthMarketData, error) {
	alerts, err := HealthAlerts(ctx, q, now)
	if err != nil {
		return HealthMarketData{}, err
	}
	checked, err := lastHealthCheck(ctx, q)
	if err != nil {
		return HealthMarketData{}, err
	}
	m := HealthMarketData{
		KabuLogin:  StatusNotWatched,
		MarketData: StatusNotWatched,
		AppRunning: detectorRunning(checked, now),
		Alerts:     alerts,
	}
	has := func(code string) bool {
		return slices.ContainsFunc(alerts, func(a Alert) bool { return a.Code == code })
	}
	if _, inWindow := healthalert.KabuWindow(now); inWindow {
		switch {
		case !m.AppRunning:
			m.KabuLogin = KabuLoginUnknown
		case has(AlertKabuNotLoggedIn):
			m.KabuLogin = KabuLoginNotLoggedIn
		case has(AlertKabuUnreachable):
			m.KabuLogin = KabuLoginUnreachable
		default:
			m.KabuLogin = KabuLoginOK
		}
	}
	if marketcalendar.TSE.IsOpen(now) {
		m.MarketData = MarketDataRunning
		if has(AlertMarketDataIdle) {
			m.MarketData = MarketDataIdleStatus
		}
	}
	return m, nil
}

// healthKillSwitch reads the state the way risk.Engine.State does: killed
// while an event is unresolved or the system.killed flag is set.
func healthKillSwitch(ctx context.Context, q Q) (HealthKillSwitch, error) {
	k := HealthKillSwitch{Reasons: []string{}}
	rows, err := q.QueryContext(ctx, `
SELECT e.triggered_at, e.reason
FROM kill_switch_events e
LEFT JOIN kill_switch_resolutions s ON s.kill_switch_event_id = e.id
WHERE s.id IS NULL
ORDER BY e.triggered_at DESC, e.id DESC
LIMIT ?`, maxKillReasons)
	if err != nil {
		return HealthKillSwitch{}, fmt.Errorf("query: unresolved kill switch events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var triggered, reason string
		if err := rows.Scan(&triggered, &reason); err != nil {
			return HealthKillSwitch{}, fmt.Errorf("query: scan unresolved kill switch event: %w", err)
		}
		if k.Since == nil {
			s := FormatOut(triggered)
			k.Since = &s
		}
		k.UnresolvedCount++
		if !slices.Contains(k.Reasons, reason) {
			k.Reasons = append(k.Reasons, reason)
		}
	}
	if err := rows.Err(); err != nil {
		return HealthKillSwitch{}, fmt.Errorf("query: unresolved kill switch events: %w", err)
	}
	_ = rows.Close()

	raw, ok, err := settingsReader{q}.Get(ctx, keySystemKilled)
	if err != nil {
		return HealthKillSwitch{}, err
	}
	if ok {
		if err := json.Unmarshal([]byte(raw), &k.FlagSet); err != nil {
			return HealthKillSwitch{}, fmt.Errorf("query: decode %s: %w", keySystemKilled, err)
		}
	}
	k.Active = k.UnresolvedCount > 0 || k.FlagSet
	return k, nil
}

// healthRetrain flags the newest retrain skip (issue #799) when it is recent
// and no model - adopted or rejected - was trained after it.
func healthRetrain(ctx context.Context, q Q, now time.Time) (HealthRetrain, error) {
	var skippedAt, code string
	err := q.QueryRowContext(ctx, `
SELECT skipped_at, reason_code FROM metalabel_retrain_skips ORDER BY skipped_at DESC, id DESC LIMIT 1`).
		Scan(&skippedAt, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return HealthRetrain{}, nil
	}
	if err != nil {
		return HealthRetrain{}, fmt.Errorf("query: latest retrain skip: %w", err)
	}
	skipped, err := sqlutil.ParseTime(skippedAt)
	if err != nil {
		return HealthRetrain{}, err
	}
	var trained sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT MAX(trained_at) FROM metalabel_models`).Scan(&trained); err != nil {
		return HealthRetrain{}, fmt.Errorf("query: latest model: %w", err)
	}
	trainedAfter := false
	if trained.Valid {
		t, err := sqlutil.ParseTime(trained.String)
		if err != nil {
			return HealthRetrain{}, err
		}
		trainedAfter = !t.Before(skipped)
	}
	at, reason := FormatOut(skippedAt), NormalizeRetrainSkipCode(code)
	return HealthRetrain{
		Skipped:         !trainedAfter && now.Sub(skipped) <= retrainSkipFreshHours*time.Hour,
		LatestSkippedAt: &at,
		ReasonCode:      &reason,
	}, nil
}

// MarketDataState is the kabu login / market-data / app state at now, for
// tools outside this package that report it next to their own data.
func MarketDataState(ctx context.Context, q Q, now time.Time) (HealthMarketData, error) {
	return healthMarketData(ctx, q, now)
}
