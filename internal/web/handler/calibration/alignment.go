package calibration

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

// AlignmentSource supplies the label-versus-actual-exit view (issue #774) for
// `GET /api/v1/calibration/exit-alignment`. A CalibrationSource that also
// implements it (internal/bootstrap/exitalignjob.CalibrationSource) enables the
// endpoint's data; without it the endpoint answers an empty table.
type AlignmentSource interface {
	// Alignment returns the table, the warnings, the comparison of the
	// decisions of the last window and the OOS-versus-paper comparison.
	Alignment(ctx context.Context, window time.Duration) (exitalign.LabelExitAlignment, error)
}

// Register mounts the Calibration API routes on api.
func (h *CalibrationHandler) Register(api huma.API) {
	huma.Get(api, "/calibration", h.APICalibration)
	huma.Get(api, "/calibration/exit-alignment", h.APIAlignment)
}

// defaultAlignmentDays is the comparison window when the query gives none.
const defaultAlignmentDays = 30

// AlignmentAPIInput is the query of `GET /api/v1/calibration/exit-alignment`.
type AlignmentAPIInput struct {
	Days int `query:"days" minimum:"1" maximum:"365" default:"30" doc:"Length in days of the window (ending now) of decisions compared between the label and the replay."`
}

type alignmentRowOutput struct {
	Key        string `json:"key" enum:"take_profit,stop_loss,horizon,cost,trailing_stop" doc:"Which condition the row compares."`
	Item       string `json:"item" doc:"Display name of the condition."`
	LabelText  string `json:"label_text" doc:"The triple-barrier label's value."`
	ExitText   string `json:"exit_text" doc:"The actual exit's (or fill model's) value."`
	Comparable bool   `json:"comparable" doc:"False when the label cannot express the condition (trailing stop): there is nothing to compare."`
	Aligned    bool   `json:"aligned" doc:"True when the two agree within the tolerance."`
}

type alignmentWarningOutput struct {
	Code    string `json:"code" doc:"take_profit_mismatch, stop_loss_mismatch, horizon_mismatch, cost_mismatch or cost_direction_asymmetry."`
	Message string `json:"message" doc:"Operator-facing description of the mismatch (Japanese)."`
}

type bpsDistributionOutput struct {
	Count  int     `json:"count" doc:"Sample size."`
	Mean   float64 `json:"mean" doc:"Mean (bps)."`
	StdDev float64 `json:"std_dev" doc:"Population standard deviation (bps)."`
	Min    float64 `json:"min" doc:"Smallest value (bps)."`
	P5     float64 `json:"p5" doc:"5th percentile (bps)."`
	P25    float64 `json:"p25" doc:"25th percentile (bps)."`
	Median float64 `json:"median" doc:"Median (bps)."`
	P75    float64 `json:"p75" doc:"75th percentile (bps)."`
	P95    float64 `json:"p95" doc:"95th percentile (bps)."`
	Max    float64 `json:"max" doc:"Largest value (bps)."`
}

func distributionResponse(d exitalign.DistributionBps) bpsDistributionOutput {
	return bpsDistributionOutput{Count: d.Count, Mean: d.Mean, StdDev: d.StdDev, Min: d.Min, P5: d.P5, P25: d.P25, Median: d.Median, P75: d.P75, P95: d.P95, Max: d.Max}
}

type reasonCountOutput struct {
	Reason string `json:"reason" doc:"Exit reason (positions.exit_reason values)."`
	Count  int    `json:"count" doc:"Number of positions or replays that ended for this reason."`
}

func reasonCountsResponse(in []exitalign.ReasonCount) []reasonCountOutput {
	out := make([]reasonCountOutput, len(in))
	for i, c := range in {
		out[i] = reasonCountOutput{Reason: c.Reason, Count: c.Count}
	}
	return out
}

type labelExitComparisonOutput struct {
	From              time.Time             `json:"from" doc:"Start of the compared decision window (inclusive)."`
	To                time.Time             `json:"to" doc:"End of the compared decision window (exclusive)."`
	HorizonMinutes    int                   `json:"horizon_minutes" doc:"The label timeout (Settings) of the compared labels."`
	ReplayedCount     int                   `json:"replayed_count" doc:"Decisions with both the label and the replayed net return."`
	SkippedCount      int                   `json:"skipped_count" doc:"Labeled decisions whose bars do not cover the exit rules' maximum holding time."`
	LabelNetBps       bpsDistributionOutput `json:"label_net_bps" doc:"Distribution of the label's net return (bps, after cost)."`
	ReplayNetBps      bpsDistributionOutput `json:"replay_net_bps" doc:"Distribution of the net return of the same decisions replayed under the actual exit rules (bps, after the same cost)."`
	DiffBps           bpsDistributionOutput `json:"diff_bps" doc:"Distribution of replay minus label per decision (bps). A negative mean means the label is optimistic against the actual exit."`
	LabelTPCount      int                   `json:"label_tp_count" doc:"Replayed decisions whose label touched the take-profit first."`
	LabelSLCount      int                   `json:"label_sl_count" doc:"Replayed decisions whose label touched the stop-loss first."`
	LabelTimeoutCount int                   `json:"label_timeout_count" doc:"Replayed decisions whose label timed out."`
	ReplayExits       []reasonCountOutput   `json:"replay_exits" doc:"Exit reasons of the replay, most frequent first."`
	PaperExits        []reasonCountOutput   `json:"paper_exits" doc:"Exit reasons (positions.exit_reason) of the paper positions closed in the window, for comparison."`
	NotEvaluatedExits []string              `json:"not_evaluated_exits" doc:"Exit conditions the replay cannot evaluate from the stored data."`
	CostLongBps       float64               `json:"cost_long_bps" doc:"The LONG label cost deducted in the replay (bps)."`
	CostShortBps      float64               `json:"cost_short_bps" doc:"The SHORT label cost deducted in the replay (bps)."`
}

