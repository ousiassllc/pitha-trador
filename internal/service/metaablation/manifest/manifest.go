// Package manifest is the single machine-readable file that pre-registers a
// metaablation run (issue #1046): the model under test, the DB copy, the
// windows, every rule value, the statistics plan and the seed. It is written
// before the evaluation window is looked at and then frozen: Freeze records a
// SHA-256 of the whole content, Verify and CheckModel refuse any later change,
// and a changed threshold is a new run (NewRun) that keeps the earlier runs
// listed and increments the run count.
//
// The pre-registered minimum days, minimum count, meaningful difference
// (delta) and power are pointers that stay nil (unset) until the proposal in
// docs/research/metaablation-thresholds.md is confirmed; while any is nil every
// verdict is "優劣未確定" (see package stats). No default is baked into code.
//
// The manifest never contains the params_json body of the model, only its hash.
package manifest

import "time"

// SchemaVersion is the manifest file format version.
const SchemaVersion = 1

// How the model under test was chosen at init (ModelRef.Selection).
const (
	// SelectionLatestRejected: the newest rejected model at init time (the default).
	SelectionLatestRejected = "latest_rejected"
	// SelectionExplicit: the version named by `init -model`.
	SelectionExplicit = "explicit"
)

// Layers a comparison is run on.
const (
	LayerLabel  = "label"
	LayerReplay = "replay"
)

// Multiple-comparison corrections the statistics plan may name.
const (
	CorrectionBonferroni = "bonferroni"
	CorrectionNone       = "none"
)

// TestMethod is the only supported test: a percentile bootstrap that resamples
// whole trading days.
const TestMethod = "day_cluster_bootstrap_percentile"

// Manifest is the pre-registration of one run.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	// Model is the one rejected model under test, fixed before the evaluation.
	Model ModelRef `json:"model"`
	// DBCopy identifies the database copy every cell reads (the source DB is
	// never opened for writing).
	DBCopy      DBCopyRef `json:"db_copy"`
	CodeVersion string    `json:"code_version"`
	// Cohort is the cohort / Jev version (issues #881, #939) the evaluation is
	// for. It is part of the frozen content: rows of any other cohort are
	// excluded, and the check cannot be switched off.
	Cohort     string     `json:"cohort"`
	Windows    Windows    `json:"windows"`
	Rules      Rules      `json:"rules"`
	Statistics Statistics `json:"statistics"`
	// Minimums are the pre-registered thresholds of the verdict; nil = unset.
	Minimums Minimums `json:"minimums"`
	// Exclusions are the periods and reasons fixed before the run.
	Exclusions []Exclusion `json:"exclusions"`
	// Replay describes the continuous replay's segments and initial state.
	Replay     ReplayPlan `json:"replay_plan"`
	Provenance Provenance `json:"provenance"`
	Run        Run        `json:"run"`
	// Frozen is nil until Freeze; it is excluded from the hash it carries.
	Frozen *Freeze `json:"frozen,omitempty"`
}

// ModelRef pins the model: its version, the SHA-256 (hex) of its stored
// params_json (never the body) and how it was chosen (Selection*). A later run
// reads the model by Version, whatever newer models the copy holds. A manifest
// frozen before issue #1049 has no selection (it was always the latest
// rejected model); it is omitted from the file so that such a manifest keeps
// its hash.
type ModelRef struct {
	Version      string `json:"version"`
	ParamsSHA256 string `json:"params_sha256"`
	Selection    string `json:"selection,omitempty"`
}

// DBCopyRef identifies the database copy and the time it reflects.
type DBCopyRef struct {
	SHA256 string    `json:"sha256"`
	AsOf   time.Time `json:"as_of"`
}

// Windows are the model's own windows (as stored with the model and the
// training split) and the evaluation window [EvalFrom, EvalTo).
//
// TrainEnd is the latest label end among the fit and calibration rows
// (training.Split.TrainEnd), CalibrationEnd the decision time of the newest
// calibration row (or, when only the model's stored row is available, an upper
// bound of it: the stored train_end, which is never earlier; the manifest notes
// it and it can only move the independence boundary later), OOSStart/OOSEnd the original out-of-sample window and
// LabelEnd the latest LabelEnd among every row the model used (fit,
// calibration, original OOS). Independent rows are strictly after all of these
// and after LabelEnd + Embargo (package independence).
type Windows struct {
	TrainStart     time.Time `json:"train_start"`
	TrainEnd       time.Time `json:"train_end"`
	CalibrationEnd time.Time `json:"calibration_end"`
	OOSStart       time.Time `json:"oos_start"`
	OOSEnd         time.Time `json:"oos_end"`
	LabelEnd       time.Time `json:"label_end"`
	EmbargoSeconds int64     `json:"embargo_seconds"`
	LLMCutoff      time.Time `json:"llm_cutoff"`
	EvalFrom       time.Time `json:"eval_from"`
	EvalTo         time.Time `json:"eval_to"`
}

// Embargo is the walk-forward embargo.
func (w Windows) Embargo() time.Duration { return time.Duration(w.EmbargoSeconds) * time.Second }

// Rules are the fixed rule values common to all four cells.
type Rules struct {
	// QualityCut is the Q axis's entry_quality floor, fixed to "good".
	QualityCut  string           `json:"quality_cut"`
	Eligibility EligibilityRules `json:"eligibility"`
	Long        FlowGuards       `json:"long"`
	Short       FlowGuards       `json:"short"`
	// MinEVBps is the M axis's minimum calibrated EV (metalabel.DecideConfig).
	MinEVBps float64     `json:"min_ev_bps"`
	Label    LabelCost   `json:"label"`
	Replay   ReplayRules `json:"replay"`
}

