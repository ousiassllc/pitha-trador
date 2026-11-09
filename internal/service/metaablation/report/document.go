// Package report renders the aggregate output of a metaablation run (issue
// #1046): a JSON document and a Markdown file, written to a directory outside
// the database. It carries only counts, bps and yen aggregates and the
// manifest: no secret, no raw price or order-book value, no state_json /
// response_json content and no model parameter body.
//
// The replay layer's result comes in as the plain ReplaySection defined here;
// the wiring step maps the replay package's output onto it. Whatever the
// layers' state, the document keeps the three populations apart, says that the
// label is a proxy, that the four one-axis comparisons are the primary ones and
// that 優劣未確定 means no verdict, and shows the reason of every layer whose
// prerequisites failed while the valid results of the other layer stay.
package report

import (
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// LabelSection is table 1: either its result or the reason it could not run.
type LabelSection struct {
	Available bool               `json:"available"`
	Reason    string             `json:"reason,omitempty"`
	Result    *labellayer.Result `json:"result,omitempty"`
}

// UnavailablePeriod is a stretch the replay layer cannot run (e.g. market
// snapshots missing) and why. The label layer still runs on it.
type UnavailablePeriod struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// DailySummary summarises a daily series (yen per day).
type DailySummary struct {
	Days         int     `json:"days"`
	MeanJPY      float64 `json:"mean_jpy"`
	BestDayJPY   float64 `json:"best_day_jpy"`
	WorstDayJPY  float64 `json:"worst_day_jpy"`
	PositiveDays int     `json:"positive_days"`
}

// ReplayCell is one cell's result over one replay segment (or, in a reference
// experiment, over independent daily resets).
type ReplayCell struct {
	Name string `json:"name"`
	// EligibleCandidates, Entries are the counts; Stages are the stage drops
	// in the live order including the replay-only stages (risk, fill).
	EligibleCandidates int                     `json:"eligible_candidates"`
	Entries            int                     `json:"entries"`
	Stages             []eligibility.StageDrop `json:"stages"`
	PnLJPY             float64                 `json:"pnl_jpy"`
	ReturnOnCapital    float64                 `json:"return_on_capital"`
	EndEquityJPY       float64                 `json:"end_equity_jpy"`
	// MaxDrawdown* are read off the daily equity curve of the segment: a
	// description, not a tested quantity.
	MaxDrawdownJPY   float64      `json:"max_drawdown_jpy"`
	MaxDrawdownRatio float64      `json:"max_drawdown_ratio"`
	Daily            DailySummary `json:"daily"`
}

// ReplaySegment is a stretch replayed continuously from one initial state, the
// same in all four cells. Segments are never joined into one equity curve.
type ReplaySegment struct {
	Name         string                `json:"name"`
	From         string                `json:"from"`
	To           string                `json:"to"`
	Days         int                   `json:"days"`
	InitialState manifest.InitialState `json:"initial_state"`
	Cells        []ReplayCell          `json:"cells"`
	// Differences are the daily differences (To - From) with their intervals
	// and verdicts: the four primary comparisons, then the diagonals.
	Differences []stats.Result `json:"daily_differences"`
}

// ReplayReference is a daily-independent-reset experiment. It is reference
// only and is not a continuous equity curve or drawdown.
type ReplayReference struct {
	Description string       `json:"description"`
	Cells       []ReplayCell `json:"cells"`
}

// ReplaySection is table 2: either its result or why the layer is not
// executable. The days the layer ran are the same in all four cells.
type ReplaySection struct {
	Available          bool                `json:"available"`
	Reason             string              `json:"reason,omitempty"`
	UnavailablePeriods []UnavailablePeriod `json:"unavailable_periods"`
	Segments           []ReplaySegment     `json:"segments"`
	// DailyResetReference is optional and always labelled reference.
	DailyResetReference *ReplayReference `json:"daily_reset_reference,omitempty"`
}

// Input is what a run hands to Build.
type Input struct {
	Manifest manifest.Manifest
	Label    LabelSection
	Replay   ReplaySection
	// ExploratoryComparisons counts the comparisons run beyond the primary
	// family (they must be within the manifest's registered total).
	ExploratoryComparisons int
}

// Population is one of the three populations that are never mixed.
type Population struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Population  string `json:"population"`
	Outcome     string `json:"outcome"`
	CapitalRisk string `json:"capital_and_risk"`
}

