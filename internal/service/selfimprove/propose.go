package selfimprove

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// ProposeDaily runs Sol's daily analysis (FR-SELFIMPROVE-1) and records
// a pending policy_proposals row when Sol finds an actionable weakness.
// It returns (proposal, true, nil) on a new proposal, or
// (domain.PolicyProposal{}, false, nil) when Sol proposes nothing today.
func (g *Governor) ProposeDaily(ctx context.Context, longCalibration, shortCalibration domain.CalibrationMetrics) (domain.PolicyProposal, bool, error) {
	current, err := g.CurrentThresholds(ctx)
	if err != nil {
		return domain.PolicyProposal{}, false, err
	}

	proposal, ok, err := g.sol.Analyze(assist.SolAnalysisInput{
		Long:  assist.DirectionCalibration{Thresholds: current.Long, Calibration: longCalibration},
		Short: assist.DirectionCalibration{Thresholds: current.Short, Calibration: shortCalibration},
	})
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: sol analysis: %w", err)
	}
	if !ok {
		return domain.PolicyProposal{}, false, nil
	}

	changes, err := domain.ParsePolicyChanges(proposal.ProposedChangesJSON)
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: parse sol proposal: %w", err)
	}
	if err := domain.ValidatePolicyChanges(changes); err != nil {
		// Defense in depth: assist.Sol.Analyze should never emit an
		// invalid change, but Governor never records one regardless
		// (FR-SELFIMPROVE-2/3).
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: sol proposal failed validation: %w", err)
	}

	stored, err := g.proposals.Insert(ctx, domain.PolicyProposal{
		ProposedAt:          g.now(),
		RationaleJSON:       proposal.RationaleJSON,
		ProposedChangesJSON: proposal.ProposedChangesJSON,
	})
	if err != nil {
		return domain.PolicyProposal{}, false, fmt.Errorf("selfimprove: insert proposal: %w", err)
	}
	return stored, true, nil
}
