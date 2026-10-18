package training

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// verdict is the adoption decision of one training with the numbers behind it.
type verdict struct {
	status, reason string
	intervals      Intervals
	adoption       repo.Adoption
}

// judge decides whether the freshly evaluated candidate is adopted (issue
// #780, FR-META-7). It applies, in this order and stopping at the first that
// rejects: the OOS bar on confidence intervals, the LLM cutoff gate (#776),
// the comparison with the model in force on the same OOS rows, and the change
// rate limit / freeze. The reason of a rejection is recorded with the model.
func (t *Trainer) judge(ctx context.Context, now time.Time, s metalabelsettings.Settings, split Split, eval Evaluation) (verdict, error) {
	seed := uint64(now.UnixNano())
	v := verdict{intervals: eval.Intervals(t.acceptance, seed), adoption: repo.Adoption{Criteria: t.acceptance.criteria(seed)}}
	v.status, v.reason = t.acceptance.Judge(eval, v.intervals)

	if r, detail := cutoffGate(s, t.mode, split.OOS); r != "" {
		v.status, v.reason = repo.ModelRejected, r
		slog.WarnContext(ctx, "metalabel: model not adopted: the LLM cutoff does not support the OOS evaluation", "reject_reason", r, "detail", detail)
	}
	if v.status != repo.ModelActive {
		return v, nil
	}

	active, hasActive, err := t.store.LatestActiveModel(ctx)
	if err != nil {
		return verdict{}, fmt.Errorf("metalabel: load the active model: %w", err)
	}
	if hasActive {
		t.compareWithActive(ctx, &v, active, seed, split, eval, metalabel.DecideConfig{MinEVBps: s.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction})
		if v.status != repo.ModelActive {
			return v, nil
		}
	}
	return v, t.applyChangeGuard(ctx, &v, now, active, hasActive)
}

// compareWithActive evaluates the model in force on the candidate's OOS rows
// and rejects the candidate unless it may replace it. A stored model this
// build cannot use is not compared with (it cannot serve decisions anyway), so
// it never blocks its own replacement.
func (t *Trainer) compareWithActive(ctx context.Context, v *verdict, active repo.Model, seed uint64, split Split, cand Evaluation, decide metalabel.DecideConfig) {
	parsed, err := metalabel.ParseModel(active)
	if err != nil {
		slog.WarnContext(ctx, "metalabel: the active model is unusable, so the candidate is not compared with it", "version", active.Version, "error", err)
		return
	}
	cmp := Compare(cand, Evaluate(parsed, split.OOS, decide), t.acceptance, seed, active.Version)
	v.adoption.Comparison = &cmp
	adopt, reason := t.acceptance.judgeComparison(cmp)
	slog.InfoContext(ctx, "metalabel: candidate compared with the active model on the same OOS rows",
		"active_version", active.Version, "comparable", cmp.Comparable, "paired_samples", cmp.PairedSamples,
		"candidate_bps", cmp.CandidateBps, "active_bps", cmp.ActiveBps, "diff_bps", cmp.DiffBps,
		"diff_ci", formatInterval(cmp.DiffCI), "active_ev_ci", formatInterval(cmp.ActiveEVCI), "adopt", adopt, "note", cmp.Note)
	if !adopt {
		v.status, v.reason = repo.ModelRejected, reason
	}
}

// applyChangeGuard rejects an otherwise adoptable candidate while the last model
// swap or the last policy.* change is too recent (changeguard), recording the
// reason, the detail and the end of the block with the model.
func (t *Trainer) applyChangeGuard(ctx context.Context, v *verdict, now time.Time, active repo.Model, hasActive bool) error {
	var h changeguard.History
	if hasActive {
		h.LastModelAdoption = active.TrainedAt
	}
	if t.policyChanges != nil {
		at, found, err := t.policyChanges.LastChangeAt(ctx)
		if err != nil {
			return fmt.Errorf("metalabel: load the last policy change: %w", err)
		}
		if found {
			h.LastPolicyChange = at
		}
	}
	block := t.limits.CheckModelAdoption(now, h)
	if block == nil {
		return nil
	}
	v.status, v.reason = repo.ModelRejected, block.Reason
	v.adoption.Gate = &repo.Gate{Reason: block.Reason, Detail: block.Detail, Until: block.Until.UTC().Format(time.RFC3339)}
	slog.WarnContext(ctx, "metalabel: model not adopted: change rate limit", "reject_reason", block.Reason,
		"detail", block.Detail, "allowed_from", block.Until)
	return nil
}
