package manifest

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// Validate reports every way the manifest is not a complete pre-registration.
// Unset Minimums are valid (they make every verdict 優劣未確定); everything
// else must be present.
func (m Manifest) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf("manifest: "+format, args...)) }
	if m.SchemaVersion != SchemaVersion {
		add("schema_version %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	if m.CreatedAt.IsZero() {
		add("created_at is required")
	}
	if m.Model.Version == "" || !isSHA256(m.Model.ParamsSHA256) {
		add("model version and params_sha256 (64 hex) are required")
	}
	if !isSHA256(m.DBCopy.SHA256) || m.DBCopy.AsOf.IsZero() {
		add("db_copy sha256 (64 hex) and as_of are required")
	}
	if m.CodeVersion == "" {
		add("code_version is required")
	}
	if strings.TrimSpace(m.Cohort) == "" {
		add("cohort is required (the cohort / Jev version the evaluation is for)")
	}
	errs = append(errs, m.validateWindows()...)
	errs = append(errs, m.validateRules()...)
	errs = append(errs, m.validateStatistics()...)
	errs = append(errs, m.validateMinimums()...)
	for i, e := range m.Exclusions {
		if e.Kind == "" || e.Reason == "" || !e.From.Before(e.To) {
			add("exclusions[%d] needs kind, reason and from < to", i)
		}
	}
	if s := m.Replay.InitialState; !(s.CapitalJPY > 0) || s.ConsecutiveLosses < 0 || strings.TrimSpace(m.Replay.SegmentRule) == "" {
		add("replay_plan needs a positive initial capital, losses >= 0 and a segment_rule")
	}
	if !m.Provenance.ChosenBeforeEvaluation {
		add("provenance.chosen_before_evaluation must be true: rule values are fixed before the evaluation window is read")
	}
	if m.Run.Count < 1 || len(m.Run.Previous) != m.Run.Count-1 {
		add("run.count %d must be 1 + the %d listed previous runs", m.Run.Count, len(m.Run.Previous))
	}
	return errors.Join(errs...)
}