// Multiple records the multiple-comparison handling.
type Multiple struct {
	Registered    int     `json:"registered_comparisons"`
	Executed      int     `json:"executed_comparisons"`
	Primary       int     `json:"primary_per_layer"`
	Correction    string  `json:"correction"`
	Alpha         float64 `json:"alpha"`
	AlphaAdjusted float64 `json:"alpha_adjusted"`
	// FamilyExceeded is set when more comparisons ran than were registered
	// (exploratory ones included). The level is then Alpha over the larger
	// count and every verdict is downgraded to 優劣未確定 (the intervals were
	// drawn at the registered level), with the reason below; no layer is
	// discarded.
	FamilyExceeded       bool   `json:"family_exceeded"`
	FamilyExceededReason string `json:"family_exceeded_reason,omitempty"`
}

// Document is the report written as JSON.
type Document struct {
	Title    string            `json:"title"`
	Manifest manifest.Manifest `json:"manifest"`
	// RunCount is the number of runs of this study including this one.
	RunCount       int           `json:"run_count"`
	Populations    []Population  `json:"populations"`
	ProxyCaveat    string        `json:"proxy_caveat"`
	PrimaryPairs   []string      `json:"primary_comparisons"`
	DiagonalNote   string        `json:"diagonal_note"`
	VerdictMeaning string        `json:"verdict_meaning"`
	UnsetMinimums  []string      `json:"unset_minimums"`
	Approval       string        `json:"db_copy_approval"`
	Multiple       Multiple      `json:"multiple_comparisons"`
	Label          LabelSection  `json:"label_layer"`
	Replay         ReplaySection `json:"replay_layer"`
	Statements     []string      `json:"statements"`
}

// Build assembles the document from a frozen manifest and the layers' results.
func Build(in Input) (Document, error) {
	m := in.Manifest
	if err := m.Verify(); err != nil {
		return Document{}, err
	}
	if in.Label.Available != (in.Label.Result != nil) {
		return Document{}, errors.New("report: the label layer must have a result exactly when it is available")
	}
	if !in.Label.Available && in.Label.Reason == "" || !in.Replay.Available && in.Replay.Reason == "" {
		return Document{}, errors.New("report: a layer that is not available needs its reason")
	}
	var primary []string
	for _, c := range metaablation.Comparisons() {
		if c.Primary {
			primary = append(primary, fmt.Sprintf("%s: %s → %s", c.Name, c.From.Name(), c.To.Name()))
		}
	}
	// The registered family is, per layer, the four primary pairs. A replay
	// layer cut into segments reports the same four pairs per segment: those are
	// the same pre-registered comparisons on separate stretches of one study, not
	// new family members. The diagonals are descriptive (no interval, no verdict)
	// and are not comparisons of the family; exploratory comparisons are counted
	// via Input.ExploratoryComparisons.
	executed := in.ExploratoryComparisons
	if in.Label.Available {
		executed += in.Label.Result.PrimaryComparisons
	}
	if in.Replay.Available {
		executed += len(primary)
	}
	registered := m.Statistics.NumComparisons
	effective := max(registered, executed)
	plan := stats.Plan{Alpha: m.Statistics.Alpha, Correction: m.Statistics.Correction, NComparisons: effective}
	label, replay := in.Label, in.Replay
	multiple := Multiple{
		Registered: registered, Executed: executed, Primary: len(primary), Correction: m.Statistics.Correction,
		Alpha: m.Statistics.Alpha, AlphaAdjusted: plan.AlphaAdjusted(),
	}
	if executed > registered {
		multiple.FamilyExceeded = true
		multiple.FamilyExceededReason = fmt.Sprintf("実行した比較数 %d が事前登録の全比較数 %d を超えた。区間は登録した数の水準で引いたので、判定はすべて優劣未確定に下げた（補正後の水準は %d 件で再計算する）。次の実行の manifest に実際の比較数を登録する。", executed, registered, executed)
		label, replay = downgrade(label, replay)
	}
	doc := Document{
		Title: "メタモデル × entry_quality 足切り 2x2 比較（オフライン・研究用）", Manifest: m, RunCount: m.Run.Count,
		Populations: populations(), ProxyCaveat: proxyCaveat(m), PrimaryPairs: primary,
		DiagonalNote:   "対角（M と Q の両方が変わる 2 組）と交互作用は参考であり、判定（優位確認／劣位確認／優劣未確定）を付けない。主比較にしない。",
		VerdictMeaning: verdictMeaning,
		UnsetMinimums:  m.Minimums.UnsetFields(),
		Approval:       "DB の写しの取得は別途の承認が要る。この実行は承認済みの写しに対してだけ行い、元の DB には書き込まない。本書は設定変更・発注・モデルの active 昇格の許可ではなく、利益を保証しない。",
		Multiple:       multiple,
		Label:          label, Replay: replay,
		Statements: []string{
			"ラベル層と再生層は別の母集団・別の損益で、混ぜない。片方の層が実行不可でも、もう片方の有効な結果は残す。",
			"元のモデル評価（元 OOS の再採点）は参考で、独立と書かず、採用・優劣の根拠にしない。",
			"主比較のファミリーは各層の 4 組。再生層を区間に分けたときの区間別の差は同じ 4 組を別の区間で見たもので、新しい比較としては数えない。対角の 2 組は記述の差だけで、区間も判定も持たず、ファミリーに数えない（探索は実行回数・比較数に足す）。",
			"この結果は採用基準（370 take・約 11 bps・最小 OOS 日数）の判定ではない。",
		},
	}
	if doc.Replay.UnavailablePeriods == nil {
		doc.Replay.UnavailablePeriods = []UnavailablePeriod{}
	}
	return doc, nil
}

