package metalabel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// TrainingScope is which Trader decisions count as meta-label training rows,
// on top of the label conditions of ListTrainingRowsFrom. The nightly
// retraining (its fit, calibration and OOS windows are all cut from the rows
// of one scope), the forward evaluation, the skip report's counts and the Paper
// results MCP's metalabel_training_readiness all build their rows and counts
// from the same scope (ResolveTrainingScope + Where), so they cannot drift.
type TrainingScope struct {
	// Foreign are the Trader prompt versions left out (issue #881): another
	// cohort than the live prompt's. Empty: every version.
	Foreign []string
	// JevModelID, when set, keeps only the decisions this Jev model answered
	// (jev_decisions.model_id). It is the model that answered last
	// (ResolveTrainingScope), so a window in which Jev's model changed is not
	// trained and evaluated across two models: the fit and the OOS window
	// would otherwise describe different models and the LLM cutoff check
	// (training.cutoffGate) would reject the result as llm_model_changed.
	// Empty: every model.
	JevModelID string
	// IncludeSkippedLabels adds, to the rows of ListTrainingRowsFrom, the
	// counterfactual labels of skipped candidates (counterfactual_labels, issue
	// #1052). false (the zero value, and the default of the Settings switch
	// metalabel.include_skipped_labels) leaves the row set exactly as it was.
	// It is not a SQL condition: Where ignores it.
	IncludeSkippedLabels bool
}

// ResolveTrainingScope returns the scope of the decisions made before to:
// foreign are left out, and the Jev model is the one that answered the latest
// Trader decision (any direction) of the remaining versions before to. With no
// such decision, or one whose model_id is empty, no model filter applies.
func ResolveTrainingScope(ctx context.Context, q Querier, to time.Time, foreign []string) (TrainingScope, error) {
	scope := TrainingScope{Foreign: foreign}
	cond := "d.decision_type = 'trader' AND d.timestamp < ?"
	args := []any{sqlutil.FormatTime(to)}
	if versionCond, versionArgs := sqlutil.NotIn("d.question_version", foreign); versionCond != "" {
		cond += " AND " + versionCond
		args = append(args, versionArgs...)
	}
	//nolint:gosec // G202: cond is assembled from package-internal fragments; every value is a bound parameter
	rows, err := q.QueryContext(ctx, `SELECT d.model_id FROM jev_decisions d WHERE `+cond+` ORDER BY d.timestamp DESC, d.id DESC LIMIT 1`, args...)
	if err != nil {
		return TrainingScope{}, fmt.Errorf("repository: resolve the latest Jev model before %s: %w", to, err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&scope.JevModelID); err != nil {
			return TrainingScope{}, fmt.Errorf("repository: scan the latest Jev model: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return TrainingScope{}, fmt.Errorf("repository: resolve the latest Jev model: %w", err)
	}
	return scope, nil
}

// Where is the scope as SQL conditions on a jev_decisions row aliased d, joined
// with AND, or "" and no arguments when the scope leaves every decision in.
func (s TrainingScope) Where() (string, []any) {
	var conds []string
	var args []any
	if c, a := sqlutil.NotIn("d.question_version", s.Foreign); c != "" {
		conds, args = append(conds, c), append(args, a...)
	}
	if s.JevModelID != "" {
		conds, args = append(conds, "d.model_id = ?"), append(args, s.JevModelID)
	}
	return strings.Join(conds, " AND "), args
}
