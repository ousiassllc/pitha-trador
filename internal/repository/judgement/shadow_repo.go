package judgement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ShadowRepository persists jev_shadow_decisions rows (issue #872): what a
// candidate Trader prompt version answered for the same input as a live
// jev_decisions row, and the cost of that call. It is deliberately separate
// from DecisionRepository: nothing that reads jev_decisions (RAG, Calibration,
// the meta-model training, Policy) can ever see a shadow row.
type ShadowRepository struct {
	db *sql.DB
}

// NewShadowRepository returns a ShadowRepository backed by db.
func NewShadowRepository(db *sql.DB) *ShadowRepository { return &ShadowRepository{db: db} }

const insertShadowSQL = `
INSERT INTO jev_shadow_decisions (
	jev_decision_id, prompt_version, model_id, response_json,
	direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability,
	latency_ms, input_tokens, request_cost, cost_is_estimate, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes one jev_shadow_decisions row. A second row for the same live
// decision and version violates the UNIQUE constraint and is an error.
func (r *ShadowRepository) Insert(ctx context.Context, d jevshadow.Decision) (jevshadow.Decision, error) {
	createdAt := d.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	res, err := r.db.ExecContext(ctx, insertShadowSQL,
		d.JevDecisionID, d.PromptVersion, d.ModelID, d.ResponseJSON,
		d.Direction, d.Regime, d.EntryQuality, d.Confidence, d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability,
		d.LatencyMs, d.InputTokens, d.RequestCost, d.CostIsEstimate, sqlutil.FormatTime(createdAt))
	if err != nil {
		return jevshadow.Decision{}, fmt.Errorf("repository: insert jev shadow decision for decision %d (%s): %w", d.JevDecisionID, d.PromptVersion, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return jevshadow.Decision{}, fmt.Errorf("repository: read jev shadow decision id for decision %d: %w", d.JevDecisionID, err)
	}
	d.ID = id
	d.CreatedAt = createdAt
	return d, nil
}

// Spend is a number of shadow calls and their estimated cost in USD.
// EstimatedCalls and EstimatedCostUSD are the part of them whose input tokens
// were estimated from the request's size because the response carried no usage
// (cost_is_estimate = 1); they are included in Calls and CostUSD.
type Spend struct {
	Calls            int
	CostUSD          float64
	EstimatedCalls   int
	EstimatedCostUSD float64
}

// spendColumns is the SELECT list that fills a Spend.
const spendColumns = `COUNT(*), COALESCE(SUM(request_cost), 0), COALESCE(SUM(cost_is_estimate), 0),
	COALESCE(SUM(CASE WHEN cost_is_estimate = 1 THEN request_cost ELSE 0 END), 0)`

// SpentBetween sums the shadow cost of the rows created in [from, to).
func (r *ShadowRepository) SpentBetween(ctx context.Context, from, to time.Time) (Spend, error) {
	var s Spend
	err := r.db.QueryRowContext(ctx,
		`SELECT `+spendColumns+` FROM jev_shadow_decisions WHERE created_at >= ? AND created_at < ?`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to)).Scan(&s.Calls, &s.CostUSD, &s.EstimatedCalls, &s.EstimatedCostUSD)
	if err != nil {
		return Spend{}, fmt.Errorf("repository: sum jev shadow cost: %w", err)
	}
	return s, nil
}

// TotalSpent sums the shadow cost of every row (the cumulative figure).
func (r *ShadowRepository) TotalSpent(ctx context.Context) (Spend, error) {
	var s Spend
	err := r.db.QueryRowContext(ctx,
		`SELECT `+spendColumns+` FROM jev_shadow_decisions`).Scan(&s.Calls, &s.CostUSD, &s.EstimatedCalls, &s.EstimatedCostUSD)
	if err != nil {
		return Spend{}, fmt.Errorf("repository: sum total jev shadow cost: %w", err)
	}
	return s, nil
}

// DayCost is the shadow cost of one JST day.
type DayCost struct {
	Date string
	Spend
}

// DailyCosts returns the shadow cost by JST day, newest day first, at most
// limit days.
func (r *ShadowRepository) DailyCosts(ctx context.Context, limit int) ([]DayCost, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT substr(datetime(created_at, '+9 hours'), 1, 10) AS day, `+spendColumns+`
FROM jev_shadow_decisions GROUP BY day ORDER BY day DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: jev shadow daily cost: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DayCost
	for rows.Next() {
		var d DayCost
		if err := rows.Scan(&d.Date, &d.Calls, &d.CostUSD, &d.EstimatedCalls, &d.EstimatedCostUSD); err != nil {
			return nil, fmt.Errorf("repository: scan jev shadow daily cost: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate jev shadow daily cost: %w", err)
	}
	return out, nil
}

// Get returns the shadow row with the given id (tests and diagnostics).
func (r *ShadowRepository) Get(ctx context.Context, id int64) (jevshadow.Decision, error) {
	var (
		d         jevshadow.Decision
		createdAt string
	)
	err := r.db.QueryRowContext(ctx, `
SELECT id, jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
	confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, cost_is_estimate, created_at
FROM jev_shadow_decisions WHERE id = ?`, id).Scan(&d.ID, &d.JevDecisionID, &d.PromptVersion, &d.ModelID, &d.ResponseJSON,
		&d.Direction, &d.Regime, &d.EntryQuality, &d.Confidence, &d.ToxicFlow, &d.LiquidityStressed, &d.ContinuationProbability,
		&d.LatencyMs, &d.InputTokens, &d.RequestCost, &d.CostIsEstimate, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return jevshadow.Decision{}, ErrDecisionNotFound
	}
	if err != nil {
		return jevshadow.Decision{}, fmt.Errorf("repository: get jev shadow decision %d: %w", id, err)
	}
	if d.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return jevshadow.Decision{}, err
	}
	return d, nil
}

// InsertPromotion appends one jev_prompt_promotions row through ex (a
// transaction when it must commit with the runtime_settings change it
// records).
func InsertPromotion(ctx context.Context, ex sqlutil.Execer, p jevshadow.Promotion) (jevshadow.Promotion, error) {
	res, err := ex.ExecContext(ctx,
		`INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES (?, ?, ?, ?, ?)`,
		sqlutil.FormatTime(p.At), p.Actor, p.Client, p.FromVersion, p.ToVersion)
	if err != nil {
		return jevshadow.Promotion{}, fmt.Errorf("repository: insert jev prompt promotion %s -> %s: %w", p.FromVersion, p.ToVersion, err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return jevshadow.Promotion{}, fmt.Errorf("repository: read jev prompt promotion id: %w", err)
	}
	return p, nil
}

// ListPromotions returns up to limit promotions, newest first.
func ListPromotions(ctx context.Context, q sqlutil.Querier, limit int) ([]jevshadow.Promotion, error) {
	var out []jevshadow.Promotion
	err := sqlutil.Each(ctx, q, func(row sqlutil.RowScanner) (jevshadow.Promotion, error) {
		var (
			p  jevshadow.Promotion
			at string
		)
		if err := row.Scan(&p.ID, &at, &p.Actor, &p.Client, &p.FromVersion, &p.ToVersion); err != nil {
			return jevshadow.Promotion{}, err
		}
		t, err := sqlutil.ParseTime(at)
		p.At = t
		return p, err
	}, func(p jevshadow.Promotion) error {
		out = append(out, p)
		return nil
	}, `SELECT id, at, actor, client, from_version, to_version FROM jev_prompt_promotions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list jev prompt promotions: %w", err)
	}
	return out, nil
}