func (m Manifest) validateWindows() []error {
	var errs []error
	w := m.Windows
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("manifest: windows: "+format, args...))
	}
	for name, t := range map[string]bool{
		"train_start": w.TrainStart.IsZero(), "train_end": w.TrainEnd.IsZero(), "calibration_end": w.CalibrationEnd.IsZero(),
		"oos_start": w.OOSStart.IsZero(), "oos_end": w.OOSEnd.IsZero(), "label_end": w.LabelEnd.IsZero(),
		"llm_cutoff": w.LLMCutoff.IsZero(), "eval_from": w.EvalFrom.IsZero(), "eval_to": w.EvalTo.IsZero(),
	} {
		if t {
			add("%s is required", name)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	switch {
	case w.EmbargoSeconds < 0:
		add("embargo_seconds must be >= 0")
	case w.TrainEnd.Before(w.TrainStart) || w.CalibrationEnd.Before(w.TrainStart):
		add("train_end and calibration_end must not precede train_start")
	case w.OOSEnd.Before(w.OOSStart):
		add("oos_end must not precede oos_start")
	case w.LabelEnd.Before(w.TrainEnd):
		add("label_end must not precede train_end")
	case !w.EvalFrom.Before(w.EvalTo):
		add("eval_from must be before eval_to")
	}
	return errs
}

func (m Manifest) validateRules() []error {
	var errs []error
	r := m.Rules
	if r.QualityCut != metaablation.QualityCut {
		errs = append(errs, fmt.Errorf("manifest: rules.quality_cut %q, must be fixed to %q", r.QualityCut, metaablation.QualityCut))
	}
	for name, v := range map[string]float64{
		"eligibility.max_spread_bps": r.Eligibility.MaxSpreadBps, "eligibility.min_turnover_5m_jpy": r.Eligibility.MinTurnover5mJPY,
		"long.min_continuation_probability": r.Long.MinContinuationProbability, "long.max_toxic_flow": r.Long.MaxToxicFlow,
		"long.max_liquidity_stressed": r.Long.MaxLiquidityStressed, "short.min_continuation_probability": r.Short.MinContinuationProbability,
		"short.max_toxic_flow": r.Short.MaxToxicFlow, "short.max_liquidity_stressed": r.Short.MaxLiquidityStressed,
		"min_ev_bps": r.MinEVBps, "label.cost_bps": r.Label.CostBps, "replay.spread_bps": r.Replay.SpreadBps,
		"replay.slippage_bps": r.Replay.SlippageBps, "replay.fee_bps": r.Replay.FeeBps,
	} {
		if !finite(v) || v < 0 {
			errs = append(errs, fmt.Errorf("manifest: rules.%s %v must be a finite number >= 0", name, v))
		}
	}
	if !(r.Label.TPBps > 0) || !(r.Label.SLBps > 0) || r.Label.HorizonMinutes <= 0 {
		errs = append(errs, errors.New("manifest: rules.label needs positive tp_bps, sl_bps and horizon_minutes"))
	}
	rp := r.Replay
	if !(rp.InitialCapitalJPY > 0) || !(rp.MaxPositionPerSymbolJPY > 0) || !(rp.MaxTotalExposureJPY > 0) || !(rp.StopLossPct > 0) || rp.FillModel == "" {
		errs = append(errs, errors.New("manifest: rules.replay needs positive capital, exposure limits, stop_loss_pct and a fill_model"))
	}
	return errs
}

func (m Manifest) validateStatistics() []error {
	var errs []error
	s := m.Statistics
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("manifest: statistics: "+format, args...))
	}
	if s.Method != TestMethod {
		add("method %q, want %q", s.Method, TestMethod)
	}
	layers := map[string]bool{}
	for _, lm := range s.PrimaryMetrics {
		if (lm.Layer != LayerLabel && lm.Layer != LayerReplay) || lm.Metric == "" || layers[lm.Layer] {
			add("primary_metrics needs one non-empty metric for each of the layers %q and %q", LayerLabel, LayerReplay)
			break
		}
		layers[lm.Layer] = true
	}
	if !layers[LayerLabel] || !layers[LayerReplay] {
		add("primary_metrics must register a metric for both layers %q and %q (a layer cannot be added or dropped after seeing results)", LayerLabel, LayerReplay)
	}
	var primary []string
	for _, c := range metaablation.Comparisons() {
		if c.Primary {
			primary = append(primary, c.Name)
		}
	}
	family := slices.Clone(s.ComparisonFamily)
	slices.Sort(family)
	slices.Sort(primary)
	if !slices.Equal(family, primary) {
		add("comparison_family %v must be exactly the four one-axis comparisons %v", s.ComparisonFamily, primary)
	}
	if min := len(layers) * len(primary); s.NumComparisons < min {
		add("num_comparisons %d is below the %d comparisons of the family over the registered layers (count exploratory ones too)", s.NumComparisons, min)
	}
	if s.Correction != CorrectionBonferroni && s.Correction != CorrectionNone {
		add("correction %q must be %q or %q", s.Correction, CorrectionBonferroni, CorrectionNone)
	}
	if !(s.Alpha > 0 && s.Alpha < 1) || s.Resamples < 1000 {
		add("alpha must be in (0, 1) and resamples >= 1000")
	}
	return errs
}

func (m Manifest) validateMinimums() []error {
	var errs []error
	mm := m.Minimums
	if mm.MinDays != nil && *mm.MinDays < 2 {
		errs = append(errs, errors.New("manifest: minimums.min_days must be >= 2 when set (a bootstrap over days needs two)"))
	}
	if mm.MinCount != nil && *mm.MinCount < 1 {
		errs = append(errs, errors.New("manifest: minimums.min_count must be >= 1 when set"))
	}
	for name, p := range map[string]*float64{"delta_label_bps": mm.DeltaLabelBps, "delta_replay_daily_return": mm.DeltaReplayDailyReturn} {
		if p != nil && !(finite(*p) && *p > 0) {
			errs = append(errs, fmt.Errorf("manifest: minimums.%s must be > 0 when set", name))
		}
	}
	if mm.Power != nil && !(*mm.Power > 0 && *mm.Power < 1) {
		errs = append(errs, errors.New("manifest: minimums.power must be in (0, 1) when set"))
	}
	return errs
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
