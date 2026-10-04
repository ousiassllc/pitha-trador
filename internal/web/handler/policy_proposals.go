package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// Bounds of `GET /api/v1/policy-proposals`'s limit query parameter
// (docs/api/endpoints.md: 既定50、最大200).
const (
	defaultPolicyProposalsLimit = 50
	maxPolicyProposalsLimit     = 200
)

// PolicyProposalSource supplies the Sol/Opus self-improvement audit
// history for `GET /api/v1/policy-proposals`. List returns proposals most
// recent first, optionally restricted to one status, at most limit rows.
// *internal/repository/judgement.ProposalRepository implements it directly.
type PolicyProposalSource interface {
	List(ctx context.Context, status string, limit int) ([]domain.PolicyProposal, error)
}

// StaticPolicyProposalSource is a fixed PolicyProposalSource, used as
// internal/router.New()'s default until the real repository is wired in
// (mirrors StaticCalibrationSource).
type StaticPolicyProposalSource struct {
	Proposals []domain.PolicyProposal
}

func (s StaticPolicyProposalSource) List(_ context.Context, status string, limit int) ([]domain.PolicyProposal, error) {
	var out []domain.PolicyProposal
	for _, p := range s.Proposals {
		if status != "" && p.Status != status {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, p)
	}
	return out, nil
}

// PolicyProposalHandler implements `GET /api/v1/policy-proposals`
// (docs/api/endpoints.md), the read-only audit API of the Sol/Opus
// self-improvement loop (functional.md FR-SELFIMPROVE-7〜9). It has no UI
// page.
type PolicyProposalHandler struct {
	source PolicyProposalSource
}

// NewPolicyProposalHandler returns a PolicyProposalHandler backed by
// source.
func NewPolicyProposalHandler(source PolicyProposalSource) *PolicyProposalHandler {
	return &PolicyProposalHandler{source: source}
}

// PolicyProposalsInput is `GET /api/v1/policy-proposals`'s query
// parameters.
type PolicyProposalsInput struct {
	Status string `query:"status" enum:"pending,approved,rejected,applied,rolled_back" doc:"Only proposals with this status. Omit for every status."`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Maximum number of proposals to return (most recent first)."`
}

// backtestResultOutput is the shadow-backtest comparison
// (FR-SELFIMPROVE-4) with its relative deltas. The delta fields are null
// when the baseline value is zero (no meaningful percentage).
type backtestResultOutput struct {
	BaselineExpectancy      float64  `json:"baseline_expectancy"`
	CandidateExpectancy     float64  `json:"candidate_expectancy"`
	BaselineMaxDrawdownPct  float64  `json:"baseline_max_drawdown_pct"`
	CandidateMaxDrawdownPct float64  `json:"candidate_max_drawdown_pct"`
	ExpectancyDeltaPct      *float64 `json:"expectancy_delta_pct" doc:"(candidate - baseline) / |baseline| * 100 of shadow-backtest Expectancy."`
	MaxDrawdownDeltaPct     *float64 `json:"max_drawdown_delta_pct" doc:"(candidate - baseline) / baseline * 100 of shadow-backtest Max Drawdown."`
}

// policyProposalOutput mirrors one docs/api/endpoints.md `items[]` entry.
type policyProposalOutput struct {
	ID                   int64                      `json:"id"`
	ProposedAt           time.Time                  `json:"proposed_at"`
	ProposedBy           string                     `json:"proposed_by"`
	Status               string                     `json:"status" enum:"pending,approved,rejected,applied,rolled_back"`
	ProposedChanges      map[string]json.RawMessage `json:"proposed_changes" doc:"policy.* key -> proposed new value."`
	BacktestResult       *backtestResultOutput      `json:"backtest_result" doc:"Null until the shadow backtest has run."`
	ReviewedBy           *string                    `json:"reviewed_by"`
	Review               json.RawMessage            `json:"review" doc:"Review verdict/reason as recorded (e.g. reason=llm_output_out_of_bounds); null until reviewed."`
	AppliedPolicyVersion *string                    `json:"applied_policy_version"`
}

// PolicyProposalsAPIOutput is the Huma response body for `GET
// /api/v1/policy-proposals`.
type PolicyProposalsAPIOutput struct {
	Body struct {
		Items []policyProposalOutput `json:"items"`
	}
}

