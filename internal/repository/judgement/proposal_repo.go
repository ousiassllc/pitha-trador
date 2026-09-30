package judgement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrPolicyProposalNotFound is returned by ProposalRepository methods
// when no matching policy_proposals row exists.
var ErrPolicyProposalNotFound = errors.New("repository: policy proposal not found")

// ProposalRepository persists policy_proposals rows: Sol's improvement
// proposals, Opus's shadow-backtest review, and the eventual apply/
// rollback outcome (docs/architecture/er.md §policy_proposals,
// functional.md §4.14 FR-SELFIMPROVE-1〜7).
type ProposalRepository struct {
	db *sql.DB
}

// NewProposalRepository returns a ProposalRepository backed by db.
func NewProposalRepository(db *sql.DB) *ProposalRepository {
	return &ProposalRepository{db: db}
}

const insertProposalSQL = `
INSERT INTO policy_proposals (
	proposed_at, proposed_by, rationale_json, proposed_changes_json, status, created_at
) VALUES (?, ?, ?, ?, ?, ?)`

// Insert writes a single policy_proposals row with status=pending
// (FR-SELFIMPROVE-1: internal/service/selfimprove.Governor records a
// Sol proposal here before running its shadow backtest). p.Status,
// p.ProposedBy default to domain.PolicyProposalStatusPending/"sol" when
// unset.
func (r *ProposalRepository) Insert(ctx context.Context, p domain.PolicyProposal) (domain.PolicyProposal, error) {
	proposedAt := p.ProposedAt
	if proposedAt.IsZero() {
		proposedAt = time.Now().UTC()
	}
	proposedBy := p.ProposedBy
	if proposedBy == "" {
		proposedBy = "sol"
	}
	status := p.Status
	if status == "" {
		status = domain.PolicyProposalStatusPending
	}
	createdAt := p.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := r.db.ExecContext(ctx, insertProposalSQL,
		sqlutil.FormatTime(proposedAt), proposedBy, p.RationaleJSON, p.ProposedChangesJSON, status, sqlutil.FormatTime(createdAt),
	)
	if err != nil {
		return domain.PolicyProposal{}, fmt.Errorf("repository: insert policy proposal: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.PolicyProposal{}, fmt.Errorf("repository: read policy proposal id: %w", err)
	}

	p.ID = id
	p.ProposedAt = proposedAt
	p.ProposedBy = proposedBy
	p.Status = status
	p.CreatedAt = createdAt
	return p, nil
}

const proposalSelectColumns = `
SELECT id, proposed_at, proposed_by, rationale_json, proposed_changes_json, status,
	backtest_result_json, reviewed_by, review_json, applied_policy_version, applied_at,
	rolled_back_at, rolled_back_reason, created_at`

// Get returns the policy_proposals row with the given id, or
// ErrPolicyProposalNotFound.
func (r *ProposalRepository) Get(ctx context.Context, id int64) (domain.PolicyProposal, error) {
	row := r.db.QueryRowContext(ctx, proposalSelectColumns+` FROM policy_proposals WHERE id = ?`, id)
	return scanProposal(row)
}

// ListByStatus returns every policy_proposals row with the given status,
// most recently proposed first (internal/service/selfimprove.Governor
// uses status=applied to find proposals whose FR-SELFIMPROVE-6 post-apply
// tracking window has not yet closed).
func (r *ProposalRepository) ListByStatus(ctx context.Context, status string) ([]domain.PolicyProposal, error) {
	rows, err := r.db.QueryContext(ctx,
		proposalSelectColumns+` FROM policy_proposals WHERE status = ? ORDER BY proposed_at DESC`, status)
	if err != nil {
		return nil, fmt.Errorf("repository: list policy proposals with status %s: %w", status, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.PolicyProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list policy proposals with status %s: %w", status, err)
	}
	return out, nil
}

// List returns policy_proposals rows, most recently proposed first
// (id descending breaks ties), for `GET /api/v1/policy-proposals`. A
// non-empty status keeps only rows with that status; limit caps the
// number of rows returned and must be positive.
func (r *ProposalRepository) List(ctx context.Context, status string, limit int) ([]domain.PolicyProposal, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("repository: list policy proposals: limit must be positive, got %d", limit)
	}
	query := proposalSelectColumns + ` FROM policy_proposals`
	var args []any
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY proposed_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list policy proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.PolicyProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list policy proposals: %w", err)
	}
	return out, nil
}

