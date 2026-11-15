// Package disclosuresrcdiag is the read-only analysis behind the Paper results
// MCP's disclosure_sources_status tool (issue #1096): per extra disclosure /
// news source (EDINET document list, 立花 news), how many rows the recorder
// stored, when it last fetched, and how many requests failed. It writes nothing,
// changes no setting and returns no secret: only counts, times, the on/off
// state and short fixed failure codes leave the database (never a key, a
// credential, a URL, an error text or a headline).
package disclosuresrcdiag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-disclosure-sources.md §3.35).
const Description = "開示・ニュースの追加の情報源（EDINET の書類一覧、立花 e支店のニュース）の記録状況の読み取り専用の集計（記録だけで、取引・Policy・Risk・発注・既存の開示見送りルールには使っていない）: " +
	"情報源ごとに、記録のオン・オフ（enabled。設定画面だけで変えられる）、記録した行数の合計（recorded_total）と期間内の行数（recorded_in_period。期間は記録した書類・ニュースの時刻）、" +
	"最古・最新の記録の時刻、最後に要求を出した時刻・成功した時刻・失敗した時刻（last_fetched_at / last_success_at / last_failure_at）、要求数の累計（request_count）と失敗数の累計（failure_count）、" +
	"最後の失敗の固定コード（last_failure_code。http_401 / http_429 / timeout など）、今日（JST）の要求数と 1 日の上限（requests_today / daily_request_cap）を返す。" +
	"API キー・認証情報・URL・エラー本文・見出し・書類のタイトルは返さない。立花のニュースは実機計測（#725）が終わるまで本番に繋がらないため、行は 0 のままになる"

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

// Source is one source's state.
type Source struct {
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`

	RecordedTotal    int     `json:"recorded_total"`
	RecordedInPeriod int     `json:"recorded_in_period"`
	OldestAt         *string `json:"oldest_recorded_at"`
	NewestAt         *string `json:"newest_recorded_at"`

	RequestCount    int     `json:"request_count"`
	FailureCount    int     `json:"failure_count"`
	LastFetchedAt   *string `json:"last_fetched_at"`
	LastSuccessAt   *string `json:"last_success_at"`
	LastFailureAt   *string `json:"last_failure_at"`
	LastFailureCode string  `json:"last_failure_code"`

	RequestsToday   int `json:"requests_today"`
	DailyRequestCap int `json:"daily_request_cap"`
}

// Data is the tool's result.
type Data struct {
	RowsFrom string   `json:"rows_from"`
	RowsTo   string   `json:"rows_to"`
	Sources  []Source `json:"sources"`
}

// Run reports every source as of now over [r.From, r.To).
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, _ Request) (any, int, error) {
	cfg, err := disclosuresrcsettings.Load(ctx, settingsReader{q})
	if err != nil {
		return nil, 0, err
	}
	from, to := r.Bounds()
	out := Data{RowsFrom: query.FormatOut(from), RowsTo: query.FormatOut(to), Sources: []Source{}}
	today := now.In(query.JST).Format(time.DateOnly)
	for _, src := range disclosuresrc.Sources() {
		s := Source{Source: src}
		table, col := "research_edinet_documents", "submitted_at"
		s.Enabled, s.DailyRequestCap = cfg.EdinetEnabled, cfg.EdinetDailyRequestCap
		if src == disclosuresrc.SourceTachibanaNews {
			table, col = "research_tachibana_news", "published_at"
			s.Enabled, s.DailyRequestCap = cfg.TachibanaNewsEnabled, cfg.TachibanaNewsDailyCap
		}
		if err := fillRecorded(ctx, q, &s, table, col, from, to); err != nil {
			return nil, 0, err
		}
		if err := fillStatus(ctx, q, &s, today); err != nil {
			return nil, 0, err
		}
		out.Sources = append(out.Sources, s)
	}
	total := 0
	for _, s := range out.Sources {
		total += s.RecordedInPeriod
	}
	return out, total, nil
}

// fillRecorded counts the rows of a recorded table. table and col are this
// package's own constants, never an argument.
func fillRecorded(ctx context.Context, q query.Q, s *Source, table, col, from, to string) error {
	var oldest, newest sql.NullString
	err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(`+col+` >= ? AND `+col+` < ?), 0), MIN(`+col+`), MAX(`+col+`) FROM `+table, from, to).
		Scan(&s.RecordedTotal, &s.RecordedInPeriod, &oldest, &newest)
	if err != nil {
		return fmt.Errorf("query: disclosure source %s counts: %w", s.Source, err)
	}
	s.OldestAt, s.NewestAt = outPtr(oldest), outPtr(newest)
	return nil
}

func fillStatus(ctx context.Context, q query.Q, s *Source, today string) error {
	var day string
	var fetched, success, failure sql.NullString
	err := q.QueryRowContext(ctx, `
SELECT requests_day, requests_today, request_count, failure_count, last_fetched_at, last_success_at, last_failure_at, last_failure_code
FROM research_disclosure_source_status WHERE source = ?`, s.Source).
		Scan(&day, &s.RequestsToday, &s.RequestCount, &s.FailureCount, &fetched, &success, &failure, &s.LastFailureCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query: disclosure source %s status: %w", s.Source, err)
	}
	if day != today {
		s.RequestsToday = 0 // the stored count is of an earlier JST day
	}
	s.LastFetchedAt, s.LastSuccessAt, s.LastFailureAt = outPtr(fetched), outPtr(success), outPtr(failure)
	return nil
}

func outPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := query.FormatOut(v.String)
	return &s
}

// settingsReader reads the disclosuresrcsettings keys only, with the same
// single-key statement the other fixed-key reads use. It cannot read another
// runtime setting.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !disclosuresrcsettings.IsKey(key) {
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