// EligibilityRules are the market conditions of the policy's eligibility step.
type EligibilityRules struct {
	MaxSpreadBps     float64 `json:"max_spread_bps"`
	MinTurnover5mJPY float64 `json:"min_turnover_5m_jpy"`
}

// FlowGuards are the values of the continuation / toxic_flow /
// liquidity_stressed guards, common to all cells.
type FlowGuards struct {
	MinContinuationProbability float64 `json:"min_continuation_probability"`
	MaxToxicFlow               float64 `json:"max_toxic_flow"`
	MaxLiquidityStressed       float64 `json:"max_liquidity_stressed"`
}

// LabelCost states the label layer's proxy outcome (triple barrier, issue
// #774) and the cost it nets out.
type LabelCost struct {
	TPBps          float64 `json:"tp_bps"`
	SLBps          float64 `json:"sl_bps"`
	HorizonMinutes int     `json:"horizon_minutes"`
	CostBps        float64 `json:"cost_bps"`
}

// ReplayRules are the replay layer's capital, risk and fill settings, equal in
// all four cells.
type ReplayRules struct {
	InitialCapitalJPY       float64 `json:"initial_capital_jpy"`
	MaxPositionPerSymbolJPY float64 `json:"max_position_per_symbol_jpy"`
	MaxTotalExposureJPY     float64 `json:"max_total_exposure_jpy"`
	StopLossPct             float64 `json:"stop_loss_pct"`
	FillModel               string  `json:"fill_model"`
	SpreadBps               float64 `json:"spread_bps"`
	SlippageBps             float64 `json:"slippage_bps"`
	FeeBps                  float64 `json:"fee_bps"`
}

// LayerMetric is the primary metric of one layer.
type LayerMetric struct {
	Layer  string `json:"layer"`
	Metric string `json:"metric"`
}

// Statistics is the pre-registered statistics plan: no layer or metric may be
// picked after seeing results, and NumComparisons counts every comparison
// including exploratory ones.
type Statistics struct {
	Method           string        `json:"method"`
	PrimaryMetrics   []LayerMetric `json:"primary_metrics"`
	ComparisonFamily []string      `json:"comparison_family"`
	NumComparisons   int           `json:"num_comparisons"`
	Correction       string        `json:"correction"`
	// Alpha is the family-wise two-sided level before correction.
	Alpha     float64 `json:"alpha"`
	Resamples int     `json:"resamples"`
	Seed      int64   `json:"seed"`
}

// Minimums are the pre-registered thresholds of the verdict. nil means not yet
// decided (the proposal awaits review): every verdict is then 優劣未確定. They
// are never the adoption bar's 370 takes / 11 bps / MinOOSDays.
type Minimums struct {
	MinDays *int `json:"min_days"`
	// MinCount is compared with the paired-difference count of a comparison
	// (the label layer's discordant pairs).
	MinCount *int `json:"min_count"`
	// DeltaLabelBps and DeltaReplayDailyReturn are the smallest meaningful
	// differences (delta) of the label layer (bps per eligible candidate) and
	// of the replay layer (daily return on capital, a ratio).
	DeltaLabelBps          *float64 `json:"delta_label_bps"`
	DeltaReplayDailyReturn *float64 `json:"delta_replay_daily_return"`
	// Power is the pre-registered power the detectable minimum difference is
	// computed at.
	Power *float64 `json:"power"`
}

// UnsetFields names the Minimums fields that are still nil.
func (m Minimums) UnsetFields() []string {
	var unset []string
	if m.MinDays == nil {
		unset = append(unset, "min_days")
	}
	if m.MinCount == nil {
		unset = append(unset, "min_count")
	}
	if m.DeltaLabelBps == nil {
		unset = append(unset, "delta_label_bps")
	}
	if m.DeltaReplayDailyReturn == nil {
		unset = append(unset, "delta_replay_daily_return")
	}
	if m.Power == nil {
		unset = append(unset, "power")
	}
	return unset
}

// Exclusion is a period fixed before the run that is left out, with its reason.
type Exclusion struct {
	Kind   string    `json:"kind"`
	Reason string    `json:"reason"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

// InitialState is the replay state every continuous segment starts from, the
// same in all cells and fixed in advance.
type InitialState struct {
	CapitalJPY        float64 `json:"capital_jpy"`
	ConsecutiveLosses int     `json:"consecutive_losses"`
}

// ReplayPlan fixes how the replay is cut when market data is missing: the
// primary evaluation replays continuously from one initial state; a missing
// stretch the state cannot be carried over splits it into segments common to
// all cells (SegmentRule), each starting from InitialState and reported
// separately, never joined into one equity curve.
type ReplayPlan struct {
	InitialState InitialState `json:"initial_state"`
	SegmentRule  string       `json:"segment_rule"`
}

// Provenance attests that the rule values were fixed without looking at the
// evaluation window; Notes lists what is unknown.
type Provenance struct {
	ChosenBeforeEvaluation bool     `json:"chosen_before_evaluation"`
	Notes                  []string `json:"notes"`
}

// Run counts the runs: a changed threshold is a new run and the earlier runs
// stay listed.
type Run struct {
	Count    int           `json:"count"`
	Previous []PreviousRun `json:"previous"`
}

// PreviousRun is an earlier frozen run of the same study.
type PreviousRun struct {
	Count        int       `json:"count"`
	SHA256       string    `json:"sha256"`
	FrozenAt     time.Time `json:"frozen_at"`
	ChangeReason string    `json:"change_reason"`
}

// Freeze records when the manifest was frozen and the SHA-256 (hex) of its
// content without this field.
type Freeze struct {
	At     time.Time `json:"at"`
	SHA256 string    `json:"sha256"`
}