// APIPolicyProposals implements `GET /api/v1/policy-proposals`
// (docs/api/endpoints.md): the proposal, review, apply and rollback
// history recorded in policy_proposals, most recent first.
func (h *PolicyProposalHandler) APIPolicyProposals(ctx context.Context, in *PolicyProposalsInput) (*PolicyProposalsAPIOutput, error) {
	limit := in.Limit
	if limit == 0 {
		limit = defaultPolicyProposalsLimit
	}
	proposals, err := h.source.List(ctx, in.Status, limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("load policy proposals failed", err)
	}

	out := &PolicyProposalsAPIOutput{}
	out.Body.Items = make([]policyProposalOutput, 0, len(proposals))
	for _, p := range proposals {
		out.Body.Items = append(out.Body.Items, toPolicyProposalOutput(ctx, p))
	}
	return out, nil
}

// toPolicyProposalOutput converts one stored row. A row whose stored JSON
// cannot be decoded is still returned (so one corrupt, old row cannot hide
// the rest of the audit history) with only the undecodable field emptied:
// proposed_changes -> {}, backtest_result / review -> null. Each such
// failure is logged with the proposal_id; only a store failure in List is a
// 500.
func toPolicyProposalOutput(ctx context.Context, p domain.PolicyProposal) policyProposalOutput {
	item := policyProposalOutput{
		ID:                   p.ID,
		ProposedAt:           p.ProposedAt,
		ProposedBy:           p.ProposedBy,
		Status:               p.Status,
		ProposedChanges:      decodeProposedChanges(ctx, p),
		ReviewedBy:           p.ReviewedBy,
		Review:               json.RawMessage("null"),
		AppliedPolicyVersion: p.AppliedPolicyVersion,
	}
	if p.ReviewJSON != nil {
		if json.Valid([]byte(*p.ReviewJSON)) {
			item.Review = json.RawMessage(*p.ReviewJSON)
		} else {
			slog.ErrorContext(ctx, "handler: policy proposal review is not valid JSON", "proposal_id", p.ID)
		}
	}
	if p.BacktestResultJSON != nil {
		var cmp assist.BacktestComparison
		if err := json.Unmarshal([]byte(*p.BacktestResultJSON), &cmp); err != nil {
			slog.ErrorContext(ctx, "handler: decode policy proposal backtest result", "proposal_id", p.ID, "error", err)
		} else {
			item.BacktestResult = &backtestResultOutput{
				BaselineExpectancy:      cmp.BaselineExpectancy,
				CandidateExpectancy:     cmp.CandidateExpectancy,
				BaselineMaxDrawdownPct:  cmp.BaselineMaxDrawdownPct,
				CandidateMaxDrawdownPct: cmp.CandidateMaxDrawdownPct,
				ExpectancyDeltaPct:      relativeDeltaPct(cmp.BaselineExpectancy, cmp.CandidateExpectancy, true),
				MaxDrawdownDeltaPct:     relativeDeltaPct(cmp.BaselineMaxDrawdownPct, cmp.CandidateMaxDrawdownPct, false),
			}
		}
	}
	return item
}

// decodeProposedChanges returns p's key -> new value map, or an empty map
// (logged) when ProposedChangesJSON is not parseable or a NewValue is not
// valid JSON (json.RawMessage would otherwise fail the whole response at
// serialization time).
func decodeProposedChanges(ctx context.Context, p domain.PolicyProposal) map[string]json.RawMessage {
	changes, err := domain.ParsePolicyChanges(p.ProposedChangesJSON)
	if err != nil {
		slog.ErrorContext(ctx, "handler: decode policy proposal changes", "proposal_id", p.ID, "error", err)
		return map[string]json.RawMessage{}
	}
	proposed := make(map[string]json.RawMessage, len(changes))
	for _, c := range changes {
		if !json.Valid([]byte(c.NewValue)) {
			slog.ErrorContext(ctx, "handler: policy proposal change value is not valid JSON", "proposal_id", p.ID, "key", c.Key)
			return map[string]json.RawMessage{}
		}
		proposed[c.Key] = json.RawMessage(c.NewValue)
	}
	return proposed
}

// relativeDeltaPct returns (candidate - baseline) / baseline * 100, or nil
// when baseline is zero. absBaseline divides by |baseline| instead, so a
// negative baseline Expectancy that improves reads as a positive delta.
func relativeDeltaPct(baseline, candidate float64, absBaseline bool) *float64 {
	if baseline == 0 {
		return nil
	}
	denom := baseline
	if absBaseline && denom < 0 {
		denom = -denom
	}
	delta := (candidate - baseline) / denom * 100
	return &delta
}
