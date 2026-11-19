// Package trenddiag is the read-only analysis behind the Paper results MCP's
// jev_evaluation_trend tool (issue #1089): how the main numbers of the Jev evaluation moved
// from day to day, read from the daily record jev_evaluation_daily (written once a JST day is
// over by the nightly research task, internal/service/evalrecord) instead of re-labeling every
// day's bars. It only reads that one table and two research settings; nothing here feeds
// Jev, Risk, Policy, an order or the meta-model, and no threshold or verdict is applied.
package trenddiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Limits of one call: a period beyond them is query.ErrLimitExceeded rather than a partial
// result. MaxDays is the longest period (query.MaxRows days) and MaxCohorts the most
// (model_id, question_version) pairs evaldiag evaluates, so the table can hold at most
// this many rows of one period and horizon.
const (
	MaxDays    = query.MaxRows
	MaxCohorts = evaldiag.MaxCohorts
	MaxRows    = MaxDays * MaxCohorts
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-jev-trend.md).
const Description = "Jev（AI）の評価（jev_evaluation）の主な数字を日ごとに並べた読み取り専用の推移（研究用。採用基準ではなく、しきい値・合否も当てない）。" +
	"夜間の研究ジョブ（research.eval.enabled=1 のときだけ動く）が、JST の日が終わった後にその日の評価を (model_id, question_version, ホライズン) ごとに jev_evaluation_daily へ 1 行ずつ記録したものだけを読む" +
	"（ラベルや足は読み直さない。記録は書いた日のもので、あとで設定を変えても書き換わらない）。" +
	"(model_id, question_version, horizon_minutes) ごとの系列として、日別の判断数・評価できた数・除外数（excluded = 判断数 − 評価数。学習 cutoff より前の除外を含む）・エントリー数・勝率（llm の net>0）・" +
	"llm / random / momentum / contrarian の平均 net bps・llm−random の平均・その日のラベル設定（take_profit・stop_loss・コスト・ホライズン）を返す。" +
	"連続する記録日でラベル設定が変わっていれば label_settings_changed=true（変わった日を label_settings_changed_on に）、期間全体の累積は日別行から " +
	"jev_evaluation と同じ日クラスタのブートストラップで作る（llm の平均・区間・勝率・検出できる最小の差・日数、llm−random の平均・区間）。" +
	"recording に記録の最初・最後の日・記録日数・保持日数・記録のスイッチ（research.eval.enabled / retention_days のこの 2 つだけ）を付ける。書き込みはしない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"horizon_minutes": map[string]any{
			"type": "integer", "minimum": barriersettings.MinTimeoutMinutes, "maximum": barriersettings.MaxTimeoutMinutes,
			"description": "記録時のホライズン（分）がこの値の行だけを読む。省略は全ホライズン（ホライズンごとに別の系列）",
		},
		"prompt_version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "この版（question_version。例: trader-v4）の系列だけを返す。省略は全版（版ごとに別の系列）"},
		"model_id":       map[string]any{"type": "string", "maxLength": evaldiag.MaxModelIDLen, "description": "この model_id の系列だけを返す。省略は全モデル（モデルごとに別の系列）"},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From, To, as every
// period tool takes it) and the tool's own. Decoding rejects an unknown field and an argument
// Validate refuses.
type Request struct {
	From           *string `json:"from"`
	To             *string `json:"to"`
	HorizonMinutes *int    `json:"horizon_minutes"`
	PromptVersion  string  `json:"prompt_version"`
	ModelID        string  `json:"model_id"`
}

// UnmarshalJSON decodes strictly and validates, so the dispatcher's decode of a bad call is
// an invalid_argument without a check of its own.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	return r.Validate()
}

// Validate reports an argument the tool does not accept.
func (r Request) Validate() error {
	if h := r.HorizonMinutes; h != nil && (*h < barriersettings.MinTimeoutMinutes || *h > barriersettings.MaxTimeoutMinutes) {
		return fmt.Errorf("trenddiag: horizon_minutes must be %d..%d", barriersettings.MinTimeoutMinutes, barriersettings.MaxTimeoutMinutes)
	}
	if r.PromptVersion != "" {
		if err := jevshadowsettings.ValidateVersion(r.PromptVersion); err != nil {
			return fmt.Errorf("trenddiag: prompt_version: %w", err)
		}
	}
	if len(r.ModelID) > evaldiag.MaxModelIDLen {
		return fmt.Errorf("trenddiag: model_id is at most %d characters", evaldiag.MaxModelIDLen)
	}
	return nil
}

// Run reads the recorded days of [r.From, r.To) and returns the series and the number of
// recorded rows read. It reads only; the clock is not used (the signature matches the other
// analyses).
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	from, to := r.From.In(query.JST).Format(time.DateOnly), r.To.In(query.JST).Format(time.DateOnly)
	rows, ok, err := evalrecord.List(ctx, q, from, to,
		evalrecord.Filter{ModelID: req.ModelID, QuestionVersion: req.PromptVersion, HorizonMinutes: req.HorizonMinutes}, MaxRows)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, query.ErrLimitExceeded
	}
	rec, err := recordingOf(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := Data{
		From: from, To: to, HorizonMinutes: req.HorizonMinutes, PromptVersion: req.PromptVersion, ModelID: req.ModelID,
		Notice: notice, Recording: rec, Series: seriesOf(rows), Definitions: definitions(), Caveats: caveats(rec),
	}
	return out, len(rows), nil
}
