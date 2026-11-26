package replaytool

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	domainreplay "github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// EstimateNotice is the fixed caveat of every estimate.
const EstimateNotice = "件数 = 判断 ×（その判断を聞くアーム）。費用の見込み max_cost_usd は予約と同じ最悪値側の単価（入力トークンの推定 × " +
	"jevtrialsettings.WorstCaseUSDPerMtok。チャットのアーム〈endpoint=/v1/chat/completions〉は入力と出力の計上単価と出力上限 max_tokens で数え、list_price_usd は null）で数えた額で、run はこの額を超えるところで止まる（list_price_usd は定価での目安。定価が分からないアームがあれば null）。" +
	"お試しと同じ 1 日（JST）の費用上限（全体・アームごと）を共有し、上限に達すると新しい呼び出しを出さずに待つ（翌日に自動で続きから再開する）。" +
	"学習カットオフが公表されている（llm_cutoff がある）アームには、その日より後の判断だけを聞く。公表されていないアームは llm_cutoff_status=unverified（未確認）で、" +
	"その結果は参考扱い（reference_only）。送る入力は保存された入力そのままで、判断より後のデータ（過去の類似事例 RAG を含む）は足さない。" +
	"開始（replay_start）には、この見積もりの confirm_token が要る"

// ArmEstimate is one arm of an estimate.
type ArmEstimate struct {
	ArmID         string  `json:"arm_id"`
	Model         string  `json:"model"`
	PromptVersion string  `json:"prompt_version"`
	LLMCutoff     *string `json:"llm_cutoff"`
	// LLMCutoffStatus is verified (the arm names a published cutoff) or
	// unverified; LLMCutoffLabel is its screen label (確認済み / 未確認).
	LLMCutoffStatus string `json:"llm_cutoff_status"`
	LLMCutoffLabel  string `json:"llm_cutoff_label"`
	// ReferenceOnly is true for an unverified arm: its results are reference-only.
	ReferenceOnly        bool     `json:"reference_only"`
	Calls                int      `json:"calls"`
	ExcludedBeforeCutoff int      `json:"excluded_before_cutoff"`
	MaxCostUSD           float64  `json:"max_cost_usd"`
	ListPriceUSD         *float64 `json:"list_price_usd"`
}

// EstimateView is replay_estimate's data.
type EstimateView struct {
	Notice string `json:"notice"`
	// ReplayEnabled is jev.replay.enabled: a start is refused while it is off.
	ReplayEnabled        bool          `json:"replay_enabled"`
	DecisionType         string        `json:"decision_type"`
	From                 string        `json:"from"`
	To                   string        `json:"to"`
	MaxRows              int           `json:"max_rows"`
	Decisions            int           `json:"decisions"`
	Calls                int           `json:"calls"`
	ExcludedInvalidState int           `json:"excluded_invalid_state"`
	MaxCostUSD           float64       `json:"max_cost_usd"`
	ListPriceUSD         *float64      `json:"list_price_usd"`
	Arms                 []ArmEstimate `json:"arms"`
	// ConfirmToken names this estimate's content; replay_start must present it.
	ConfirmToken string `json:"confirm_token"`
}

func newEstimateView(p jevreplay.Plan, enabled bool) EstimateView {
	v := EstimateView{
		Notice: EstimateNotice, ReplayEnabled: enabled, DecisionType: p.Spec.DecisionType, From: p.Spec.From, To: p.Spec.To, MaxRows: p.Spec.MaxRows,
		Decisions: len(p.Decisions), Calls: len(p.Calls), ExcludedInvalidState: p.ExcludedInvalidState,
		MaxCostUSD: p.MaxCostUSD, ListPriceUSD: p.ListPriceUSD, Arms: make([]ArmEstimate, len(p.Arms)), ConfirmToken: p.Token,
	}
	for i, a := range p.Arms {
		v.Arms[i] = ArmEstimate{
			ArmID: a.Arm.ID, Model: a.Arm.Model, PromptVersion: a.Arm.PromptVersion, LLMCutoff: cutoffDate(a.Arm),
			LLMCutoffStatus: a.CutoffStatus, LLMCutoffLabel: domainreplay.CutoffLabel(a.CutoffStatus), ReferenceOnly: a.CutoffStatus != domainreplay.CutoffVerified,
			Calls: a.Calls, ExcludedBeforeCutoff: a.ExcludedBeforeCutoff, MaxCostUSD: a.MaxCostUSD, ListPriceUSD: a.ListPriceUSD,
		}
	}
	return v
}

