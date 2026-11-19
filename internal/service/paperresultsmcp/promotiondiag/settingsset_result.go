package promotiondiag

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"

// Detail codes of the items of an experiment / setting candidate (besides the DetailXxx of items.go).
const (
	// DetailUnpairedPeriods: before and after are different trades, not the same rows (item 1).
	DetailUnpairedPeriods = "not_applicable_unpaired_periods"
	// DetailNoAICost: the trades' pnl carries no AI cost (item 9).
	DetailNoAICost = "not_applicable_ai_cost_not_in_trade_pnl"
	// DetailNoCutoffFilter: trades are not filtered by an LLM training cutoff (item 10).
	DetailNoCutoffFilter = "not_applicable_trades_not_cutoff_filtered"
	// DetailCriterionUnit: the criterion is in bps of a decision; an experiment's interval is in yen per trade.
	DetailCriterionUnit = "criterion_in_bps_not_comparable_to_yen_per_trade"
	// DetailNoExperimentResult: no tuning experiment of the setting has been recorded.
	DetailNoExperimentResult = "no_experiment_result"
	// DetailModelOrPromptChanged: the Jev model or prompt version changed apart from the experiment's own variable.
	DetailModelOrPromptChanged = "model_or_prompt_version_changed"
	// DetailNoTradeDays: a period has no closed trade, so its days cannot be compared.
	DetailNoTradeDays = "no_trade_days_in_a_period"
	// detailExperimentPrefix + the experiment's verdict: its before / after comparison is withheld.
	detailExperimentPrefix = "experiment_"
)

// ExperimentInfo is the experiment a settings-set result is read from. It names the changed variable but not
// its before / after values.
type ExperimentInfo struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	VariableKind string `json:"variable_kind"`
	VariableName string `json:"variable_name"`
	// Verdict and Judged are experiment_compare's own.
	Verdict     string `json:"verdict"`
	Judged      bool   `json:"judged"`
	Confounders int    `json:"confounders"`
	BeforeFrom  string `json:"before_from"`
	BeforeTo    string `json:"before_to"`
	AfterFrom   string `json:"after_from"`
	AfterTo     string `json:"after_to"`
}

// ExperimentNumbers are the numbers behind the items of an experiment / setting candidate. Yen are net of
// trading costs; a trade's day is its JST close date; only days with a closed trade count.
type ExperimentNumbers struct {
	BeforeTrades   int      `json:"before_trades"`
	AfterTrades    int      `json:"after_trades"`
	BeforeDays     int      `json:"before_days"`
	AfterDays      int      `json:"after_days"`
	BeforeMeanJPY  *float64 `json:"before_mean_jpy_per_trade"`
	AfterMeanJPY   *float64 `json:"after_mean_jpy_per_trade"`
	DiffMeanJPY    *float64 `json:"diff_mean_jpy_per_trade"`
	DiffCILowerJPY *float64 `json:"diff_ci_lower_jpy_per_trade"`
	DiffCIUpperJPY *float64 `json:"diff_ci_upper_jpy_per_trade"`
	PValue         *float64 `json:"p_value"`
	HolmAdjustedP  *float64 `json:"holm_adjusted_p"`
	// FamilySize is every recorded experiment (the others counted as p=1) plus family_size_extra.
	FamilySize           int      `json:"family_size"`
	FamilySizeExtra      int      `json:"family_size_extra"`
	PositiveDayPct       *float64 `json:"positive_day_pct"`
	BeforeMeanDayJPY     *float64 `json:"before_mean_day_jpy"`
	RestWithoutBestDay   *float64 `json:"after_minus_before_without_best_day_jpy"`
	BeforeMaxDrawdownJPY float64  `json:"before_max_drawdown_jpy"`
	AfterMaxDrawdownJPY  float64  `json:"after_max_drawdown_jpy"`
}

// rows is the number of rows the result is computed on, for the tool's log line.
func (r *Result) rows() int {
	switch {
	case r.KeyNumbers != nil:
		return r.KeyNumbers.CommonRows
	case r.ExperimentNumbers != nil:
		return min(r.ExperimentNumbers.BeforeTrades, r.ExperimentNumbers.AfterTrades)
	}
	return 0
}

func infoOf(c experiment.Comparison) *ExperimentInfo {
	e := c.Experiment
	return &ExperimentInfo{
		ID: e.ID, Status: e.Status, VariableKind: e.Variable.Kind, VariableName: e.Variable.Name,
		Verdict: c.Verdict, Judged: c.Judged, Confounders: len(c.Confounders),
		BeforeFrom: c.Before.From, BeforeTo: c.Before.To, AfterFrom: c.After.From, AfterTo: c.After.To,
	}
}
