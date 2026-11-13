package judgement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// TrialRepository persists jev_trial_decisions rows (issue #1059): what a
// non-live decision model (an arm) answered, or why it did not, for the same
// input as a live jev_decisions row, and the cost counted for the call. Like
// ShadowRepository it is separate from DecisionRepository: nothing that reads
// jev_decisions (RAG, Calibration, the meta-model training, Policy) can see a
// trial row.
type TrialRepository struct {
	db *sql.DB
}

// NewTrialRepository returns a TrialRepository backed by db.
func NewTrialRepository(db *sql.DB) *TrialRepository { return &TrialRepository{db: db} }

const insertTrialSQL = `
INSERT INTO jev_trial_decisions (
	jev_decision_id, arm_id, prompt_version, provider, requested_model, served_model, served_provider, response_id,
	status, failure_code, http_status, error_excerpt,
	direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json,
	latency_ms, input_tokens, output_tokens, cost_usd, cost_source, counted_cost_usd,
	request_bytes, state_bytes, truncation_suspected, llm_cutoff_status, rag_includes_live_model_answers, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes one jev_trial_decisions row. A second row for the same live
// decision and arm violates the UNIQUE constraint and is an error. An empty
// LLMCutoffStatus is stored as jevtrial.CutoffUnverified.
func (r *TrialRepository) Insert(ctx context.Context, d jevtrial.Decision) (jevtrial.Decision, error) {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.LLMCutoffStatus == "" {
		d.LLMCutoffStatus = jevtrial.CutoffUnverified
	}
	res, err := r.db.ExecContext(ctx, insertTrialSQL,
		d.JevDecisionID, d.ArmID, d.PromptVersion, d.Provider, d.RequestedModel, d.ServedModel, d.ServedProvider, d.ResponseID,
		d.Status, d.FailureCode, d.HTTPStatus, d.ErrorExcerpt,
		d.Direction, d.Regime, d.EntryQuality, d.Confidence, d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability, d.ResponseJSON,
		d.LatencyMs, d.InputTokens, d.OutputTokens, d.CostUSD, d.CostSource, d.CountedCostUSD,
		d.RequestBytes, d.StateBytes, d.TruncationSuspected, d.LLMCutoffStatus, d.RAGIncludesLiveModelAnswers, sqlutil.FormatTime(d.CreatedAt))
	if err != nil {
		return jevtrial.Decision{}, fmt.Errorf("repository: insert jev trial decision for decision %d (arm %s): %w", d.JevDecisionID, d.ArmID, err)
	}
	if d.ID, err = res.LastInsertId(); err != nil {
		return jevtrial.Decision{}, fmt.Errorf("repository: read jev trial decision id for decision %d: %w", d.JevDecisionID, err)
	}
	return d, nil
}

// TrialSpend is the number of trial calls and the cost counted against the daily
// caps (jev_trial_decisions.counted_cost_usd: the reported or list-price-estimated
// cost, and for an unknown cost the worst-case figure).
type TrialSpend struct {
	Calls          int
	CountedCostUSD float64
}

// SpentBetween sums the counted cost of every arm's rows created in [from, to).
func (r *TrialRepository) SpentBetween(ctx context.Context, from, to time.Time) (TrialSpend, error) {
	return r.spent(ctx, "", from, to)
}

// SpentByArmBetween sums the counted cost of arm's rows created in [from, to).
func (r *TrialRepository) SpentByArmBetween(ctx context.Context, arm string, from, to time.Time) (TrialSpend, error) {
	return r.spent(ctx, arm, from, to)
}

func (r *TrialRepository) spent(ctx context.Context, arm string, from, to time.Time) (TrialSpend, error) {
	var s TrialSpend
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(counted_cost_usd), 0) FROM jev_trial_decisions
WHERE created_at >= ? AND created_at < ? AND (? = '' OR arm_id = ?)`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to), arm, arm).Scan(&s.Calls, &s.CountedCostUSD)
	if err != nil {
		return TrialSpend{}, fmt.Errorf("repository: sum jev trial cost: %w", err)
	}
	return s, nil
}

// Get returns the trial row with the given id (tests and diagnostics).
func (r *TrialRepository) Get(ctx context.Context, id int64) (jevtrial.Decision, error) {
	var (
		d         jevtrial.Decision
		createdAt string
	)
	err := r.db.QueryRowContext(ctx, `
SELECT id, jev_decision_id, arm_id, prompt_version, provider, requested_model, served_model, served_provider, response_id,
	status, failure_code, http_status, error_excerpt,
	direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, response_json,
	latency_ms, input_tokens, output_tokens, cost_usd, cost_source, counted_cost_usd,
	request_bytes, state_bytes, truncation_suspected, llm_cutoff_status, rag_includes_live_model_answers, created_at
FROM jev_trial_decisions WHERE id = ?`, id).Scan(
		&d.ID, &d.JevDecisionID, &d.ArmID, &d.PromptVersion, &d.Provider, &d.RequestedModel, &d.ServedModel, &d.ServedProvider, &d.ResponseID,
		&d.Status, &d.FailureCode, &d.HTTPStatus, &d.ErrorExcerpt,
		&d.Direction, &d.Regime, &d.EntryQuality, &d.Confidence, &d.ToxicFlow, &d.LiquidityStressed, &d.ContinuationProbability, &d.ResponseJSON,
		&d.LatencyMs, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.CostSource, &d.CountedCostUSD,
		&d.RequestBytes, &d.StateBytes, &d.TruncationSuspected, &d.LLMCutoffStatus, &d.RAGIncludesLiveModelAnswers, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return jevtrial.Decision{}, ErrDecisionNotFound
	}
	if err != nil {
		return jevtrial.Decision{}, fmt.Errorf("repository: get jev trial decision %d: %w", id, err)
	}
	if d.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return jevtrial.Decision{}, err
	}
	return d, nil
}