// UpdateBacktestResult records Opus's shadow-backtest Expectancy/
// MaxDrawdown comparison for a still-pending proposal (FR-SELFIMPROVE-4),
// without changing its status.
func (r *ProposalRepository) UpdateBacktestResult(ctx context.Context, id int64, backtestResultJSON string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE policy_proposals SET backtest_result_json = ? WHERE id = ?`, backtestResultJSON, id)
	if err != nil {
		return fmt.Errorf("repository: update policy proposal %d backtest result: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read update result for policy proposal %d: %w", id, err)
	}
	if n == 0 {
		return ErrPolicyProposalNotFound
	}
	return nil
}

// UpdateReview records Opus's approve/reject decision (FR-SELFIMPROVE-5):
// status must be domain.PolicyProposalStatusApproved or
// PolicyProposalStatusRejected.
func (r *ProposalRepository) UpdateReview(ctx context.Context, id int64, status, reviewedBy, reviewJSON string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE policy_proposals SET status = ?, reviewed_by = ?, review_json = ? WHERE id = ?`,
		status, reviewedBy, reviewJSON, id)
	if err != nil {
		return fmt.Errorf("repository: update policy proposal %d review: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read update result for policy proposal %d: %w", id, err)
	}
	if n == 0 {
		return ErrPolicyProposalNotFound
	}
	return nil
}

// MarkApplied records a successful FR-SELFIMPROVE-5 apply: status
// becomes domain.PolicyProposalStatusApplied.
func (r *ProposalRepository) MarkApplied(ctx context.Context, id int64, appliedPolicyVersion string, appliedAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE policy_proposals SET status = ?, applied_policy_version = ?, applied_at = ? WHERE id = ?`,
		domain.PolicyProposalStatusApplied, appliedPolicyVersion, sqlutil.FormatTime(appliedAt), id)
	if err != nil {
		return fmt.Errorf("repository: mark policy proposal %d applied: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read mark-applied result for policy proposal %d: %w", id, err)
	}
	if n == 0 {
		return ErrPolicyProposalNotFound
	}
	return nil
}

// MarkRolledBack records FR-SELFIMPROVE-6's automatic rollback: status
// becomes domain.PolicyProposalStatusRolledBack.
func (r *ProposalRepository) MarkRolledBack(ctx context.Context, id int64, rolledBackAt time.Time, reason string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE policy_proposals SET status = ?, rolled_back_at = ?, rolled_back_reason = ? WHERE id = ?`,
		domain.PolicyProposalStatusRolledBack, sqlutil.FormatTime(rolledBackAt), reason, id)
	if err != nil {
		return fmt.Errorf("repository: mark policy proposal %d rolled back: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read mark-rolled-back result for policy proposal %d: %w", id, err)
	}
	if n == 0 {
		return ErrPolicyProposalNotFound
	}
	return nil
}

func scanProposal(row sqlutil.RowScanner) (domain.PolicyProposal, error) {
	var (
		p                    domain.PolicyProposal
		proposedAt           string
		backtestResultJSON   sql.NullString
		reviewedBy           sql.NullString
		reviewJSON           sql.NullString
		appliedPolicyVersion sql.NullString
		appliedAt            sql.NullString
		rolledBackAt         sql.NullString
		rolledBackReason     sql.NullString
		createdAt            string
	)
	err := row.Scan(
		&p.ID, &proposedAt, &p.ProposedBy, &p.RationaleJSON, &p.ProposedChangesJSON, &p.Status,
		&backtestResultJSON, &reviewedBy, &reviewJSON, &appliedPolicyVersion, &appliedAt,
		&rolledBackAt, &rolledBackReason, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PolicyProposal{}, ErrPolicyProposalNotFound
	}
	if err != nil {
		return domain.PolicyProposal{}, fmt.Errorf("repository: scan policy proposal: %w", err)
	}

	if p.ProposedAt, err = sqlutil.ParseTime(proposedAt); err != nil {
		return domain.PolicyProposal{}, err
	}
	if p.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return domain.PolicyProposal{}, err
	}
	if p.AppliedAt, err = sqlutil.ParseNullableTime(appliedAt); err != nil {
		return domain.PolicyProposal{}, err
	}
	if p.RolledBackAt, err = sqlutil.ParseNullableTime(rolledBackAt); err != nil {
		return domain.PolicyProposal{}, err
	}
	if backtestResultJSON.Valid {
		p.BacktestResultJSON = &backtestResultJSON.String
	}
	if reviewedBy.Valid {
		p.ReviewedBy = &reviewedBy.String
	}
	if reviewJSON.Valid {
		p.ReviewJSON = &reviewJSON.String
	}
	if appliedPolicyVersion.Valid {
		p.AppliedPolicyVersion = &appliedPolicyVersion.String
	}
	if rolledBackReason.Valid {
		p.RolledBackReason = &rolledBackReason.String
	}
	return p, nil
}
