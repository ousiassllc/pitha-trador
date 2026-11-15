// Package researchquotediag is the read-only comparison of kabu's market_snapshots
// with the 立花 research 時価 samples (issue #1083) behind the
// research_quote_source_compare MCP tool. Every statement is a SELECT of named
// columns; nothing here is a trading input, and no secret or URL is read.
package researchquotediag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/budget"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// MaxScanRows is how many rows (both sources together) one call reads: the
// comparison pairs rows in memory. A period that needs more is answered for
// the days that fit, and says so in scan.
const MaxScanRows = 400_000

// rowCap is MaxScanRows; tests lower it.
var rowCap = MaxScanRows

// Notice is returned with every answer.
const Notice = "研究専用の比較です。取引・しきい値・採用の判断には使いません。立花の時価は研究用の記録（research_quote_samples）で、kabu の market_snapshots は読み取るだけです"

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-research-quotes.md).
const Description = "kabu の market_snapshots と、立花の研究用の時価の記録（research_quote_samples。1 分に 1 回・銘柄ごと）を並べる、読み取り専用の比較（取引の入力ではない）: " +
	"期間（JST）のうち立花の記録がある日について、日ごと・合計で、ソース別の行数・銘柄数・分数（立花は via の event / rest の内訳つき）、" +
	"同じ銘柄・同じ UTC の分で kabu の最後のスナップショットの price と立花の price を突き合わせた件数（minutes_both。立花だけ only_tachibana、kabu だけ only_kabu）と、" +
	"対になった分の価格差（kabu 比の bps）の絶対値の平均・中央値・95 パーセンタイル・最大と、5・10・50bps を超えた割合、符号つきの平均。" +
	"by_phase に同じ比較を立会の局面（open_auction=9:00〜9:05 / morning / lunch=11:25〜12:35 / afternoon / close=15:20 以降）別に返す（市場の文脈）。" +
	"status に記録の状態（research_tachibana_status）。symbol で 1 銘柄に絞れる。読み取る行数の上限（scan.row_cap）か時間で止まったときは scan.complete=false と" +
	"どの日まで読んだか（covered_through）を返す。認証情報は返さない。しきい値の判定・採用の推奨は返さない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"symbol": map[string]any{"type": "string", "pattern": symbolPattern.String(), "description": "1 銘柄に絞る銘柄コード（省略: 全銘柄）"},
	}
}

var symbolPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,8}$`)

// Request is the tool's arguments as decoded from the call. Decoding rejects an
// unknown field and a malformed symbol.
type Request struct {
	From   *string `json:"from"`
	To     *string `json:"to"`
	Symbol string  `json:"symbol"`
}

// UnmarshalJSON decodes strictly and validates.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	if r.Symbol != "" && !symbolPattern.MatchString(r.Symbol) {
		return fmt.Errorf("researchquotediag: symbol %q is not a 銘柄コード", r.Symbol)
	}
	return nil
}

// Run compares the sources over [r.From, r.To) and returns the data and the
// number of rows read.
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	d, rows, err := Analyze(ctx, q, r, req)
	return d, rows, err
}

// Analyze is Run with typed data.
func Analyze(ctx context.Context, q query.Q, r query.Range, req Request) (Data, int, error) {
	data := Data{
		Days: []Day{}, ByPhase: []PhaseComparison{}, Definitions: definitions(), Notice: Notice,
		Scan: Scan{RowCap: rowCap, Complete: true},
	}
	if req.Symbol != "" {
		s := req.Symbol
		data.Symbol = &s
	}
	var err error
	if data.Status, err = readStatus(ctx, q); err != nil {
		return Data{}, 0, err
	}
	soft, cancel := budget.Soft(ctx)
	defer cancel()

	total := &cmpAcc{}
	byPhase := map[string]*cmpAcc{}
	tSymbols, kSymbols := map[string]struct{}{}, map[string]struct{}{}
	for day := r.From; day.Before(r.To); day = day.AddDate(0, 0, 1) {
		if soft.Err() != nil {
			data.Scan.Complete, data.Scan.Note = false, "時間の上限に達したため、これ以降の日は読んでいません"
			break
		}
		from, to := sqlutil.FormatTime(day), sqlutil.FormatTime(day.AddDate(0, 0, 1))
		left := rowCap - data.Scan.RowsRead
		t, err := readTachibana(soft, q, from, to, req.Symbol, left)
		if err == nil && t.stats.Rows == 0 { // no 立花 sample: the day is not part of the comparison
			markCovered(&data, day)
			continue
		}
		var k *kabuDay
		if err == nil {
			k, err = readKabu(soft, q, from, to, req.Symbol, left-t.stats.Rows)
		}
		switch {
		case errors.Is(err, errCapped):
			data.Scan.Complete, data.Scan.Note = false, fmt.Sprintf("読む行数の上限（%d 行）に達したため、これ以降の日は読んでいません。期間を短くしてください", rowCap)
		case err != nil && budget.Spent(ctx, soft, err):
			data.Scan.Complete, data.Scan.Note = false, "時間の上限に達したため、これ以降の日は読んでいません"
		case err != nil:
			return Data{}, 0, err
		}
		if !data.Scan.Complete {
			break
		}
		data.Scan.RowsRead += t.stats.Rows + k.stats.Rows
		dayCmp := compare(t, k, byPhase)
		total.merge(dayCmp)
		data.Days = append(data.Days, Day{Date: day.In(query.JST).Format(time.DateOnly), Tachibana: t.stats, Kabu: k.stats, Comparison: dayCmp.comparison()})
		addTotals(&data.Tachibana, tSymbols, t)
		addTotals(&data.Kabu, kSymbols, k)
		markCovered(&data, day)
	}
	data.Tachibana.Symbols, data.Kabu.Symbols = len(tSymbols), len(kSymbols)
	data.Comparison = total.comparison()
	for _, p := range researchquote.Phases {
		if a := byPhase[p]; a != nil {
			data.ByPhase = append(data.ByPhase, PhaseComparison{Phase: p, Comparison: a.comparison()})
		}
	}
	return data, data.Scan.RowsRead, nil
}

type withDay interface{ day() *dayData }

func (d *tachibanaDay) day() *dayData { return &d.dayData }
func (d *kabuDay) day() *dayData      { return &d.dayData }

// addTotals adds one day's stats of a source to its totals. Minutes carry the
// date, so the total number of distinct minutes is the sum over the days.
func addTotals(total *SourceStats, symbols map[string]struct{}, d withDay) {
	s := d.day()
	total.Rows += s.stats.Rows
	total.Minutes += s.stats.Minutes
	total.ViaEvent += s.stats.ViaEvent
	total.ViaREST += s.stats.ViaREST
	for sym := range s.symbols {
		symbols[sym] = struct{}{}
	}
}

func markCovered(data *Data, day time.Time) {
	through := day.In(query.JST).Format(time.DateOnly)
	data.Scan.CoveredThrough = &through
}

func readStatus(ctx context.Context, q query.Q) (Status, error) {
	var state, detail, updated string
	err := q.QueryRowContext(ctx, `SELECT state, detail, updated_at FROM research_tachibana_status WHERE component = 'quotes'`).
		Scan(&state, &detail, &updated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Status{State: "unknown"}, nil
	case err != nil:
		return Status{}, fmt.Errorf("researchquotediag: status: %w", err)
	}
	out := query.FormatOut(updated)
	return Status{State: state, Detail: detail, UpdatedAt: &out}, nil
}

func definitions() []string {
	return []string{
		"日は JST の暦日。立花の記録がある日だけが対象で、その日の kabu の market_snapshots（同じ日の全行。symbol で絞ったときはその銘柄）と並べる",
		"突き合わせの単位は 銘柄 × UTC の分（timestamp の先頭 16 文字）。kabu はその分の最後のスナップショットの price、立花は記録した分の price",
		"価格差 bps = (立花の price − kabu の price) / kabu の price × 10000。mean_abs / median_abs / p95_abs（最近順位）/ max_abs は絶対値、mean_signed は符号つき（正: 立花が高い）",
		"only_tachibana: 立花だけがある分（kabu のスナップショットがない）、only_kabu: kabu だけがある分（立花の記録がない）",
		"by_phase の局面は、立花の行はその行の session_phase、kabu だけの分はその時刻から決める（open_auction=9:00〜9:05 / morning=〜11:25 / lunch=〜12:35 / afternoon=〜15:20 / close=15:20 以降。立会外は記録しない）",
		"立花の行は各分の 5 秒目の最新値（observed_at が値の更新時刻。静かな銘柄は古い値のまま繰り返す）で、kabu はその分の最後のスナップショットのため、差には最大で約 55 秒の値動きが混ざる。速い相場では差が大きく出る",
		"via: event は EVENT の通知から、rest は REST の 時価の取得から入った値（その分に使った最新の値の入り方）",
	}
}
