// Package slicediag is the read-only analysis behind the Paper results MCP's
// jev_evaluation_slices tool (issue #1089): the jev_evaluation LLM arm (the same
// decisions, the same cost-inclusive label, the same cutoff and cohort split, taken
// from evaldiag.Rows) cut by a fixed set of features, with a multiple-comparison
// correction (Holm) inside each family of cuts and a flag on small buckets. The
// package only reads and ranks nothing: no bucket is named best, no threshold or
// verdict is applied, and nothing here feeds Jev, Risk, Policy, an order or the
// meta-model.
package slicediag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-jev-slices.md).
const Description = "jev_evaluation の llm アーム（期間内の trader の LONG/SHORT 全判断・コスト控除後の同じ triple barrier ラベル・cutoff・(model_id, question_version) ごとの別集計は jev_evaluation と同じ）を、" +
	"特徴で切り分けた読み取り専用の集計（研究用。採用基準ではなく、しきい値・判定・推奨は返さない）。" +
	"事前登録した切り口（" + PreregisteredOn + "・issue " + PreregisteredIssue + "）: regime（地合い）・time_of_day（open 寄り直後 / morning 前場 / afternoon 後場 / pre_close 引け前 / other。JST）・direction・confidence_band（[0,0.6) [0.6,0.7) [0.7,0.8) [0.8,0.9) [0.9,1]）。" +
	"後から見た切り口（exploratory。仮説であり確認ではない）: weekday・volatility_band / turnover_band / spread_band（コホート内の三分位）・sector（上位 20 業種 + other）。" +
	"切り口ごと・バケットごとに、件数 n・日数・平均 net bps・日クラスタのブートストラップ 95% 区間・勝率・検出できる最小の差・p 値・Holm 補正後の p 値・low_sample（n<30 または日数<5 の印と理由）を、固定の順で返す（結果で並べ替えず、最良のバケットも示さない）。" +
	"Holm 補正は事前登録 / exploratory を別々の family として、各 family の全バケット（dimensions で絞っても family 全体）で行い、family_size を返す。各切り口の n の合計はコホートの評価行数に一致する。書き込みはしない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	props := evaldiag.SchemaProperties()
	delete(props, "fx_symbol")    // the FX context is jev_evaluation's own (issue #1084); this tool's request has no such field
	delete(props, "index_symbol") // so is the index context (issue #1084)
	props["dimensions"] = map[string]any{
		"type": "array", "uniqueItems": true, "minItems": 1, "maxItems": len(dimensions),
		"items":       map[string]any{"type": "string", "enum": Names()},
		"description": "返す切り口（省略は全部）。Holm 補正の family は絞っても全切り口のまま（絞ると補正が甘くなるため）",
	}
	return props
}

// Request is the tool's arguments as decoded from the call: the period (From, To, as
// every period tool takes it), the jev_evaluation scope arguments (the same
// validation) and the dimensions. Decoding rejects an unknown field and an argument
// Validate refuses.
type Request struct {
	From           *string  `json:"from"`
	To             *string  `json:"to"`
	HorizonMinutes *int     `json:"horizon_minutes"`
	PromptVersion  string   `json:"prompt_version"`
	ModelID        string   `json:"model_id"`
	Dimensions     []string `json:"dimensions"`
}

// UnmarshalJSON decodes strictly and validates, so the dispatcher's decode of a
// bad call is an invalid_argument without a check of its own.
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
	if err := r.evalRequest().Validate(); err != nil {
		return err
	}
	if r.Dimensions == nil {
		return nil
	}
	if len(r.Dimensions) == 0 {
		return fmt.Errorf("slicediag: dimensions must not be empty (omit it for all)")
	}
	seen := map[string]bool{}
	for _, d := range r.Dimensions {
		if !slices.Contains(Names(), d) {
			return fmt.Errorf("slicediag: unknown dimension %q (one of %v)", d, Names())
		}
		if seen[d] {
			return fmt.Errorf("slicediag: dimension %q is listed twice", d)
		}
		seen[d] = true
	}
	return nil
}

func (r Request) evalRequest() evaldiag.Request {
	return evaldiag.Request{From: r.From, To: r.To, HorizonMinutes: r.HorizonMinutes, PromptVersion: r.PromptVersion, ModelID: r.ModelID}
}

// wanted is the dimensions to return in output order.
func (r Request) wanted() []string {
	if r.Dimensions == nil {
		return Names()
	}
	return slices.DeleteFunc(Names(), func(n string) bool { return !slices.Contains(r.Dimensions, n) })
}

// Run cuts the evaluated decisions of [r.From, r.To) and returns the data and the
// number of decisions read. It reads only; the clock is not used (the signature
// matches the other analyses). A period beyond evaldiag's limits is
// query.ErrLimitExceeded, never a partial result.
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	rows, meta, err := evaldiag.Rows(ctx, q, r, req.evalRequest())
	if err != nil {
		return nil, 0, err
	}
	want := req.wanted()
	out := Data{
		From: r.From.In(query.JST).Format(time.DateOnly), To: r.To.In(query.JST).Format(time.DateOnly),
		HorizonMinutes: meta.HorizonMinutes, PromptVersion: req.PromptVersion, ModelID: req.ModelID, Dimensions: want,
		Notice: notice, ExploratoryNote: exploratoryNote, Decisions: meta.Decisions, CutoffApplied: meta.CutoffApplied,
		Preregistration: Preregistration{Date: PreregisteredOn, Issue: PreregisteredIssue, Dimensions: namesOf(FamilyPreregistered), Statement: preregistrationText},
		LabelAssumptions: LabelAssumptions{
			HorizonMinutes: meta.HorizonMinutes, TakeProfitBps: meta.TakeProfitBps, StopLossBps: meta.StopLossBps, BarrierCostBps: meta.BarrierCostBps,
		},
		Statistics: Statistics{
			Method: statisticsText, Alpha: evaldiag.Alpha, Power: evaldiag.Power, Resamples: evaldiag.Resamples, Seed: evaldiag.Seed,
			Correction: correctionText, MinBucketRows: MinBucketRows, MinBucketDays: MinBucketDays,
		},
		Cohorts: []Cohort{}, Definitions: definitions(), Caveats: caveats(meta.CutoffApplied),
	}
	type key struct{ model, version string }
	byCohort := map[key][]evaldiag.Row{}
	for _, row := range rows {
		k := key{row.ModelID, row.QuestionVersion}
		byCohort[k] = append(byCohort[k], row)
	}
	for _, cc := range meta.Cohorts { // oldest cohort first, as jev_evaluation lists them
		c, err := cutCohort(ctx, byCohort[key{cc.ModelID, cc.QuestionVersion}], want)
		if err != nil {
			return nil, 0, err
		}
		c.ModelID, c.QuestionVersion, c.Decisions = cc.ModelID, cc.QuestionVersion, cc.Decisions
		out.Cohorts = append(out.Cohorts, c)
	}
	return out, meta.Decisions, nil
}
