package jevreplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

const insertResultSQL = `
INSERT INTO jev_replay_results (
	run_id, jev_decision_id, arm_id, prompt_version, provider, requested_model, served_model, served_provider, response_id,
	status, failure_code, http_status, error_excerpt,
	direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json,
	latency_ms, input_tokens, output_tokens, cost_usd, cost_source, counted_cost_usd,
	request_bytes, state_bytes, truncation_suspected, llm_cutoff_status, reference_only, rag_includes_live_model_answers, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// InsertResult stores one result row and adds it to its run's progress (calls
// done, failed, counted cost) in one transaction, so the run's numbers always
// equal its rows. A second row for the same (run, decision, arm) violates the
// UNIQUE constraint and is an error.
func (r *Repository) InsertResult(ctx context.Context, res jevreplay.Result) error {
	d := res.Decision
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.LLMCutoffStatus == "" {
		d.LLMCutoffStatus = jevreplay.CutoffUnverified
	}
	// The database refuses an unverified row without the mark; make the mark follow the status.
	reference := res.ReferenceOnly || d.LLMCutoffStatus != jevreplay.CutoffVerified
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin jev replay result transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, insertResultSQL,
		res.RunID, d.JevDecisionID, d.ArmID, d.PromptVersion, d.Provider, d.RequestedModel, d.ServedModel, d.ServedProvider, d.ResponseID,
		d.Status, d.FailureCode, d.HTTPStatus, d.ErrorExcerpt,
		d.Direction, d.Regime, d.EntryQuality, d.Confidence, d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability, d.ResponseJSON,
		d.LatencyMs, d.InputTokens, d.OutputTokens, d.CostUSD, d.CostSource, d.CountedCostUSD,
		d.RequestBytes, d.StateBytes, d.TruncationSuspected, d.LLMCutoffStatus, reference, d.RAGIncludesLiveModelAnswers, sqlutil.FormatTime(d.CreatedAt)); err != nil {
		return fmt.Errorf("repository: insert jev replay result (run %d, decision %d, arm %s): %w", res.RunID, d.JevDecisionID, d.ArmID, err)
	}
	failed := 0
	if d.Status == jevtrial.StatusFailed {
		failed = 1
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE jev_replay_runs SET calls_done = calls_done + 1, calls_failed = calls_failed + ?, counted_cost_usd = counted_cost_usd + ?, updated_at = ?
WHERE id = ?`, failed, d.CountedCostUSD, sqlutil.FormatTime(d.CreatedAt), res.RunID); err != nil {
		return fmt.Errorf("repository: add jev replay result to run %d: %w", res.RunID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit jev replay result: %w", err)
	}
	return nil
}

// Key names one planned call: a live decision asked of an arm.
type Key struct {
	DecisionID int64
	ArmID      string
}

// DoneKeys returns the (decision, arm) pairs the run already has a row for, so
// a restarted run continues where it stopped and never asks a pair twice.
func (r *Repository) DoneKeys(ctx context.Context, runID int64) (map[Key]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT jev_decision_id, arm_id FROM jev_replay_results WHERE run_id = ?`, runID)
	if err != nil {
		return nil, fmt.Errorf("repository: read done jev replay calls of run %d: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	done := map[Key]struct{}{}
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.DecisionID, &k.ArmID); err != nil {
			return nil, fmt.Errorf("repository: scan done jev replay calls: %w", err)
		}
		done[k] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read done jev replay calls of run %d: %w", runID, err)
	}
	return done, nil
}

// Spend is the number of replay calls and the cost counted against the daily
// caps (jev_replay_results.counted_cost_usd, the same counting as the trial's).
type Spend struct {
	Calls          int
	CountedCostUSD float64
}

// SpentBetween sums the counted cost of every run's rows created in [from, to).
func (r *Repository) SpentBetween(ctx context.Context, from, to time.Time) (Spend, error) {
	return r.spent(ctx, "", from, to)
}

// SpentByArmBetween sums the counted cost of arm's rows created in [from, to).
func (r *Repository) SpentByArmBetween(ctx context.Context, arm string, from, to time.Time) (Spend, error) {
	return r.spent(ctx, arm, from, to)
}

func (r *Repository) spent(ctx context.Context, arm string, from, to time.Time) (Spend, error) {
	var s Spend
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(counted_cost_usd), 0) FROM jev_replay_results
WHERE created_at >= ? AND created_at < ? AND (? = '' OR arm_id = ?)`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to), arm, arm).Scan(&s.Calls, &s.CountedCostUSD)
	if err != nil {
		return Spend{}, fmt.Errorf("repository: sum jev replay cost: %w", err)
	}
	return s, nil
}

// FailedByArmBetween counts arm's failed rows created in [from, to): the day's
// failed calls that bound an arm like the trial's failed-call counter.
func (r *Repository) FailedByArmBetween(ctx context.Context, arm string, from, to time.Time) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM jev_replay_results WHERE arm_id = ? AND status = 'failed' AND created_at >= ? AND created_at < ?`,
		arm, sqlutil.FormatTime(from), sqlutil.FormatTime(to)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repository: count failed jev replay calls: %w", err)
	}
	return n, nil
}

// GetResult returns the result row with the given id (tests and diagnostics).
func (r *Repository) GetResult(ctx context.Context, id int64) (jevreplay.Result, error) {
	var (
		res       jevreplay.Result
		d         = &res.Decision
		createdAt string
	)
	err := r.db.QueryRowContext(ctx, `
SELECT id, run_id, jev_decision_id, arm_id, prompt_version, provider, requested_model, served_model, served_provider, response_id,
	status, failure_code, http_status, error_excerpt,
	direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json,
	latency_ms, input_tokens, output_tokens, cost_usd, cost_source, counted_cost_usd,
	request_bytes, state_bytes, truncation_suspected, llm_cutoff_status, reference_only, rag_includes_live_model_answers, created_at
FROM jev_replay_results WHERE id = ?`, id).Scan(
		&d.ID, &res.RunID, &d.JevDecisionID, &d.ArmID, &d.PromptVersion, &d.Provider, &d.RequestedModel, &d.ServedModel, &d.ServedProvider, &d.ResponseID,
		&d.Status, &d.FailureCode, &d.HTTPStatus, &d.ErrorExcerpt,
		&d.Direction, &d.Regime, &d.EntryQuality, &d.Confidence, &d.ToxicFlow, &d.LiquidityStressed, &d.ContinuationProbability, &d.ResponseJSON,
		&d.LatencyMs, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.CostSource, &d.CountedCostUSD,
		&d.RequestBytes, &d.StateBytes, &d.TruncationSuspected, &d.LLMCutoffStatus, &res.ReferenceOnly, &d.RAGIncludesLiveModelAnswers, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return jevreplay.Result{}, fmt.Errorf("repository: jev replay result %d not found", id)
		}
		return jevreplay.Result{}, fmt.Errorf("repository: get jev replay result %d: %w", id, err)
	}
	if d.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return jevreplay.Result{}, err
	}
	return res, nil
}