type oosPaperOutput struct {
	ModelVersion string    `json:"model_version" doc:"Version of the newest trained model."`
	OOSStart     time.Time `json:"oos_start" doc:"Start of the model's out-of-sample window."`
	OOSEnd       time.Time `json:"oos_end" doc:"End of the model's out-of-sample window."`
	OOSEVBps     *float64  `json:"oos_ev_bps" nullable:"true" doc:"The model's OOS expected value (bps); null when undefined."`
	PaperTrades  int       `json:"paper_trades" doc:"Paper positions closed in the OOS window."`
	PaperMeanBps *float64  `json:"paper_mean_bps" nullable:"true" doc:"Mean realized return (bps, realized_pnl net of fees over the entry notional) of those positions; null without positions (データ無し)."`
	DiffBps      *float64  `json:"diff_bps" nullable:"true" doc:"paper_mean_bps - oos_ev_bps; null unless both exist."`
}

// AlignmentAPIOutput is the Huma response body for
// `GET /api/v1/calibration/exit-alignment`.
type AlignmentAPIOutput struct {
	Body struct {
		Rows             []alignmentRowOutput       `json:"rows" doc:"The label basis versus the actual exit and fill-model cost."`
		Warnings         []alignmentWarningOutput   `json:"warnings" doc:"Mismatches between the label basis and the actual exit or the fill-model cost; empty when everything agrees within the tolerance."`
		ToleranceBps     float64                    `json:"tolerance_bps" doc:"Provisional tolerance for the take-profit/stop-loss levels (bps)."`
		ToleranceMinutes float64                    `json:"tolerance_minutes" doc:"Provisional tolerance for the timeout versus the maximum holding time (minutes)."`
		Comparison       *labelExitComparisonOutput `json:"comparison,omitempty" doc:"Per-decision label net return versus the same decisions replayed under the actual exit rules, computed on read; omitted when no source is wired."`
		OOSPaper         *oosPaperOutput            `json:"oos_paper,omitempty" doc:"The newest model's OOS EV beside the mean realized paper return over the same window; omitted when no model was ever trained."`
	}
}

// APIAlignment implements `GET /api/v1/calibration/exit-alignment`
// (docs/api/endpoints/huma-api-insights.md, issue #774).
func (h *CalibrationHandler) APIAlignment(ctx context.Context, in *AlignmentAPIInput) (*AlignmentAPIOutput, error) {
	days := in.Days
	if days <= 0 {
		days = defaultAlignmentDays
	}
	out := &AlignmentAPIOutput{}
	out.Body.Rows = []alignmentRowOutput{}
	out.Body.Warnings = []alignmentWarningOutput{}
	source, ok := h.source.(AlignmentSource)
	if !ok {
		return out, nil
	}
	a, err := source.Alignment(ctx, time.Duration(days)*24*time.Hour)
	if err != nil {
		return nil, huma.Error500InternalServerError("load exit alignment failed", err)
	}
	for _, r := range a.Rows {
		out.Body.Rows = append(out.Body.Rows, alignmentRowOutput{Key: r.Key, Item: r.Item, LabelText: r.LabelText, ExitText: r.ExitText, Comparable: r.Comparable, Aligned: r.Aligned})
	}
	for _, w := range a.Warnings {
		out.Body.Warnings = append(out.Body.Warnings, alignmentWarningOutput{Code: w.Code, Message: w.Message})
	}
	out.Body.ToleranceBps, out.Body.ToleranceMinutes = a.ToleranceBps, a.ToleranceMinutes
	if c := a.Comparison; c != nil {
		out.Body.Comparison = &labelExitComparisonOutput{
			From: c.From, To: c.To, HorizonMinutes: c.HorizonMinutes, ReplayedCount: c.ReplayedCount, SkippedCount: c.SkippedCount,
			LabelNetBps: distributionResponse(c.LabelNetBps), ReplayNetBps: distributionResponse(c.ReplayNetBps), DiffBps: distributionResponse(c.DiffBps),
			LabelTPCount: c.LabelTPCount, LabelSLCount: c.LabelSLCount, LabelTimeoutCount: c.LabelTimeoutCount,
			ReplayExits: reasonCountsResponse(c.ReplayExits), PaperExits: reasonCountsResponse(c.PaperExits),
			NotEvaluatedExits: append([]string{}, c.NotEvaluatedExits...), CostLongBps: c.CostLongBps, CostShortBps: c.CostShortBps,
		}
	}
	if o := a.OOSPaper; o != nil {
		out.Body.OOSPaper = &oosPaperOutput{ModelVersion: o.ModelVersion, OOSStart: o.OOSStart, OOSEnd: o.OOSEnd, OOSEVBps: o.OOSEVBps, PaperTrades: o.PaperTrades, PaperMeanBps: o.PaperMeanBps, DiffBps: o.DiffBps}
	}
	return out, nil
}