func cutoffDate(a jevtrialsettings.Arm) *string {
	if a.LLMCutoff == "" {
		return nil
	}
	return &a.LLMCutoff
}

// plan reads the configured arms and plans the run args asks for over q.
func plan(ctx context.Context, q repo.Querier, r query.Range, a PlanArgs) (jevreplay.Plan, bool, error) {
	if err := a.Validate(); err != nil {
		return jevreplay.Plan{}, false, err
	}
	reader := settingsReader{q}
	enabled, err := jevreplaysettings.Enabled(ctx, reader)
	if err != nil {
		return jevreplay.Plan{}, false, err
	}
	cfg := jevtrialsettings.Load(ctx, reader)
	if cfg.ReadErr != nil {
		return jevreplay.Plan{}, false, cfg.ReadErr
	}
	in, err := a.Request(r).Resolve(cfg.Arms)
	if err != nil {
		return jevreplay.Plan{}, false, err
	}
	p, err := jevreplay.BuildPlan(ctx, q, in)
	return p, enabled, err
}

// Estimate is replay_estimate: the pre-flight view of a run (decisions, calls
// = decisions x arms after the cutoff rule, the worst-case cost and a confirm
// token) over the read-only q. It sends nothing and writes nothing.
func Estimate(ctx context.Context, q query.Q, r query.Range, a PlanArgs) (EstimateView, error) {
	p, enabled, err := plan(ctx, q, r, a)
	if err != nil {
		return EstimateView{}, err
	}
	return newEstimateView(p, enabled), nil
}

// StartArgs are replay_start's arguments besides the period: the run to make
// and the token of its estimate.
type StartArgs struct {
	PlanArgs
	ConfirmToken string `json:"confirm_token"`
}

// maxClientLen bounds the caller's host stored with a run.
const maxClientLen = 64

// Start is replay_start: it re-plans the run inside tx, refuses it unless the
// switch is on, no other run is active, the plan has calls and ConfirmToken is
// exactly the token of this plan (an estimate that changed since is refused), and
// then inserts one pending run. It writes only jev_replay_runs; the application's
// runner executes the run. The caller runs it behind the MCP write switch.
func Start(ctx context.Context, tx *sql.Tx, now time.Time, client string, r query.Range, a StartArgs) (RunView, error) {
	if a.ConfirmToken == "" {
		return RunView{}, ErrStaleEstimate
	}
	p, enabled, err := plan(ctx, tx, r, a.PlanArgs)
	if err != nil {
		return RunView{}, err
	}
	switch {
	case !enabled:
		return RunView{}, ErrDisabled
	case p.Token != a.ConfirmToken:
		return RunView{}, ErrStaleEstimate
	case len(p.Calls) == 0:
		return RunView{}, ErrNothingToReplay
	}
	active, err := repo.CountActive(ctx, tx)
	if err != nil {
		return RunView{}, err
	}
	if active > 0 {
		return RunView{}, ErrActiveRun
	}
	run, err := p.NewRun(truncate(client, maxClientLen))
	if err != nil {
		return RunView{}, err
	}
	id, err := repo.InsertRun(ctx, tx, run, now)
	if err != nil {
		return RunView{}, err
	}
	created, err := repo.GetRun(ctx, tx, id)
	if err != nil {
		return RunView{}, err
	}
	return View(created), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Stop is replay_stop: it sets the stop flag of an active run inside tx (the
// runner ends the run as stopped before its next call). It writes only
// jev_replay_runs. A stop of a finished run is ErrNotActive, of an unknown run
// ErrNotFound.
func Stop(ctx context.Context, tx *sql.Tx, now time.Time, runID int64) (RunView, error) {
	changed, err := repo.RequestStop(ctx, tx, runID, now)
	if err != nil {
		return RunView{}, err
	}
	run, err := repo.GetRun(ctx, tx, runID)
	switch {
	case errors.Is(err, repo.ErrRunNotFound):
		return RunView{}, ErrNotFound
	case err != nil:
		return RunView{}, err
	case !changed:
		return RunView{}, ErrNotActive
	}
	return View(run), nil
}
