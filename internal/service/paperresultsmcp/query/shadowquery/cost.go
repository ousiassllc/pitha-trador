package shadowquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Promotion is one jev_prompt_promotions row as the MCP shows it.
type Promotion struct {
	ID          int64   `json:"id"`
	At          string  `json:"at"`
	Actor       string  `json:"actor"`
	Client      *string `json:"client"`
	FromVersion string  `json:"from_version"`
	ToVersion   string  `json:"to_version"`
}

// DayCost is the shadow's cost of one JST day.
type DayCost struct {
	Date             string  `json:"date"`
	Calls            int     `json:"calls"`
	RequestCostUSD   float64 `json:"request_cost_usd"`
	EstimatedCalls   int     `json:"estimated_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// estimatedColumns is the SQL for the estimated share of a cost sum
// (rows with cost_is_estimate = 1: the response had no usage).
const estimatedColumns = `COALESCE(SUM(cost_is_estimate), 0), COALESCE(SUM(CASE WHEN cost_is_estimate = 1 THEN request_cost ELSE 0 END), 0)`

// CapSource values of jev_shadow_cost: a limit that is the build's default
// (nothing stored), one a person stored on the Settings screen, and one the
// build fixes (the failure cap: Settings cannot change it).
const (
	CapSourceDefault = "default"
	CapSourceStored  = "stored"
	CapSourceFixed   = "fixed"
)

// Spend is a number of shadow calls and their cost. EstimatedCalls and
// EstimatedCostUSD are the part of them whose token count was estimated from
// the request's size because the response carried no usage
// (cost_is_estimate = 1); they are included in Calls / RequestCostUSD.
type Spend struct {
	Calls            int     `json:"calls"`
	RequestCostUSD   float64 `json:"request_cost_usd"`
	EstimatedCalls   int     `json:"estimated_calls"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// ShadowHalt is why the shadow stopped on a day (fixed code, no free text).
type ShadowHalt struct {
	Date     string  `json:"date"`
	Reason   string  `json:"reason"`
	SpentUSD float64 `json:"spent_usd"`
	CapUSD   float64 `json:"cap_usd"`
	// Failures and FailureCap are set for reason daily_failure_cap only.
	Failures   int    `json:"failures,omitempty"`
	FailureCap int    `json:"failure_cap,omitempty"`
	At         string `json:"at"`
}

// ShadowCostStatus is jev_shadow_cost's data: the shadow's request_cost (today
// in JST, cumulative and by day), the daily cap with its stop, the switch and
// versions, and the recent promotions.
type ShadowCostStatus struct {
	Enabled          bool    `json:"enabled"`
	CandidateVersion *string `json:"candidate_version"`
	LiveVersion      string  `json:"live_version"`
	// DailyCapUSD is the daily cost cap (確定。0.10 USD の既定値を Settings
	// 画面で上げ下げできる). CapSource says where it comes from:
	// CapSourceDefault (nothing stored: the build's
	// jevshadowsettings.DefaultDailyCostCapUSD) or CapSourceStored (a value a
	// person saved on the Settings screen). CapIsDefault is CapSource ==
	// CapSourceDefault.
	DailyCapUSD  float64 `json:"daily_cap_usd"`
	CapIsDefault bool    `json:"cap_is_default"`
	CapSource    string  `json:"cap_source"`
	// FailureCap is how many failed shadow calls a JST day may have before the
	// shadow stops (a constant, jevshadowsettings.DailyFailureCap, 確定。
	// Settings cannot change it, so FailureCapSource is always CapSourceFixed);
	// FailuresToday is today's count.
	FailureCap       int       `json:"failure_cap"`
	FailureCapSource string    `json:"failure_cap_source"`
	FailuresToday    int       `json:"failures_today"`
	Today            DayCost   `json:"today"`
	Total            Spend     `json:"total"`
	Daily            []DayCost `json:"daily"`
	// HaltToday is set when the cap stopped the shadow on today's JST day.
	HaltToday          *ShadowHalt `json:"halt_today"`
	LastHalt           *ShadowHalt `json:"last_halt"`
	RecentPromotions   []Promotion `json:"recent_promotions"`
	RegisteredVersions []string    `json:"registered_versions"`
}

// shadowSettingsReader reads the jevshadowsettings keys only, with the same
// single-key statement the other fixed-key reads use.
type shadowSettingsReader struct{ q query.Q }

func (s shadowSettingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	allowed := false
	for _, k := range jevshadowsettings.Keys() {
		allowed = allowed || k == key
	}
	if !allowed {
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

// Cost reads the shadow's cost and state. now decides which JST day is
// "today"; liveDefault is the build's default live version (the stored
// promotion wins), registered the versions this build defines.
func Cost(ctx context.Context, q query.Q, now time.Time, liveDefault string, registered []string) (ShadowCostStatus, error) {
	cfg := jevshadowsettings.Load(ctx, shadowSettingsReader{q})
	status := ShadowCostStatus{
		Enabled: cfg.Enabled, LiveVersion: liveDefault, DailyCapUSD: cfg.CapUSD, CapIsDefault: cfg.CapIsDefault, CapSource: CapSourceStored,
		FailureCap: jevshadowsettings.DailyFailureCap, FailureCapSource: CapSourceFixed,
		Daily: []DayCost{}, RecentPromotions: []Promotion{}, RegisteredVersions: registered,
	}
	if cfg.CandidateVersion != "" {
		status.CandidateVersion = &cfg.CandidateVersion
	}
	if cfg.LiveVersion != "" {
		status.LiveVersion = cfg.LiveVersion
	}

	from, to := jevshadowsettings.DayBounds(now)
	today := jevshadowsettings.Day(now)
	status.Today = DayCost{Date: today}
	status.FailuresToday = cfg.Failures.FailuresOn(today)
	if cfg.CapIsDefault {
		status.CapSource = CapSourceDefault
	}
	if err := q.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(request_cost), 0), `+estimatedColumns+` FROM jev_shadow_decisions WHERE created_at >= ? AND created_at < ?`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to)).Scan(&status.Today.Calls, &status.Today.RequestCostUSD, &status.Today.EstimatedCalls, &status.Today.EstimatedCostUSD); err != nil {
		return ShadowCostStatus{}, fmt.Errorf("query: today's jev shadow cost: %w", err)
	}
	if err := q.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(request_cost), 0), `+estimatedColumns+` FROM jev_shadow_decisions`).Scan(&status.Total.Calls, &status.Total.RequestCostUSD, &status.Total.EstimatedCalls, &status.Total.EstimatedCostUSD); err != nil {
		return ShadowCostStatus{}, fmt.Errorf("query: total jev shadow cost: %w", err)
	}

	rows, err := q.QueryContext(ctx, `
SELECT substr(datetime(created_at, '+9 hours'), 1, 10) AS day, COUNT(*), COALESCE(SUM(request_cost), 0), `+estimatedColumns+`
FROM jev_shadow_decisions GROUP BY day ORDER BY day DESC LIMIT ?`, MaxShadowCostDays)
	if err != nil {
		return ShadowCostStatus{}, fmt.Errorf("query: jev shadow daily cost: %w", err)
	}
	for rows.Next() {
		var d DayCost
		if err := rows.Scan(&d.Date, &d.Calls, &d.RequestCostUSD, &d.EstimatedCalls, &d.EstimatedCostUSD); err != nil {
			_ = rows.Close()
			return ShadowCostStatus{}, fmt.Errorf("query: scan jev shadow daily cost: %w", err)
		}
		status.Daily = append(status.Daily, d)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ShadowCostStatus{}, fmt.Errorf("query: jev shadow daily cost: %w", err)
	}
	_ = rows.Close()

	if h := cfg.Halt; h != nil {
		halt := ShadowHalt{Date: h.Date, Reason: h.Reason, SpentUSD: h.SpentUSD, CapUSD: h.CapUSD, Failures: h.Failures, FailureCap: h.FailureCap, At: h.At}
		status.LastHalt = &halt
		if h.Date == today {
			status.HaltToday = &halt
		}
	}

	prows, err := q.QueryContext(ctx, `
SELECT id, at, actor, client, from_version, to_version FROM jev_prompt_promotions ORDER BY id DESC LIMIT ?`, RecentPromotions)
	if err != nil {
		return ShadowCostStatus{}, fmt.Errorf("query: jev prompt promotions: %w", err)
	}
	defer func() { _ = prows.Close() }()
	for prows.Next() {
		var (
			p      Promotion
			at     string
			client string
		)
		if err := prows.Scan(&p.ID, &at, &p.Actor, &client, &p.FromVersion, &p.ToVersion); err != nil {
			return ShadowCostStatus{}, fmt.Errorf("query: scan jev prompt promotions: %w", err)
		}
		p.At = query.FormatOut(at)
		if client != "" {
			p.Client = &client
		}
		status.RecentPromotions = append(status.RecentPromotions, p)
	}
	return status, prows.Err()
}
