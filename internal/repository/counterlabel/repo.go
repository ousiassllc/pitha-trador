package counterlabel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository reads the skipped candidates still to label and writes their
// counterfactual labels (counterfactual_labels).
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Why a skipped candidate was skipped (counterfactual_labels.skip_kind, and the
// origin of calibration_outcome_origins, which uses the same words).
const (
	// SkipRiskRejected: the Policy Engine passed it and the Risk Engine rejected it.
	SkipRiskRejected = "risk_rejected"
	// SkipGuardSkipped: a Policy guard skipped it (entry quality, flow guards, the
	// meta model, eligibility ...): the latest signal has any other reject reason.
	SkipGuardSkipped = "guard_skipped"
	// SkipNoSignal: the decision has no trade signal at all.
	SkipNoSignal = "no_signal"
)

// riskRejectedReason is the head of trade_signals.reject_reason the Policy Engine
// writes for a Risk Engine rejection (policy.ReasonRiskEngineRejected, with the
// rule after ": "). It is spelled here because the repository layer does not
// import the policy package; the migration's SQL and a test pin the same head.
const riskRejectedReason = "risk_engine_rejected"

// SkipKindOf classifies a skipped candidate from its latest trade signal:
// hasSignal says whether the decision has one and reason is its reject_reason.
func SkipKindOf(hasSignal bool, reason string) string {
	switch {
	case !hasSignal:
		return SkipNoSignal
	case reason == riskRejectedReason || strings.HasPrefix(reason, riskRejectedReason+":"):
		return SkipRiskRejected
	default:
		return SkipGuardSkipped
	}
}

// Target is one skipped LONG/SHORT decision still to label at a horizon.
type Target struct {
	JevDecisionID int64
	// RejectReason is the reason of the decision's latest trade signal ("" when
	// it has none).
	RejectReason string
	// SkipKind is one of SkipRiskRejected, SkipGuardSkipped and SkipNoSignal.
	SkipKind string
}

// targetsQuery args: from, to, label version, horizon, horizon, limit.
//
// A target is a Trader decision answered LONG or SHORT, inside [from, to], that
// the entry never followed (no signal that passed Policy and Risk) and that has
// no label of its own yet: no triple-barrier calibration_outcomes row with a net
// return (so a candidate the per-minute labeling labeled is never computed
// twice and its numbers are never touched), no calibration_label_skips mark for
// the horizon (the window never reaches it) and no counterfactual label at the
// horizon. A NONE
// decision is never a target: no direction is imputed.
const targetsQuery = `
SELECT d.id, ls.id IS NOT NULL, COALESCE(ls.reject_reason, '')
FROM jev_decisions d
LEFT JOIN trade_signals ls ON ls.id = (SELECT MAX(s0.id) FROM trade_signals s0 WHERE s0.jev_decision_id = d.id)
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
	AND d.timestamp >= ? AND d.timestamp <= ?
	AND NOT EXISTS (SELECT 1 FROM trade_signals s WHERE s.jev_decision_id = d.id AND s.risk_passed = 1)
	AND NOT EXISTS (SELECT 1 FROM calibration_outcomes o
		WHERE o.jev_decision_id = d.id AND o.label_version = ? AND o.net_return_bps IS NOT NULL)
	AND NOT EXISTS (SELECT 1 FROM calibration_label_skips k WHERE k.jev_decision_id = d.id AND k.horizon_minutes = ?)
	AND NOT EXISTS (SELECT 1 FROM counterfactual_labels c WHERE c.jev_decision_id = d.id AND c.horizon_minutes = ?)
ORDER BY d.timestamp, d.id
LIMIT ?`

// PendingTargets returns up to limit skipped decisions made in [from, to] that
// have no label at horizon yet (see targetsQuery), oldest first.
func (r *Repository) PendingTargets(ctx context.Context, horizon int, from, to time.Time, limit int) ([]Target, error) {
	rows, err := r.db.QueryContext(ctx, targetsQuery,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to), domain.CalibrationLabelVersionTripleBarrier, horizon, horizon, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list skipped candidates to label at horizon %dm: %w", horizon, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Target
	for rows.Next() {
		var (
			t         Target
			hasSignal bool
		)
		if err := rows.Scan(&t.JevDecisionID, &hasSignal, &t.RejectReason); err != nil {
			return nil, fmt.Errorf("repository: scan skipped candidate: %w", err)
		}
		t.SkipKind = SkipKindOf(hasSignal, t.RejectReason)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list skipped candidates to label at horizon %dm: %w", horizon, err)
	}
	return out, nil
}

// Label is one counterfactual label to store.
type Label struct {
	Target
	// Outcome is the triple-barrier outcome (service/calibration.Labeler):
	// HorizonMinutes, BarrierHit, NetReturnBps and EntryRefPrice must be set.
	Outcome domain.CalibrationOutcome
	// LLMCutoff is the confirmed metalabel.llm_cutoff ("YYYY-MM-DD") in force
	// when the label was recorded.
	LLMCutoff string
	// CutoffUnverified marks a label recorded while the cutoff had a date but no
	// recorded Jev model (metalabelsettings.CutoffUnverified). Such labels are
	// kept but left out of the training and evaluation rows.
	CutoffUnverified bool
	// CreatedAt is the recording time (zero: now).
	CreatedAt time.Time
}

// Insert writes one counterfactual label. A pair already labeled returns the
// UNIQUE constraint violation as-is: each (decision, horizon) is labeled once.
func (r *Repository) Insert(ctx context.Context, l Label) error {
	o := l.Outcome
	if o.NetReturnBps == nil || o.EntryRefPrice == nil || o.BarrierHit == "" {
		return fmt.Errorf("repository: counterfactual label of decision %d lacks the triple-barrier fields", l.JevDecisionID)
	}
	skipKind := l.SkipKind
	if skipKind == "" {
		return fmt.Errorf("repository: counterfactual label of decision %d has no skip kind", l.JevDecisionID)
	}
	createdAt := l.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO counterfactual_labels (
	jev_decision_id, horizon_minutes, reject_reason, skip_kind, barrier_hit, net_return_bps, entry_ref_price, label_version,
	future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff, cutoff_unverified, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.JevDecisionID, o.HorizonMinutes, l.RejectReason, skipKind, o.BarrierHit, *o.NetReturnBps, *o.EntryRefPrice, o.LabelVersion,
		o.FutureReturn, o.MaxAdverseExcursion, o.MaxFavorableExcursion, l.LLMCutoff, l.CutoffUnverified, sqlutil.FormatTime(createdAt),
	); err != nil {
		return fmt.Errorf("repository: insert counterfactual label for decision %d (horizon %dm): %w", l.JevDecisionID, o.HorizonMinutes, err)
	}
	return nil
}
