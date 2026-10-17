package calibration

import "github.com/ousiassllc/pitha-trador/internal/domain/metamodel"

// The adoption record of a meta-labeling model (issue #780) in
// `GET /api/v1/calibration`'s `meta_model`.

type intervalOutput struct {
	Low  float64 `json:"low" doc:"Lower confidence limit."`
	High float64 `json:"high" doc:"Upper confidence limit."`
}

type criteriaOutput struct {
	MinOOSSamples int     `json:"min_oos_samples" doc:"Fewest OOS samples an evaluation is trusted on."`
	MinTakes      int     `json:"min_takes" doc:"Fewest OOS signals the model must have taken."`
	MinOOSDays    int     `json:"min_oos_days" doc:"Fewest JST trading days the OOS samples must span."`
	MinEVLowerBps float64 `json:"min_ev_lower_bps" doc:"The lower confidence limit of the OOS EV must exceed this (bps)."`
	MinAUCLower   float64 `json:"min_auc_lower" doc:"The lower confidence limit of the OOS AUC must exceed this."`
	Alpha         float64 `json:"alpha" doc:"One-sided tail of the bootstrap intervals (0.01 = a 98% interval)."`
	Resamples     int     `json:"resamples" doc:"Bootstrap resamples."`
}

type comparisonOutput struct {
	ActiveVersion string          `json:"active_version" doc:"The model in force the candidate was compared with."`
	Comparable    bool            `json:"comparable" doc:"False when the comparison was not possible (the current model was then kept); see note."`
	Note          string          `json:"note" doc:"Why it was not comparable; empty otherwise."`
	PairedSamples int             `json:"paired_samples" doc:"OOS samples both models scored."`
	CandidateBps  float64         `json:"candidate_bps" doc:"Candidate's mean net return per OOS decision opportunity (a skip earns 0), bps."`
	ActiveBps     float64         `json:"active_bps" doc:"The active model's, on the same samples, bps."`
	DiffBps       float64         `json:"diff_bps" doc:"candidate_bps - active_bps."`
	DiffCI        *intervalOutput `json:"diff_ci,omitempty" doc:"Cluster-bootstrap interval of diff_bps; omitted when undefined."`
}

type gateOutput struct {
	Reason string `json:"reason" doc:"model_swap_rate_limited / policy_change_freeze."`
	Detail string `json:"detail" doc:"Human-readable detail (trading days passed of the window)."`
	Until  string `json:"until" doc:"When the block ends (RFC 3339)."`
}

type forwardOutput struct {
	EvaluatedAt string          `json:"evaluated_at" doc:"When it was last evaluated (RFC 3339)."`
	From        string          `json:"from" doc:"Start of the forward window: the model's trained_at (RFC 3339)."`
	Samples     int             `json:"samples" doc:"Labeled decisions made after training."`
	Takes       int             `json:"takes" doc:"Of those, the signals the model took."`
	Days        int             `json:"days" doc:"JST trading days they span."`
	EVBps       *float64        `json:"ev_bps" nullable:"true" doc:"Mean net return per taken signal (bps); null without a take."`
	EVCI        *intervalOutput `json:"ev_ci,omitempty" doc:"Cluster-bootstrap interval of ev_bps; omitted when undefined."`
	Degraded    bool            `json:"degraded" doc:"True when the interval's upper limit is below 0: the model loses money out of sample."`
}

func intervalResponse(iv *metamodel.Interval) *intervalOutput {
	if iv == nil {
		return nil
	}
	return &intervalOutput{Low: iv.Low, High: iv.High}
}

func criteriaResponse(c *metamodel.Criteria) *criteriaOutput {
	if c == nil {
		return nil
	}
	return &criteriaOutput{
		MinOOSSamples: c.MinOOSSamples, MinTakes: c.MinTakes, MinOOSDays: c.MinOOSDays,
		MinEVLowerBps: c.MinEVLowerBps, MinAUCLower: c.MinAUCLower, Alpha: c.Alpha, Resamples: c.Resamples,
	}
}

func comparisonResponse(c *metamodel.Comparison) *comparisonOutput {
	if c == nil {
		return nil
	}
	return &comparisonOutput{
		ActiveVersion: c.ActiveVersion, Comparable: c.Comparable, Note: c.Note, PairedSamples: c.PairedSamples,
		CandidateBps: c.CandidateBps, ActiveBps: c.ActiveBps, DiffBps: c.DiffBps, DiffCI: intervalResponse(c.DiffCI),
	}
}

func gateResponse(g *metamodel.Gate) *gateOutput {
	if g == nil {
		return nil
	}
	return &gateOutput{Reason: g.Reason, Detail: g.Detail, Until: g.Until}
}

// forwardResponse is nil until the model was evaluated forward.
func forwardResponse(f metamodel.Forward) *forwardOutput {
	if f.EvaluatedAt == "" {
		return nil
	}
	return &forwardOutput{
		EvaluatedAt: f.EvaluatedAt, From: f.From, Samples: f.Samples, Takes: f.Takes, Days: f.Days,
		EVBps: f.EVBps, EVCI: intervalResponse(f.EVCI), Degraded: f.Degraded,
	}
}