const verdictMeaning = "優劣未確定: 区間が 0 を含む、日数・件数が事前登録の最小に届かない、観測した分散で検出できる最小の差が意味のある差 δ より大きい、または事前登録の最小日数・最小件数・δ・検出力のどれかが未設定（提案のレビュー待ち）のいずれか。「差がない」という意味ではなく、点推定だけで優劣は書かない。"

func proxyCaveat(m manifest.Manifest) string {
	l := m.Rules.Label
	return fmt.Sprintf("ラベル基準の net_return_bps は proxy（トリプルバリア TP %.0f bps / SL %.0f bps・%d 分・コスト %.1f bps。issue #774）で、再生層の約定・出口の損益とは別物。", l.TPBps, l.SLBps, l.HorizonMinutes, l.CostBps)
}

func populations() []Population {
	return []Population{
		{"i", "(i) 元のモデル評価（参考）", "学習・元 OOS が使う行: LONG/SHORT の trader 判断で label_version=2・net_return_bps あり・8 特徴量そろい。ガード・適格条件・Risk を通っていない。", "ラベル基準の net_return_bps（proxy）", "なし"},
		{"ii", "(ii) 表 1: ラベル層", "(i) のうち独立な行で適格条件を通った候補（共通）。Risk・資金なし。", "ラベル基準の net_return_bps（proxy）", "なし"},
		{"iii", "(iii) 表 2: 再生層", "(ii) のうち、そのセルの Q・M と Risk・資金制約を通って実際にエントリーされたもの。", "再生した約定・出口の円・資金比", "あり"},
	}
}

// downgrade returns copies of the layers with every verdict lowered to
// 優劣未確定 (reason family_exceeded); the inputs are left untouched.
func downgrade(l LabelSection, r ReplaySection) (LabelSection, ReplaySection) {
	lower := func(rs []stats.Result) []stats.Result {
		out := make([]stats.Result, len(rs))
		for i, x := range rs {
			if !x.Reference {
				x.Verdict = stats.Undetermined
				x.Reasons = append(append([]string(nil), x.Reasons...), ReasonFamilyExceeded)
			}
			out[i] = x
		}
		return out
	}
	if l.Result != nil {
		res := *l.Result
		res.Comparisons = make([]labellayer.ComparisonResult, len(l.Result.Comparisons))
		for i, c := range l.Result.Comparisons {
			c.Result = lower([]stats.Result{c.Result})[0]
			res.Comparisons[i] = c
		}
		l.Result = &res
	}
	segs := make([]ReplaySegment, len(r.Segments))
	for i, sg := range r.Segments {
		sg.Differences = lower(sg.Differences)
		segs[i] = sg
	}
	r.Segments = segs
	return l, r
}

// ReasonFamilyExceeded is added to the reasons of a verdict that was lowered
// because more comparisons ran than were registered.
const ReasonFamilyExceeded = "family_exceeded"
