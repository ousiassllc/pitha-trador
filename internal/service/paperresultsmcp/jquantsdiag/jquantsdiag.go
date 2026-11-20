// Package jquantsdiag is the read-only analysis behind the Paper results MCP's
// jquants_status tool (issue #1093): what the J-Quants read adapter has stored,
// when it last fetched, how many requests it sent today against its cap, and how
// many failed. It writes nothing, changes no setting and returns no secret: only
// counts, dates, the on/off state, the plan flags and short fixed failure codes
// leave the database (never the API key, a URL, an error text or any price).
package jquantsdiag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-jquants.md §3.39).
const Description = "J-Quants 読み取りアダプタの記録状況の読み取り専用の集計（研究用の記録だけで、取引・Policy・Risk・発注・Jev には使っていない）: " +
	"取り込みのオン・オフ（enabled。設定画面だけで変えられる）、契約のフラグ（plan_daily / plan_minute）、1 日の要求数の上限（daily_request_cap）と今日（JST）の要求数（requests_today）、" +
	"表ごとの行数の合計（rows。symbols / daily_bars / minute_bars / coverage）、期間内の日足・分足の日数（daily_days_in_period / minute_days_in_period）、" +
	"直近 7 日の失敗数（failures_recent）と最後の失敗の固定コード（last_failure_code。http_401 / http_429 / transport など）、最後に取得に成功した時刻（last_success_at）を返す。" +
	"API キー・URL・エラー本文・価格は返さない。削除の依頼（jquants.purge_requested）も、この MCP からは変えられない"

// SchemaProperties are the tool's own arguments besides the period (none).
func SchemaProperties() map[string]any { return nil }

// Request is the tool's arguments as decoded from the call: the period only.
type Request struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// UnmarshalJSON decodes strictly.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode((*plain)(r))
}

// Data is the tool's result.
type Data struct {
	Enabled         bool `json:"enabled"`
	PlanDaily       bool `json:"plan_daily"`
	PlanMinute      bool `json:"plan_minute"`
	DailyRequestCap int  `json:"daily_request_cap"`
	RequestsToday   int  `json:"requests_today"`

	// Rows are the stored rows per table.
	Rows map[string]int `json:"rows"`
	// DaysFrom / DaysTo are the JST days the period covers (DaysTo exclusive).
	DaysFrom           string `json:"days_from"`
	DaysTo             string `json:"days_to"`
	DailyDaysInPeriod  int    `json:"daily_days_in_period"`
	MinuteDaysInPeriod int    `json:"minute_days_in_period"`

	FailuresRecent  int     `json:"failures_recent"`
	LastFailureCode string  `json:"last_failure_code"`
	LastSuccessAt   *string `json:"last_success_at"`
}

// tableKeys maps the stored tables to the names the tool returns.
var tableKeys = []struct{ table, name string }{
	{"research_jquants_symbols", "symbols"},
	{"research_jquants_daily_bars", "daily_bars"},
	{"research_jquants_minute_bars", "minute_bars"},
	{"research_jquants_coverage", "coverage"},
}

// Run reports the adapter's state as of now over the period [r.From, r.To).
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, _ Request) (any, int, error) {
	cfg, err := jquantssettings.Load(ctx, settingsReader{q})
	if err != nil {
		return nil, 0, err
	}
	out := Data{Enabled: cfg.Enabled, PlanDaily: cfg.PlanDaily, PlanMinute: cfg.PlanMinute, DailyRequestCap: cfg.DailyRequestCap, Rows: map[string]int{},
		DaysFrom: r.From.In(query.JST).Format(time.DateOnly), DaysTo: r.To.In(query.JST).Format(time.DateOnly)}
	for _, t := range tableKeys {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t.table).Scan(&n); err != nil { //nolint:gosec // G202: constant table names of this package
			return nil, 0, fmt.Errorf("query: jquants %s rows: %w", t.name, err)
		}
		out.Rows[t.name] = n
	}
	today := now.In(query.JST).Format(time.DateOnly)
	if err := fill(ctx, q, &out, today); err != nil {
		return nil, 0, err
	}
	return out, out.DailyDaysInPeriod + out.MinuteDaysInPeriod, nil
}

func fill(ctx context.Context, q query.Q, out *Data, today string) error {
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(requests), 0) FROM research_jquants_fetch_log WHERE day = ?`, today).Scan(&out.RequestsToday); err != nil {
		return fmt.Errorf("query: jquants requests today: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(DISTINCT day) FROM research_jquants_daily_bars WHERE day >= ? AND day < ?`, out.DaysFrom, out.DaysTo).Scan(&out.DailyDaysInPeriod); err != nil {
		return fmt.Errorf("query: jquants daily days: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(DISTINCT day) FROM research_jquants_minute_bars WHERE day >= ? AND day < ?`, out.DaysFrom, out.DaysTo).Scan(&out.MinuteDaysInPeriod); err != nil {
		return fmt.Errorf("query: jquants minute days: %w", err)
	}
	since, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(failures), 0) FROM research_jquants_fetch_log WHERE day > ? AND day <= ?`,
		since.AddDate(0, 0, -7).Format(time.DateOnly), today).Scan(&out.FailuresRecent); err != nil {
		return fmt.Errorf("query: jquants failures: %w", err)
	}
	var code, success sql.NullString
	err = q.QueryRowContext(ctx, `SELECT last_failure_code FROM research_jquants_fetch_log WHERE last_failure_code <> '' ORDER BY day DESC LIMIT 1`).Scan(&code)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query: jquants last error: %w", err)
	}
	out.LastFailureCode = code.String
	if err := q.QueryRowContext(ctx, `SELECT MAX(last_success_at) FROM research_jquants_fetch_log`).Scan(&success); err != nil {
		return fmt.Errorf("query: jquants last success: %w", err)
	}
	if success.Valid {
		s := query.FormatOut(success.String)
		out.LastSuccessAt = &s
	}
	return nil
}

// settingsReader reads the jquantssettings keys only, with the same single-key
// statement the other fixed-key reads use. It cannot read another runtime
// setting (nor a secret: those are in another table).
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !jquantssettings.IsKey(key) {
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
