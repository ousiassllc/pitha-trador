// Package counterfactual records, for the LONG/SHORT Jev decisions the guards
// skipped and the outcome labeling left without a label, the counterfactual
// triple-barrier label "had it entered" (issue #1052, parent #752).
//
// It is a non-trading record: Policy, Risk, order placement, the Paper guard,
// Live and the Jev prompts never read it (internal/repository/counterlabel
// pins that); the nightly retraining reads it only while
// metalabel.include_skipped_labels is on (default off).
package counterfactual

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/counterlabel"
)

// minAge is how old a skipped candidate must be before the recorder looks at
// it: the 24-hour retry window of the per-minute outcome labeling
// (scheduler.outcomeLabelRetryWindow) plus an hour of margin. Inside it the
// labeling still enqueues the decision's pair itself, so the recorder only
// picks up what that scan left behind (downtime, an outage, a horizon that
// changed since) and no pair is labeled twice. By then the market data of the
// horizon has long landed, so a window that falls short is permanently short.
const minAge = 25 * time.Hour

// batch bounds one run: the oldest skipped candidates are labeled first, the
// rest on the next night's run.
const batch = 2000

// ErrNoDirection is returned for a decision whose direction is not LONG or
// SHORT: a NONE decision has no side to price, and the counterfactual label
// never imputes one.
var ErrNoDirection = errors.New("counterfactual: the decision has no LONG/SHORT direction to label")

// Store is the skipped-candidate record the recorder reads its targets from
// and writes labels to; *counterlabel.Repository implements it.
type Store interface {
	PendingTargets(ctx context.Context, horizon int, from, to time.Time, limit int) ([]counterlabel.Target, error)
	Insert(ctx context.Context, l counterlabel.Label) error
}

// Labeler computes the label of one (decision, horizon) pair without storing it:
// the barrier computation of the live Outcome Labeling, so the counterfactual
// label has the same horizon, entry reference and cost-deducted bps definition.
// *calibration.Labeler implements it. gap is the non-empty reason when the bars
// do not cover the horizon.
type Labeler interface {
	Compute(ctx context.Context, jevDecisionID int64, horizonMinutes int) (decision domain.JevDecision, outcome domain.CalibrationOutcome, gap string, err error)
}

// Skips marks a pair that can never be labeled, as the live labeling does
// (*calibration.CalibrationRepository.MarkUnlabelable), so it is not tried again.
type Skips interface {
	MarkUnlabelable(ctx context.Context, jevDecisionID int64, horizonMinutes int, reason string) error
}

// Recorder records the counterfactual labels. It is also the only path that
// could label past decisions, so it is as conservative as the Issue asks: it
// only looks at decisions made on or after the confirmed metalabel.llm_cutoff
// and does nothing while that cutoff is the provisional placeholder (issue
// #776), so no label overlaps the LLM's training period. A NONE decision is
// never labeled (no direction is imputed).
type Recorder struct {
	store    Store
	labeler  Labeler
	skips    Skips
	settings func(ctx context.Context) (metalabelsettings.Settings, error)
	horizons func(ctx context.Context) ([]int, error)
	now      func() time.Time
}

// New returns a Recorder reading the meta-labeling settings (the LLM cutoff)
// and the label horizons (calibration.TimeoutHorizons) at every run.
func New(store Store, labeler Labeler, skips Skips,
	settings func(ctx context.Context) (metalabelsettings.Settings, error), horizons func(ctx context.Context) ([]int, error)) *Recorder {
	return &Recorder{store: store, labeler: labeler, skips: skips, settings: settings, horizons: horizons, now: time.Now}
}

// WithClock overrides the clock (default time.Now), for tests and for the
// nightly job whose own clock is injectable.
func (r *Recorder) WithClock(now func() time.Time) *Recorder {
	r.now = now
	return r
}

// Run records the counterfactual labels due as of now and returns how many it
// recorded. Nothing is recorded (0, nil) while the LLM cutoff is a placeholder.
func (r *Recorder) Run(ctx context.Context) (int, error) {
	s, err := r.settings(ctx)
	if err != nil {
		return 0, fmt.Errorf("counterfactual: load meta-labeling settings: %w", err)
	}
	if s.CutoffStatus() == metalabelsettings.CutoffPlaceholder {
		slog.InfoContext(ctx, "counterfactual: labels of skipped candidates not recorded: the LLM cutoff is still a placeholder", "llm_cutoff", s.LLMCutoff)
		return 0, nil
	}
	cutoff, err := s.LLMCutoffTime()
	if err != nil {
		return 0, fmt.Errorf("counterfactual: %w", err)
	}
	horizons, err := r.horizons(ctx)
	if err != nil {
		return 0, fmt.Errorf("counterfactual: resolve label horizons: %w", err)
	}

	now := r.now().UTC()
	to := now.Add(-minAge)
	recorded := 0
	for _, horizon := range horizons {
		targets, err := r.store.PendingTargets(ctx, horizon, cutoff, to, batch)
		if err != nil {
			return recorded, fmt.Errorf("counterfactual: %w", err)
		}
		for _, target := range targets {
			ok, err := r.record(ctx, target, horizon, s.LLMCutoff, now)
			if err != nil {
				return recorded, err
			}
			if ok {
				recorded++
			}
		}
	}
	if recorded > 0 {
		slog.InfoContext(ctx, "counterfactual: recorded labels of skipped candidates", "labels", recorded, "llm_cutoff", s.LLMCutoff)
	}
	return recorded, nil
}

// record labels one target; false when there is nothing to record (the window
// never reaches the horizon: the pair is marked, never retried).
func (r *Recorder) record(ctx context.Context, target counterlabel.Target, horizon int, llmCutoff string, now time.Time) (bool, error) {
	decision, outcome, gap, err := r.labeler.Compute(ctx, target.JevDecisionID, horizon)
	if err != nil {
		return false, err
	}
	if decision.Direction == nil || (*decision.Direction != domain.JevDirectionLong && *decision.Direction != domain.JevDirectionShort) {
		return false, fmt.Errorf("%w (decision %d)", ErrNoDirection, target.JevDecisionID)
	}
	if gap != "" {
		if err := r.skips.MarkUnlabelable(ctx, target.JevDecisionID, horizon, gap); err != nil {
			return false, fmt.Errorf("counterfactual: %w", err)
		}
		slog.InfoContext(ctx, "counterfactual: skipped candidate horizon unlabelable, no label recorded",
			"jev_decision_id", target.JevDecisionID, "horizon_minutes", horizon, "reason", gap)
		return false, nil
	}
	if err := r.store.Insert(ctx, counterlabel.Label{Target: target, Outcome: outcome, LLMCutoff: llmCutoff, CreatedAt: now}); err != nil {
		return false, fmt.Errorf("counterfactual: %w", err)
	}
	return true, nil
}
